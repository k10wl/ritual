package projection_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"ritual/internal/adapters/observed"
	"ritual/internal/core/ports"
	"ritual/internal/gui/projection"
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

func TestProjection_UpdateFailed_RoutesToFailed(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(observed.UpdateCheckStarted{})
		bus.Publish(observed.UpdateFailed{Stage: "apply", Err: errors.New("checksum mismatch")})
	})
	got := last(vms)
	assert.Equal(t, projection.StageFailed, got.Stage, "best-effort mandatory: failure uses 017's single failed pathway")
	assert.Equal(t, projection.PhaseFailed, got.Phase)
	assert.Equal(t, "checksum mismatch", got.ErrorText)
}
