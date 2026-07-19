package checking_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/checks"
	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/checking"
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

func newRunState(t *testing.T) *ritual.RunState {
	t.Helper()
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func passing(seq *[]string, name string) checks.Check {
	return func(_ context.Context) error {
		*seq = append(*seq, name)
		return nil
	}
}

func failing(seq *[]string, name string, err error) checks.Check {
	return func(_ context.Context) error {
		*seq = append(*seq, name)
		return err
	}
}

func TestChecking_RunsEveryCheckInOrderAndRoutesToOnOKWhenAllPass(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	var seq []string

	stage := checking.New([]checks.Check{
		passing(&seq, "ram"),
		passing(&seq, "disk"),
		passing(&seq, "java"),
	}, onOK, onFail)

	next, err := stage.Run(t.Context(), newRunState(t))

	require.NoError(t, err, "Checking stage must never return a Go error — failures travel via onFail")
	assert.Equal(t, []string{"ram", "disk", "java"}, seq,
		"Checking stage must invoke checks in the slice order so dependency-ordered preconditions hold")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Checking stage must route to onOK after every check passes so the chain advances to Fetching")
}

func TestChecking_RecordsFirstFailingCheckErrorAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("insufficient RAM: have 1024 MB, need 4096 MB")
	var seq []string

	stage := checking.New([]checks.Check{
		passing(&seq, "ram"),
		failing(&seq, "disk", boom),
		passing(&seq, "java"),
	}, onOK, onFail)

	rs := newRunState(t)
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Checking stage must never return a Go error — failures travel via onFail and rs.Err")
	assert.Equal(t, []string{"ram", "disk"}, seq,
		"Checking stage must short-circuit on the first failure so wasted probes do not run")
	assert.Same(t, boom, rs.Err,
		"Checking stage must record the failing check's error verbatim on RunState so the Failed stage can surface it")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Checking stage must route to onFail when any check fails so the operator sees the error path")
}

func TestChecking_AbortsImmediatelyAndRoutesToOnFailWhenContextCancelledMidBatch(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	var seq []string

	ctx, cancel := context.WithCancel(t.Context())
	cancelAfterFirst := checks.Check(func(_ context.Context) error {
		seq = append(seq, "ram")
		cancel()
		return nil
	})

	stage := checking.New([]checks.Check{
		cancelAfterFirst,
		passing(&seq, "disk"),
	}, onOK, onFail)

	rs := newRunState(t)
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err, "Checking stage must never return a Go error — cancellation travels via onFail")
	assert.Equal(t, []string{"ram"}, seq,
		"Checking stage must observe ctx cancellation between checks so subsequent probes do not run")
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Checking stage must record context.Canceled on RunState so cancellation surfaces as the failure cause")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Checking stage must route to onFail when ctx is cancelled mid-batch so the chain does not silently advance")
}

func TestChecking_RoutesToOnFailWhenContextAlreadyCancelledBeforeFirstCheck(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	var seq []string

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	stage := checking.New([]checks.Check{passing(&seq, "ram")}, onOK, onFail)

	rs := newRunState(t)
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err)
	assert.Empty(t, seq,
		"Checking stage must not invoke any check when ctx is cancelled before entry — protects providers from wasted IO")
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Checking stage must record context.Canceled when entered with a cancelled ctx")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Checking stage must route to onFail on entry-time cancellation so the chain does not advance")
}

func TestChecking_PublishesBatchLifecycleEventsOnTheBus(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	var seq []string
	stage := checking.New([]checks.Check{passing(&seq, "ram")}, onOK, onFail)
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
			if s, ok := e.(ritual.StartInfo); ok && s.Operation == "check" {
				sawStart = true
			}
			if f, ok := e.(ritual.FinishInfo); ok && f.Operation == "check" {
				sawFinish = true
			}
		}
	}
}
