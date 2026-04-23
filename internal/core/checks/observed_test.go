package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/checks"
	"ritual/internal/core/ports"
)

func collectEvents(bus ports.EventBus) func() []ports.Event {
	ch, unsub := bus.Subscribe()
	done := make(chan struct{})
	var captured []ports.Event
	go func() {
		defer close(done)
		for e := range ch {
			captured = append(captured, e)
		}
	}()
	return func() []ports.Event {
		unsub()
		<-done
		return captured
	}
}

func okCheck() checks.Check  { return func(_ context.Context) error { return nil } }
func errCheck(err error) checks.Check {
	return func(_ context.Context) error { return err }
}

func TestObserved_PublishesStartedThenPassedWhenWrappedCheckSucceeds(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	wrapped := checks.Observed("ram", okCheck(), bus)

	require.NoError(t, wrapped(t.Context()),
		"Observed must propagate the underlying nil result so the stage sees a passing check")
	events := drain()

	require.Len(t, events, 2,
		"Observed must publish exactly Started and Passed when the wrapped check succeeds — no extra noise")
	_, ok := events[0].(checks.CheckStarted)
	assert.True(t, ok, "first event must be CheckStarted so subscribers can pair it with the terminal event")
	passed, ok := events[1].(checks.CheckPassed)
	require.True(t, ok, "second event must be CheckPassed so subscribers can mark the check green")
	assert.Equal(t, "ram", passed.Name,
		"CheckPassed must carry the configured name so logs and GUI surfaces can identify which check passed")
}

func TestObserved_PublishesStartedThenFailedAndPropagatesError(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	underlying := errors.New("perfmon unavailable")
	wrapped := checks.Observed("ram", errCheck(underlying), bus)

	err := wrapped(t.Context())
	events := drain()

	require.Error(t, err, "Observed must propagate the underlying error so the stage routes to onFail")
	assert.ErrorIs(t, err, underlying,
		"Observed must wrap with %%w so callers can still match the underlying sentinel")
	require.Len(t, events, 2,
		"Observed must publish exactly Started and Failed when the wrapped check errors")
	failed, ok := events[1].(checks.CheckFailed)
	require.True(t, ok, "terminal event for a failing check must be CheckFailed so subscribers can mark it red")
	assert.Equal(t, "ram", failed.Name,
		"CheckFailed must carry the configured name so failures are self-identifying in logs and the GUI")
	assert.Same(t, underlying, failed.Err,
		"CheckFailed must carry the underlying error verbatim so subscribers can render the root cause")
}

func TestObserved_WrapsErrorWithCheckNameSoFailuresAreSelfIdentifying(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	wrapped := checks.Observed("disk", errCheck(errors.New("io timeout")), bus)

	err := wrapped(t.Context())
	_ = drain()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "check disk:",
		"Observed must prefix the error with the check name so a single log line identifies which check failed")
	assert.Contains(t, err.Error(), "io timeout",
		"Observed must keep the underlying message so root causes survive the wrapping")
}
