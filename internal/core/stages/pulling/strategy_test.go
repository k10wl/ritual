package pulling_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func noHeadResolver() pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return "", pulling.ErrNoHead }
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

func TestPulling_RecordsPulledHeadOnRunStateForDownstreamParentResolution(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, staticResolver("2026-04-23T10-00-00.000Z"), onOK, onFail)

	rs := newRunState()
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, domain.RefID("2026-04-23T10-00-00.000Z"), rs.ParentRefID,
		"Pulling stage must record the pulled head id on rs.ParentRefID after Apply succeeds so the committing stage's fresh-commit branch has a Parent to reference")
}

func TestPulling_OnErrNoHeadFromResolver_RoutesToOnOKWithoutCallingVerbs(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, noHeadResolver(), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Empty(t, puller.calls,
		"Pulling stage must short-circuit on ErrNoHead so an empty-storage bootstrap does not issue pointless GETs")
	assert.Empty(t, applier.calls,
		"Pulling stage must not call Apply when the resolver signals there is nothing to materialise")
	assert.Equal(t, domain.RefID(""), rs.ParentRefID,
		"Pulling stage must leave rs.ParentRefID empty on ErrNoHead so the committing stage creates a true root commit with no parent (spec §1073 permits empty Parent on initial commit)")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pulling stage must route to onOK on ErrNoHead — bootstrap is success, not failure, so acquiring and running still proceed")
}

func TestPulling_LeavesParentRefIDEmptyOnPullError(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{err: errors.New("pull: network")}
	applier := &recordingApplier{}

	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)

	rs := newRunState()
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, domain.RefID(""), rs.ParentRefID,
		"Pulling stage must not write rs.ParentRefID when Pull fails — the local workdir was not materialised from that ref, so downstream must not treat it as the pulled head")
}

func TestPulling_LeavesParentRefIDEmptyOnApplyError(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{err: errors.New("apply: disk full")}

	stage := pulling.New(puller, applier, staticResolver("id"), onOK, onFail)

	rs := newRunState()
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, domain.RefID(""), rs.ParentRefID,
		"Pulling stage must not write rs.ParentRefID when Apply fails — the workdir is partial, so claiming this id as pulled-head would mislead the committing stage into an inconsistent Parent link")
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
	for !sawStart || !sawFinish {
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

type blockingPuller struct {
	started chan struct{}
}

func (p *blockingPuller) Pull(ctx context.Context, _ domain.RefID) error {
	close(p.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestPulling_StopRequestedMidPull_AbortsFastAndRoutesOnFail(t *testing.T) {
	bus := adapters.NewEventBus(16)
	puller := &blockingPuller{started: make(chan struct{})}
	applier := &recordingApplier{}
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	stage := pulling.New(puller, applier, staticResolver("2026-04-23T10-00-00.000Z"), onOK, onFail)
	rs := &ritual.RunState{Bus: bus}

	type result struct {
		next machine.Strategy[ritual.RunState]
		err  error
	}
	done := make(chan result, 1)
	go func() {
		next, err := stage.Run(t.Context(), rs)
		done <- result{next, err}
	}()

	<-puller.started
	bus.Publish(ritual.StopRequested{})

	select {
	case r := <-done:
		require.NoError(t, r.err, "Pulling stage must never return a Go error — failures travel via onFail per stage contract")
		assert.Equal(t, machine.Strategy[ritual.RunState](onFail), r.next, "ritual.StopRequested arriving mid-pull must abort the transfer and route to onFail — without this the user is stuck waiting up to 20s after pressing Stop while the in-flight blob batch drains naturally (audit open item #3)")
		assert.ErrorIs(t, rs.Err, context.Canceled, "rs.Err must carry context.Canceled so the Failed handler can recognise this as a user-initiated abort instead of a real network failure")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pulling stage did not abort within 200ms of ritual.StopRequested — bus subscription missing or local stop-ctx never cancelled")
	}
}

// --- Restore flow: target-pinned resolver (design-log/038) ---

func TestPulling_FromTarget_PullsRunStateTargetRefAndRoutesOnOK(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.NewWithResolver(puller, applier, pulling.FromTarget(), onOK, onFail)

	rs := newRunState()
	rs.TargetRefID = "2026-03-01T08-00-00.000Z" // an older ref the user chose to restore
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, []domain.RefID{"2026-03-01T08-00-00.000Z"}, puller.calls,
		"restore must pull the run state's TargetRefID, not the HEAD — that is the whole point of rolling back to a chosen version")
	assert.Equal(t, []domain.RefID{"2026-03-01T08-00-00.000Z"}, applier.calls,
		"restore must apply the target ref into the workdir so the rollback is materialised")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next)
}

func TestPulling_FromTarget_EmptyTargetRoutesOnFailWithoutCallingVerbs(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	puller := &recordingPuller{}
	applier := &recordingApplier{}

	stage := pulling.NewWithResolver(puller, applier, pulling.FromTarget(), onOK, onFail)

	rs := newRunState() // TargetRefID unset
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Pulling stage never returns a Go error — failures travel via onFail")
	assert.Empty(t, puller.calls, "an empty target is a wiring bug, not a no-op bootstrap: must not pull")
	assert.Empty(t, applier.calls)
	assert.ErrorIs(t, rs.Err, pulling.ErrNoTarget,
		"a restore with no TargetRefID must surface ErrNoTarget (unlike ErrNoHead it is a hard failure, not a no-op success)")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next)
}

func TestPulling_FromHeadAdapter_IgnoresRunStateTarget(t *testing.T) {
	// The HEAD-pinned adapter must resolve storage HEAD regardless of any
	// TargetRefID left on the run state — proving FromHead preserves the exact
	// pre-038 behaviour for Session/Download.
	puller := &recordingPuller{}
	applier := &recordingApplier{}
	stage := pulling.New(puller, applier, staticResolver("2026-05-09T12-00-00.000Z"),
		&sentinelStrategy{name: "ok"}, &sentinelStrategy{name: "fail"})

	rs := newRunState()
	rs.TargetRefID = "2026-01-01T00-00-00.000Z" // must be ignored by the HEAD adapter
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, []domain.RefID{"2026-05-09T12-00-00.000Z"}, puller.calls,
		"FromHead must ignore rs.TargetRefID and pull the storage HEAD — Session/Download are unchanged by the 038 resolver generalisation")
}

var (
	_ ports.Puller  = (*recordingPuller)(nil)
	_ ports.Applier = (*recordingApplier)(nil)
	_ ports.Puller  = (*blockingPuller)(nil)
)
