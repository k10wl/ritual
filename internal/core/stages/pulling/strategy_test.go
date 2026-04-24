package pulling_test

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
	"ritual/internal/core/stages/pulling"
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

type recordingPuller struct {
	calls []domain.RefID
	err   error
}

func (p *recordingPuller) Pull(_ context.Context, id domain.RefID) error {
	p.calls = append(p.calls, id)
	return p.err
}

type recordingApplier struct {
	calls []domain.RefID
	err   error
}

func (a *recordingApplier) Apply(_ context.Context, id domain.RefID) error {
	a.calls = append(a.calls, id)
	return a.err
}

func staticResolver(id domain.RefID) pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return id, nil }
}

func failingResolver(err error) pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return "", err }
}

func newRunState() *ritual.RunState {
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func TestPulling_ResolvesHeadPullsThenAppliesAndRoutesToOnOKOnSuccess(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, staticResolver("2026-04-23T10-00-00.000Z"), onOK, onFail)

	next, err := stage.Run(t.Context(), newRunState())

	require.NoError(t, err, "Pulling stage must never return a Go error — failures travel via onFail")
	assert.Equal(t, []domain.RefID{"2026-04-23T10-00-00.000Z"}, puller.calls,
		"Pulling stage must call Puller.Pull exactly once with the head-resolved ref id so remote blobs land in local storage")
	assert.Equal(t, []domain.RefID{"2026-04-23T10-00-00.000Z"}, applier.calls,
		"Pulling stage must call Applier.Apply with the same id so the workdir reflects the pulled ref")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pulling stage must route to onOK after both verbs succeed so the chain advances to Acquiring")
}

func TestPulling_RecordsHeadResolverErrorAndRoutesToOnFailWithoutCallingVerbs(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("no refs on remote")
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, failingResolver(boom), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Pulling stage must record the head-resolver error verbatim on RunState so the operator sees why pull could not start")
	assert.Empty(t, puller.calls,
		"Pulling stage must not call Puller.Pull when HEAD cannot be resolved — wasted network round-trips are forbidden")
	assert.Empty(t, applier.calls,
		"Pulling stage must not call Applier.Apply when HEAD cannot be resolved — there is nothing to materialise")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pulling stage must route to onFail when head-resolver errors so the chain does not silently advance")
}

func TestPulling_RecordsPullerErrorAndSkipsApply(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("pull 2026-04-23: blob deadbeef: download failed")
	puller := &recordingPuller{err: boom}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Pulling stage must record Puller.Pull's error verbatim so retry/operator messaging preserves the original cause")
	assert.Empty(t, applier.calls,
		"Pulling stage must not call Applier.Apply after Pull failed — Apply would read incomplete local blobs and corrupt the workdir")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pulling stage must route to onFail when Pull errors so the failure propagates up the chain")
}

func TestPulling_RecordsApplierErrorAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("apply id: place world/level.dat (hash deadbeef): write workdir: disk full")
	puller := &recordingPuller{}
	applier := &recordingApplier{err: boom}

	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Pulling stage must record Applier.Apply's error verbatim so the operator sees which file failed")
	assert.Equal(t, 1, len(puller.calls),
		"Pulling stage must still invoke Pull before Apply — apply failure does not roll back the pulled blobs (they are content-addressed and reusable)")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pulling stage must route to onFail when Apply errors so downstream stages do not run on a partial workdir")
}

func TestPulling_RoutesToOnFailWhenContextAlreadyCancelledBeforeResolve(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err)
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Pulling stage must record context.Canceled when entered with a cancelled ctx so cancellation surfaces as the failure cause")
	assert.Empty(t, puller.calls,
		"Pulling stage must not issue any remote call when ctx is cancelled on entry — protects the storage port from wasted IO")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pulling stage must route to onFail on entry-time cancellation so the chain does not advance")
}

func TestPulling_PublishesBatchLifecycleEventsOnTheBus(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	puller := &recordingPuller{}
	applier := &recordingApplier{}
	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)
	rs := &ritual.RunState{Bus: bus}

	_, err := stage.Run(t.Context(), rs)
	require.NoError(t, err)

	deadline := time.After(time.Second)
	var sawStart, sawFinish bool
	for !(sawStart && sawFinish) {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for batch lifecycle events: start=%v finish=%v", sawStart, sawFinish)
		case e := <-ch:
			if s, ok := e.(ritual.StartInfo); ok && s.Operation == "pull" {
				sawStart = true
			}
			if f, ok := e.(ritual.FinishInfo); ok && f.Operation == "pull" {
				sawFinish = true
			}
		}
	}
}

var _ ports.Puller = (*recordingPuller)(nil)
var _ ports.Applier = (*recordingApplier)(nil)
