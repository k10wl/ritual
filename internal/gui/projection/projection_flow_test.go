package projection_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/gui/projection"
)

// Direction-aware dial (design-log/031 addendum): a Download is one honest
// `downloading` beat and an Upload is one `saving` beat across every shared
// stage — never the session's Preparing / Wrapping beats.

func TestProjection_DownloadFlow_RetainingStaysDownloadingNotSaving(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowDownload})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRetaining, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseDownloading, final.Phase,
		"a Download's trailing Retaining is local housekeeping — it must stay on the ⬇ downloading beat, not flip to the ⬆ saving glyph")
	assert.Equal(t, projection.StageDownloading, final.Stage,
		"a Download never enters the uploading bucket — nothing is pushed")
}

func TestProjection_DownloadFlow_ApplyStartedDoesNotFlipToPreparing(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowDownload})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling, RunID: "r1"})
		bus.Publish(pulling.ApplyStartedInfo{})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseDownloading, final.Phase,
		"the download→preparing flip is a server-prep beat; a Download has no server, so ApplyStarted must leave it on the downloading beat")
}

func TestProjection_UploadFlow_CheckingIsSavingNotDownloading(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowUpload})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageChecking, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseSaving, final.Phase,
		"an Upload's Checking must read as the ⬆ saving beat — it must not start on the ⬇ download glyph for a flow that never downloads")
	assert.Equal(t, projection.StageUploading, final.Stage,
		"an Upload sits in the uploading bucket from the first stage")
}

func TestProjection_UploadFlow_CommittingStaysSavingNotWrapping(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowUpload})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageCommitting, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseSaving, final.Phase,
		"an Upload's Committing must stay on the saving beat — the session's Wrapping copy ('Spinning down') assumes a server shutting down")
}

func TestProjection_SessionFlow_RetainingStillSaving(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRetaining, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseSaving, final.Phase,
		"the direction-aware override must not touch the session: its Retaining stays on the saving beat as before")
}

func TestProjection_DefaultFlow_CheckingIsDownloading(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageChecking, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.PhaseDownloading, final.Phase,
		"with no FlowStartedInfo the projection defaults to the session map — Checking is downloading")
}
