package projection_test

import (
	"errors"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/ports"
	"ritual/internal/gui/projection"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjection_UpdateCheckStarted_GraysToPreflight(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
	})
	got := last(vms)
	assert.Equal(t, projection.StagePreflight, got.Stage, "launch boots the gray inert bucket")
	assert.Equal(t, projection.PhasePreflight, got.Phase, "dial shows 'Checking for updates···'")
}

func TestProjection_UpdateCheckUpToDate_WakesToIdle(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
		bus.Publish(observed.UpdateCheckInfo{From: "2.0.0", To: "2.0.0", Outdated: false})
	})
	got := last(vms)
	assert.Equal(t, projection.StageIdle, got.Stage, "up-to-date wakes straight to IDLE")
	assert.Equal(t, projection.PhaseIdle, got.Phase)
}

func TestProjection_UpdateCheckOutdated_RemembersTarget(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
		bus.Publish(observed.UpdateCheckInfo{From: "2.0.0", To: "2.1.0", Outdated: true})
	})
	got := last(vms)
	assert.Equal(t, "2.1.0", got.TargetVersion, "outdated check records the target for the 'Updating → vN' copy")
	assert.Equal(t, projection.StagePreflight, got.Stage, "stays gray until the Updating beat begins")
}

func TestProjection_UpdateApplyStarted_ShowsUpdating(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateApplyStarted{Version: "2.1.0"})
	})
	got := last(vms)
	assert.Equal(t, projection.StagePreflight, got.Stage, "Updating stays in the gray bucket")
	assert.Equal(t, projection.PhaseUpdating, got.Phase)
	assert.Equal(t, "2.1.0", got.TargetVersion)
}

func TestProjection_UpdateFailed_DropsToIdleWithHint(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
		bus.Publish(observed.UpdateFailed{Stage: "apply", Err: errors.New("checksum mismatch")})
	})
	got := last(vms)
	assert.Equal(t, projection.StageIdle, got.Stage, "update failures are non-blocking: drop to idle so the user can keep using the app")
	assert.Equal(t, projection.PhaseIdle, got.Phase)
	assert.NotEmpty(t, got.ErrorText, "hint text surfaces under the dial so the user knows to retry via Advanced")
}

func TestProjection_UpdateCheckStarted_ClearsPriorHint(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
		bus.Publish(observed.UpdateFailed{Stage: "check", Err: errors.New("timeout")})
		bus.Publish(observed.UpdateCheckStarted{}) // manual retry
	})
	got := last(vms)
	assert.Equal(t, projection.StagePreflight, got.Stage)
	assert.Empty(t, got.ErrorText, "starting a new check clears the prior failure hint")
}
