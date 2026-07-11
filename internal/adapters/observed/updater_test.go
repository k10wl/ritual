package observed_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/ports"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUpdater is a scriptable ports.UpdaterService inner for the decorator.
type fakeUpdater struct {
	up       ports.Update
	outdated bool
	checkErr error
	applyErr error
}

func (f fakeUpdater) Check(context.Context) (ports.Update, bool, error) {
	return f.up, f.outdated, f.checkErr
}
func (f fakeUpdater) Apply(context.Context, ports.Update) error { return f.applyErr }

func TestObservedUpdater_CheckOutdated_StartedThenInfo(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	u := observed.NewUpdater(fakeUpdater{up: ports.Update{Version: "2.1.0"}, outdated: true}, "2.0.0", bus)

	_, outdated, err := u.Check(t.Context())
	require.NoError(t, err)
	assert.True(t, outdated, "decorator must propagate the inner verdict unchanged")
	events := drain()

	require.Len(t, events, 2, "outdated check publishes exactly Started + Info, no Failed")
	_, ok := events[0].(observed.UpdateCheckStarted)
	assert.True(t, ok, "first event is the Preflight entry signal")
	info, ok := events[1].(observed.UpdateCheckInfo)
	require.True(t, ok)
	assert.Equal(t, "2.0.0", info.From)
	assert.Equal(t, "2.1.0", info.To)
	assert.True(t, info.Outdated)
}

func TestObservedUpdater_CheckError_PublishesFailedAndPropagates(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	boom := errors.New("r2: offline")
	u := observed.NewUpdater(fakeUpdater{checkErr: boom}, "2.0.0", bus)

	_, _, err := u.Check(t.Context())
	require.ErrorIs(t, err, boom, "decorator must not swallow the inner error")
	events := drain()

	require.Len(t, events, 3, "errored check publishes Started + Info(err) + Failed")
	info, ok := events[1].(observed.UpdateCheckInfo)
	require.True(t, ok)
	assert.Equal(t, boom, info.Err)
	failed, ok := events[2].(observed.UpdateFailed)
	require.True(t, ok)
	assert.Equal(t, "check", failed.Stage, "a failed check shares the single PhaseFailed pathway, tagged by stage")
}

func TestObservedUpdater_ApplyError_StartedInfoFailed(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	boom := errors.New("checksum mismatch")
	u := observed.NewUpdater(fakeUpdater{applyErr: boom}, "2.0.0", bus)

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0"})
	require.ErrorIs(t, err, boom)
	events := drain()

	require.Len(t, events, 3, "failed apply publishes Started + Info(err) + Failed")
	_, ok := events[0].(observed.UpdateApplyStarted)
	assert.True(t, ok, "Updating entry signal fires before the replace")
	failed, ok := events[2].(observed.UpdateFailed)
	require.True(t, ok)
	assert.Equal(t, "apply", failed.Stage)
}

func TestObservedUpdater_ApplyReturnsNil_NoFailed(t *testing.T) {
	bus := adapters.NewEventBus(16)
	drain := collectEvents(bus)
	u := observed.NewUpdater(fakeUpdater{}, "2.0.0", bus)

	require.NoError(t, u.Apply(t.Context(), ports.Update{Version: "2.1.0"}))
	events := drain()

	require.Len(t, events, 2, "a non-erroring apply (relaunch-less path) publishes Started + Info, no Failed")
	info, ok := events[1].(observed.UpdateApplyInfo)
	require.True(t, ok)
	assert.NoError(t, info.Err)
}
