package pushing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pushing"
)

type sentinelStrategy struct {
	name   string
	called bool
}

func (s *sentinelStrategy) Name() string { return s.name }
func (s *sentinelStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	s.called = true
	return nil, nil
}

type recordingPusher struct {
	calls []domain.RefID
	err   error
}

func (p *recordingPusher) Push(_ context.Context, id domain.RefID) error {
	p.calls = append(p.calls, id)
	return p.err
}

func newRunState() *ritual.RunState {
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func TestPushing_PushesRSRefIDAndRoutesToOnOKOnSuccess(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "2026-04-25T10-00-00.000Z"
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Pushing stage must never return a Go error — failures travel via onFail")
	assert.Equal(t, []domain.RefID{"2026-04-25T10-00-00.000Z"}, pusher.calls,
		"Pushing stage must call Pusher.Push exactly once with rs.RefID — the committing stage wrote that id and pushing is the commit point that makes it visible on remote")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pushing stage must route to onOK after Push succeeds so remote retention runs next and the chain advances toward unlocking")
}

func TestPushing_RecordsPusherErrorAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("push: upload objects/abc: 500 internal")
	pusher := &recordingPusher{err: boom}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "id"
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Pushing stage must record Pusher.Push's error verbatim on rs.Err so the operator sees which blob or ref upload failed")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pushing stage must route to onFail when Push errors — the failure path skips remote retention so a half-uploaded ref is not swept")
}

func TestPushing_RoutesToOnFailWhenContextAlreadyCancelledBeforePush(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "id"
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err)
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Pushing stage must record context.Canceled when entered with a cancelled ctx so cancellation surfaces as the failure cause")
	assert.Empty(t, pusher.calls,
		"Pushing stage must not invoke Pusher.Push when ctx is cancelled on entry — no partial upload IO")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pushing stage must route to onFail on entry-time cancellation")
}

func TestPushing_SkipsPushWhenRefIDEmpty(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Empty(t, pusher.calls,
		"Pushing stage must skip Pusher.Push when rs.RefID is empty — committing produced no new ref (e.g., nothing changed) so there is nothing to upload")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pushing stage must route to onOK when rs.RefID is empty so remote retention still runs (idempotent GC over whatever prior sessions left)")
}

type blockingPusher struct {
	started chan struct{}
}

func (p *blockingPusher) Push(ctx context.Context, _ domain.RefID) error {
	close(p.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestPushing_StopRequestedMidPush_AbortsFastAndRoutesToOnStopBypassingFailEdge(t *testing.T) {
	bus := adapters.NewEventBus(16)
	pusher := &blockingPusher{started: make(chan struct{})}
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	onStop := &sentinelStrategy{name: "stop"}
	stage := pushing.New(pusher, onOK, onFail)
	stage.OnStop(onStop)
	rs := &ritual.RunState{Bus: bus, RefID: "2026-04-25T10-00-00.000Z"}

	type result struct {
		next machine.Strategy[ritual.RunState]
		err  error
	}
	done := make(chan result, 1)
	go func() {
		next, err := stage.Run(t.Context(), rs)
		done <- result{next, err}
	}()

	<-pusher.started
	bus.Publish(ritual.StopRequested{})

	select {
	case r := <-done:
		require.NoError(t, r.err, "Pushing stage must never return a Go error — outcomes travel via onOK / onFail / onStop edges")
		assert.Same(t, machine.Strategy[ritual.RunState](onStop), r.next, "ritual.StopRequested arriving mid-push must route to the onStop edge — production wires this to unlocking so the held lock is released even though the user-cancelled run never finishes the upload (audit open item #3). Routing to onFail instead would terminate the run with the lock still held, blocking the next session")
		assert.ErrorIs(t, rs.Err, context.Canceled, "rs.Err must carry context.Canceled so the unlocking handler can recognise this as a user-initiated abort instead of a real network failure")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pushing stage did not abort within 200ms of ritual.StopRequested — bus subscription missing or local stop-ctx never cancelled")
	}
}

func TestPushing_StopRequestedMidPush_FallsBackToOnFailWhenOnStopUnset(t *testing.T) {
	bus := adapters.NewEventBus(16)
	pusher := &blockingPusher{started: make(chan struct{})}
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	stage := pushing.New(pusher, onOK, onFail)
	rs := &ritual.RunState{Bus: bus, RefID: "id"}

	type result struct {
		next machine.Strategy[ritual.RunState]
		err  error
	}
	done := make(chan result, 1)
	go func() {
		next, err := stage.Run(t.Context(), rs)
		done <- result{next, err}
	}()

	<-pusher.started
	bus.Publish(ritual.StopRequested{})

	select {
	case r := <-done:
		require.NoError(t, r.err)
		assert.Same(t, machine.Strategy[ritual.RunState](onFail), r.next, "when OnStop is unset Push errors caused by stop-ctx cancellation must fall back to the onFail edge — preserves the pre-fix contract for callers that have not opted into the dedicated stop routing yet")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pushing stage did not return within 200ms of ritual.StopRequested fallback path")
	}
}

func TestPushing_PublishesBatchLifecycleEventsOnTheBus(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	stage := pushing.New(&recordingPusher{}, onOK, onFail)
	rs := &ritual.RunState{Bus: bus, RefID: "id"}

	_, err := stage.Run(t.Context(), rs)
	require.NoError(t, err)

	deadline := time.After(time.Second)
	var sawStart, sawFinish bool
	for !(sawStart && sawFinish) {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for batch lifecycle events: start=%v finish=%v", sawStart, sawFinish)
		case e := <-ch:
			if s, ok := e.(ritual.StartInfo); ok && s.Operation == "push" {
				sawStart = true
			}
			if f, ok := e.(ritual.FinishInfo); ok && f.Operation == "push" {
				sawFinish = true
			}
		}
	}
}

var _ ports.Pusher = (*recordingPusher)(nil)
