package projection_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/sync"
	"ritual/internal/gui/projection"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu       atomic.Int32
	received []projection.ViewModel
}

func (r *recorder) Emit(vm projection.ViewModel) {
	r.mu.Add(1)
	r.received = append(r.received, vm)
}

func (r *recorder) count() int { return int(r.mu.Load()) }

type staticAddresses struct{ list []projection.JoinAddress }

func (s staticAddresses) Addresses() []projection.JoinAddress { return s.list }

func runProjection(t *testing.T, addrs projection.AddressProvider, publish func(bus ports.EventBus)) []projection.ViewModel {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(64)
	rec := &recorder{}
	p := projection.New(bus, rec, addrs)

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	waitFor(t, func() bool { return rec.count() >= 1 }, "initial snapshot")

	publish(bus)
	waitFor(t, func() bool { return rec.count() >= 2 }, "at least one post-publish emit")
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	return rec.received
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func last(vms []projection.ViewModel) projection.ViewModel { return vms[len(vms)-1] }

func TestProjection_InitialEmit_IsIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	p := projection.New(bus, rec, nil)

	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	waitFor(t, func() bool { return rec.count() >= 1 }, "initial snapshot")
	cancel()
	<-done

	require.GreaterOrEqual(t, len(rec.received), 1, "projection must emit an initial snapshot so the UI never sees an empty view")
	assert.Equal(t, projection.StageIdle, rec.received[0].Stage, "initial snapshot must be Idle — Start button screen")
}

func TestProjection_StateChangedToChecking_FlipsStageToDownloading(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageChecking, RunID: "r1"})
	})
	assert.Equal(t, projection.StageDownloading, last(vms).Stage, "Checking state must map to Downloading UI stage so the user sees a download screen during preflight checks")
	assert.Equal(t, "Checking…", last(vms).Label, "Checking stage must surface a 'Checking…' label to describe what's happening in plain English")
}

func TestProjection_StateChangedToRunning_FlipsStageAndLoadsAddresses(t *testing.T) {
	addrs := staticAddresses{list: []projection.JoinAddress{{Label: "localhost", Address: "127.0.0.1:25565"}}}
	vms := runProjection(t, addrs, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.StageRunning, final.Stage, "Running state must map to Running UI stage where the user sees IPs and Stop button")
	require.Len(t, final.Addresses, 1, "entering StageRunning must populate addresses via AddressProvider so users can copy a join address")
	assert.Equal(t, "127.0.0.1:25565", final.Addresses[0].Address, "address payload must carry the dial string verbatim from the provider so the UI can copy it")
}

func TestProjection_ServerReady_FlipsReadyLight(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning})
		bus.Publish(running.ServerReadyInfo{})
	})
	final := last(vms)
	assert.True(t, final.ReadyLight, "ServerReadyInfo must flip ReadyLight so the Running screen shows the ready indicator to the user")
	assert.Equal(t, "Ready", final.Label, "ReadyLight trigger must also update the label to 'Ready' so the UI has a human caption")
}

func TestProjection_SyncStageProgress_UpdatesBytesAndPercent(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(sync.SyncStageProgressInfo{
			File: "world.dat", FilesDone: 5, FilesTotal: 10,
			BytesDone: 50 * 1024 * 1024, BytesTotal: 100 * 1024 * 1024,
		})
	})
	final := last(vms)
	assert.Equal(t, 50, final.Progress, "50MB done out of 100MB total must compute to 50%% — the progress bar depends on this integer percent")
	assert.Equal(t, int64(50*1024*1024), final.BytesDone, "BytesDone must reflect the most recent SyncStageProgressInfo value so the 'X / Y MB' line is accurate")
	assert.Equal(t, int64(100*1024*1024), final.BytesTotal, "BytesTotal must reflect the most recent SyncStageProgressInfo value so the 'X / Y MB' line is accurate")
	assert.Equal(t, 5, final.FilesDone, "FilesDone must track SyncStageProgressInfo so per-file UI hints can render")
}

func TestProjection_StateChangedToBackup_FlipsStageToUploading(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageBackup})
	})
	final := last(vms)
	assert.Equal(t, projection.StageUploading, final.Stage, "Backup state must map to Uploading UI stage — user sees a single 'uploading' screen for the whole post-game persistence phase")
	assert.Equal(t, "Backing up…", final.Label, "Backup stage must carry a 'Backing up…' label so the user understands which post-game step is running")
}

func TestProjection_LockHeldInfo_RoutesToLockedStageWithHolder(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(acquiring.LockHeldInfo{Holder: "gaming-pc|12345"})
	})
	final := last(vms)
	assert.Equal(t, projection.StageLocked, final.Stage, "LockHeldInfo must flip the UI to the Locked stage — the friendly 'someone else is playing' screen")
	assert.Equal(t, "gaming-pc|12345", final.LockHolder, "LockHolder must carry the holder string verbatim so the UI can show who is playing")
}

func TestProjection_StatusFailed_FlipsStageToFailedWithErrorText(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(app.StatusChanged{Status: app.Failed, Err: errors.New("remote storage exploded")})
	})
	final := last(vms)
	assert.Equal(t, projection.StageFailed, final.Stage, "StatusChanged{Failed} must flip the UI to Failed stage so the red banner renders")
	assert.Equal(t, "remote storage exploded", final.ErrorText, "ErrorText must carry err.Error() verbatim for POC — the banner surfaces it to the user")
}

func TestProjection_StatusFailedAfterLockHeld_StaysOnLockedScreen(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(acquiring.LockHeldInfo{Holder: "other"})
		bus.Publish(app.StatusChanged{Status: app.Failed, Err: errors.New("already locked by other")})
	})
	final := last(vms)
	assert.Equal(t, projection.StageLocked, final.Stage, "after LockHeldInfo the subsequent StatusChanged{Failed} is redundant — UI must stay on the friendly Locked screen, not flip to a scary error banner")
	assert.Equal(t, "other", final.LockHolder, "LockHolder must survive across the follow-up StatusChanged{Failed} so the UI keeps showing who is playing")
}

func TestProjection_StatusDone_ResetsToIdle(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning})
		bus.Publish(app.StatusChanged{Status: app.Done})
	})
	final := last(vms)
	assert.Equal(t, projection.StageIdle, final.Stage, "successful Done terminal status must return the UI to Idle so the user can start another session")
	assert.Zero(t, final.Progress, "Done reset must clear residual progress so the Idle screen is clean")
	assert.Empty(t, final.ErrorText, "Done reset must clear any residual ErrorText so the Idle screen isn't haunted by a prior failure")
}

func TestProjection_Snapshot_ReturnsCurrentState(t *testing.T) {
	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	p := projection.New(bus, rec, nil)
	snap := p.Snapshot()
	assert.Equal(t, projection.StageIdle, snap.Stage, "Snapshot before Run must return Idle — consumers rely on this to render the first frame before the subscriber loop starts")
}
