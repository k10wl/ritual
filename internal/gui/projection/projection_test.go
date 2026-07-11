package projection_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/adapters/progress"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/running"
	"ritual/internal/gui/projection"
	"ritual/internal/subsystems/lifecycle"
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
	assert.Equal(t, projection.StageIdle, rec.received[0].Stage, "initial snapshot must be Idle so the dial shows the Start affordance")
	assert.Equal(t, projection.PhaseIdle, rec.received[0].Phase, "initial Phase must be Idle so the dial picks the play glyph + 'Start' copy")
}

func TestProjection_StateChangedToChecking_FlipsStageToDownloadingAndPhaseToDownloading(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageChecking, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.StageDownloading, final.Stage, "Checking maps to the downloading bucket so the dial shows the yellow prep ring")
	assert.Equal(t, projection.PhaseDownloading, final.Phase, "Checking is conceptually a downloading-phase precursor — frontend renders download glyph + ETA slot")
}

func TestProjection_StateChangedToAcquiring_StaysOnDownloadingBucketButFlipsPhaseToPreparing(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageAcquiring, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.StageDownloading, final.Stage, "Acquiring stays under the downloading bucket — color/dial bucket only flips on ServerReady")
	assert.Equal(t, projection.PhasePreparing, final.Phase, "Acquiring has no bytes flowing; Phase must be preparing so the dial swaps to brain-cog + 'Preparing…' and hides ETA")
}

func TestProjection_PullingApplyStarted_FlipsPhaseToPreparingWithinDownloadingBucket(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling, RunID: "r1"})
		bus.Publish(pulling.ApplyStartedInfo{})
	})
	final := last(vms)
	assert.Equal(t, projection.StageDownloading, final.Stage, "ApplyStartedInfo keeps the downloading bucket — apply is part of Pulling, not a new color phase")
	assert.Equal(t, projection.PhasePreparing, final.Phase, "ApplyStartedInfo means network bytes are done; Phase flips to preparing so the dial swaps glyph from download → brain-cog and drops the (now lying) ETA")
}

func TestProjection_StateChangedToRunning_StaysOnDownloadingBucketUntilServerReady(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning, RunID: "r1"})
	})
	final := last(vms)
	assert.Equal(t, projection.StageDownloading, final.Stage, "StageRunning entry must NOT flip the dial to the running bucket until the server actually accepts connections — design-log/017 ServerReady gate")
	assert.Equal(t, projection.PhasePreparing, final.Phase, "while the server is booting, Phase reads preparing so the dial keeps brain-cog + 'Preparing…' and ETA stays hidden")
	assert.Empty(t, final.Addresses, "addresses must not appear until ServerReady — they're unreachable while the listener is still binding")
}

func TestProjection_ServerReady_FlipsBucketToRunningAndLoadsAddresses(t *testing.T) {
	addrs := staticAddresses{list: []projection.JoinAddress{{Label: "localhost", Address: "127.0.0.1:25565"}}}
	vms := runProjection(t, addrs, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning})
		bus.Publish(running.ServerReadyInfo{})
	})
	final := last(vms)
	assert.Equal(t, projection.StageRunning, final.Stage, "ServerReadyInfo gates the running bucket flip — only at this moment is the address actually reachable")
	assert.Equal(t, projection.PhasePlaying, final.Phase, "ServerReady flips Phase to playing so the dial picks the stop glyph + 'Ready to play' copy")
	require.Len(t, final.Addresses, 1, "ServerReady must trigger the address fetch — addresses appear precisely when they're useful to copy")
	assert.Equal(t, "127.0.0.1:25565", final.Addresses[0].Address, "address payload must carry the dial string verbatim so the frontend copy button has the raw value")
}

func TestProjection_ServerStopping_FlipsBucketToUploadingAndPhaseToWrappingAndClearsAddresses(t *testing.T) {
	addrs := staticAddresses{list: []projection.JoinAddress{{Label: "localhost", Address: "127.0.0.1:25565"}}}
	vms := runProjection(t, addrs, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning})
		bus.Publish(running.ServerReadyInfo{})
		bus.Publish(running.ServerStoppingInfo{})
	})
	final := last(vms)
	assert.Equal(t, projection.StageUploading, final.Stage, "ServerStoppingInfo must flip the bucket to uploading (teal) the moment the user-driven shutdown starts — not wait for Committing")
	assert.Equal(t, projection.PhaseWrapping, final.Phase, "ServerStoppingInfo flips Phase to wrapping so the dial swaps to unplug + 'Wrapping up…' and ETA stays hidden")
	assert.Empty(t, final.Addresses, "ServerStopping must clear the address list — the server is going offline, those addresses no longer connect to anything")
}

func TestProjection_StateChangedToCommitting_StaysInWrappingPhaseWithinUploadingBucket(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageCommitting})
	})
	final := last(vms)
	assert.Equal(t, projection.StageUploading, final.Stage, "Committing is the start of the save bucket — frontend sees teal final-color ring")
	assert.Equal(t, projection.PhaseWrapping, final.Phase, "Committing is local-only work with no bytes flowing; Phase stays wrapping so ETA stays hidden until Pushing fires")
}

func TestProjection_StateChangedToPushing_FlipsPhaseToSavingForETAVisibility(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
	})
	final := last(vms)
	assert.Equal(t, projection.StageUploading, final.Stage, "Pushing stays under the uploading bucket")
	assert.Equal(t, projection.PhaseSaving, final.Phase, "Pushing has bytes flowing out — Phase=saving lets the frontend show the upload glyph + ETA")
}

func TestProjection_StateChangedToUnlocking_StaysInSavingPhase(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageUnlocking})
	})
	final := last(vms)
	assert.Equal(t, projection.StageUploading, final.Stage, "Unlocking stays in the uploading bucket — the user reads it as part of 'saving' housekeeping")
	assert.Equal(t, projection.PhaseSaving, final.Phase, "Unlocking is part of the saving phase tail; frontend detects sub emptiness via bytesDone>=bytesTotal, not Phase change")
}

func TestProjection_LockHeldInfo_StoresHolderWithoutFlippingStageYet(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(acquiring.LockHeldInfo{Holder: "gaming-pc|12345"})
	})
	final := last(vms)
	assert.Equal(t, "gaming-pc|12345", final.LockHolder, "LockHeldInfo must record the holder string so a subsequent Failed status can render the friendly lock-conflict copy on the frontend")
	// Stage/Phase remain whatever they were before the conflict — the
	// failure transition is owned by lifecycle.Failed, not the lock event
	// itself. Design-log/017 folded PhaseLocked into PhaseFailed.
}

func TestProjection_LockHeldThenFailed_RoutesToFailedStageWithHolder(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(acquiring.LockHeldInfo{Holder: "alice"})
		bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed, Err: errors.New("already locked by alice")})
	})
	final := last(vms)
	assert.Equal(t, projection.StageFailed, final.Stage, "Lock conflicts route through Failed under design-log/017 — there is no separate StageLocked")
	assert.Equal(t, projection.PhaseFailed, final.Phase, "Phase=failed lets the dial pick its fail visual; frontend renders friendly '{holder} is playing' copy because LockHolder is populated")
	assert.Equal(t, "alice", final.LockHolder, "LockHolder must survive the StatusChanged{Failed} so the frontend has the holder string for friendly copy")
}

func TestProjection_StatusFailed_FlipsStageAndPhaseToFailedWithErrorText(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed, Err: errors.New("remote storage exploded")})
	})
	final := last(vms)
	assert.Equal(t, projection.StageFailed, final.Stage, "StatusChanged{Failed} must flip Stage to Failed so the dial reads its red-overlay branch")
	assert.Equal(t, projection.PhaseFailed, final.Phase, "Phase=failed pairs with Stage=failed; frontend uses Phase as its single dispatch key")
	assert.Equal(t, "remote storage exploded", final.ErrorText, "ErrorText must carry err.Error() verbatim — design-log/017 fail-copy lives entirely in the frontend, but the underlying error string is still useful for power-user log surfaces")
}

func TestProjection_StatusDone_ResetsToIdle(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageRunning})
		bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})
	})
	final := last(vms)
	assert.Equal(t, projection.StageIdle, final.Stage, "successful Done terminal status must return the UI to Idle so the user can start another session")
	assert.Equal(t, projection.PhaseIdle, final.Phase, "Done must reset Phase to idle so the dial returns to the Start affordance")
	assert.Zero(t, final.Progress, "Done reset must clear residual progress so the Idle screen is clean")
	assert.Empty(t, final.ErrorText, "Done reset must clear any residual ErrorText so the Idle screen isn't haunted by a prior failure")
}

func TestProjection_StatusDismissed_ResetsFailureBackToIdle(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed, Err: errors.New("boom")})
		bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Dismissed})
	})
	final := last(vms)
	assert.Equal(t, projection.StageIdle, final.Stage, "Dismissed must return to Idle — design-log/017 dismiss-to-idle contract")
	assert.Equal(t, projection.PhaseIdle, final.Phase, "Dismissed must reset Phase to idle so the dial drops the failure overlay and shows the Start affordance again")
	assert.Empty(t, final.ErrorText, "Dismissed must clear ErrorText so a subsequent run doesn't render stale failure copy")
}

func TestProjection_TickInPullingStage_UpdatesBytesDone(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(progress.Tick{Remote: progress.Side{Down: progress.Stream{Data: 500}}})
	})
	final := last(vms)
	assert.Equal(t, int64(500), final.BytesDone, "progress.Tick during Pulling must propagate Remote.Down.Data (logical bytes) into ViewModel.BytesDone so the arc fills as bytes stream down — Data is uncompressed so numerator matches PlanInfo.BytesTotal's denominator")
}

func TestProjection_PlanInfoDuringPulling_PopulatesBytesTotalAndFilesTotal(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 6_000, FilesTotal: 3})
	})
	final := last(vms)
	assert.Equal(t, int64(6000), final.BytesTotal, "PlanInfo.BytesTotal must populate ViewModel.BytesTotal so the arc denominator is non-zero before the first Tick")
	assert.Equal(t, 3, final.FilesTotal, "PlanInfo.FilesTotal must populate ViewModel.FilesTotal so the frontend has the file count for the under-block caption")
	assert.Equal(t, 0, final.Progress, "Non-empty plan must leave Progress at 0 so arcFromBytes derives the arc from bytesDone/bytesTotal (the live counter), not from a stale anchor")
}

func TestProjection_PlanInfoWithZeroDelta_AnchorsProgressTo100(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 0, FilesTotal: 0})
	})
	final := last(vms)
	assert.Equal(t, int64(0), final.BytesTotal, "Empty-delta plan must carry BytesTotal=0 through — the projection does not invent a denominator the runtime won't fill")
	assert.Equal(t, 0, final.FilesTotal, "FilesTotal=0 mirrors BytesTotal=0 — nothing will move, nothing to count")
	assert.Equal(t, 100, final.Progress, "Empty-delta plan must anchor Progress=100 so arcFromBytes resolves the dial to complete-on-arrival when bytesTotal is zero — without this anchor the dial would stick at 0%% for the duration of the transfer stage (no Ticks fire when no blob streams). Design-log/019.")
}

func TestProjection_TickInPullingStage_PopulatesSpeedMbps(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(progress.Tick{Remote: progress.Side{Down: progress.Stream{Data: 500, Average: 12.3}}})
	})
	final := last(vms)
	assert.InDelta(t, 12.3, final.SpeedMbps, 0.001, "SpeedMbps must read Down.Average — 5-second rolling wire rate (curl --progress-bar convention, design-log/001 §Refinement). Frontend uses this for the speed line under the dial.")
}

func TestProjection_TickInPushingStage_NoPlanInfo_FallsBackToUpData(t *testing.T) {
	// When FilesTotal is zero (no PlanInfo published yet), push falls back to
	// s.Data so the bar is never stuck at 0. Normal sessions publish PlanInfo
	// before the first tick; this guards the degenerate path.
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 700, Average: 8.0}}})
	})
	final := last(vms)
	assert.Equal(t, int64(700), final.BytesDone, "fallback: no FilesTotal yet → BytesDone reads Up.Data so the bar is never frozen at zero on the degenerate no-plan-info path")
	assert.InDelta(t, 8.0, final.SpeedMbps, 0.001, "SpeedMbps during Pushing reads Up.Average — the user sees live upload speed under the dial")
}

func TestProjection_TickDuringCommitting_DoesNotMutateBytesDone(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 100, Average: 5.0}}})
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageCommitting})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 200, Average: 9.0}}})
	})
	final := last(vms)
	assert.Equal(t, int64(100), final.BytesDone, "BytesDone must freeze at the last Pushing-stage value once Committing starts — the bar must not keep moving on stale upload counters during local refs work")
}

func TestProjection_LogicalDrivesProgress_AverageDrivesSpeed(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 1024 * 1024 * 1024, FilesTotal: 100})
		bus.Publish(progress.Tick{
			Remote: progress.Side{
				Down: progress.Stream{
					Data: 200 * 1024 * 1024, Transfer: 80 * 1024 * 1024,
					Instant: 180, Average: 42, Smoothed: 38, DataAverage: 110,
				},
			},
		})
	})
	final := last(vms)
	assert.Equal(t, int64(200*1024*1024), final.BytesDone, "BytesDone must read from Down.Data (LOGICAL bytes) so it lines up with PlanInfo.BytesTotal (also logical) — the bar would lie about completion if numerator/denominator used different units")
	assert.Equal(t, int64(1024*1024*1024), final.BytesTotal, "BytesTotal must carry the logical-byte plan budget so progress = Down.Data / BytesTotal is the user-facing 'fraction of my world transferred'")
	assert.InDelta(t, 42.0, final.SpeedMbps, 0.001, "SpeedMbps must equal Down.Average — the 5-second rolling WIRE rate the frontend renders as the speed line")
	assert.InDelta(t, 110.0, final.LogicalMbps, 0.001, "LogicalMbps must equal Down.DataAverage — the logical (decompress/install) rate; distinct from SpeedMbps, often higher on compressible payloads")
}

func TestProjection_ZeroRateTickMidPush_MarksStalled(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 2})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 700, Instant: 8.0, Average: 8.0}}})
		// Heartbeat during an R2 stall: counters frozen, zero "now" rate, no ops
		// confirmed yet — bytes still owed (0 ops × 500 avg = 0 < 1000).
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 700, Instant: 0, Average: 4.0}}})
	})
	final := last(vms)
	assert.True(t, final.Stalled, "a heartbeat Tick with Up.Instant==0 mid-Pushing (bytes still owed) must set Stalled so the frontend renders 'Stalled — waiting on R2…' instead of a silently frozen dial — design-log/022 #2")
	assert.Equal(t, int64(0), final.BytesDone, "BytesDone for push comes from Ops.Done × avg_blob_size (confirmed R2 deliveries); no ops have completed yet so BytesDone stays 0 while the link is stalled")
}

func TestProjection_BytesResumeAfterStall_ClearsStalled(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 2})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 700, Instant: 0, Average: 4.0}}})   // stalled
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 900, Instant: 6.0, Average: 5.0}}}) // resumed
	})
	final := last(vms)
	assert.False(t, final.Stalled, "once bytes flow again (Instant>0) Stalled must clear so the caption returns to live throughput")
}

func TestProjection_FinalZeroDeltaAtCompletion_NotStalled(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 2})
		// Transfer completes: all 2 ops confirmed by R2, Instant==0 (no more wire
		// bytes). BytesDone = 2 ops × 500 avg = 1000 = BytesTotal → not a stall.
		bus.Publish(progress.Tick{
			Remote: progress.Side{Up: progress.Stream{Data: 1000, Instant: 0, Average: 0}},
			Ops:    progress.OpsTally{Done: 2},
		})
	})
	final := last(vms)
	assert.False(t, final.Stalled, "a zero-rate Tick once all ops are confirmed (BytesDone>=BytesTotal) is the completion marker, not a stall — Stalled must stay false so the dial shows complete, not 'waiting on R2'")
}

func TestProjection_StageChange_ClearsStalled(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 2})
		bus.Publish(progress.Tick{Remote: progress.Side{Up: progress.Stream{Data: 700, Instant: 0, Average: 4.0}}}) // stalled
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageUnlocking})                                             // moved on
	})
	final := last(vms)
	assert.False(t, final.Stalled, "leaving the transfer stage must clear Stalled — a stall caption must never bleed into the next beat (Unlocking/Retaining)")
}

func TestProjection_Snapshot_ReturnsCurrentState(t *testing.T) {
	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	p := projection.New(bus, rec, nil)
	snap := p.Snapshot()
	assert.Equal(t, projection.StageIdle, snap.Stage, "Snapshot before Run must return Idle — consumers rely on this to render the first frame before the subscriber loop starts")
	assert.Equal(t, projection.PhaseIdle, snap.Phase, "initial Phase must be Idle so the dial chooses the play glyph")
}

// --- design-log/028: stable transfer ETA (EtaSeconds) ---

func TestProjection_FirstTickOfBeat_LeavesEtaZeroToAnchor(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 1000, FilesTotal: 1})
		bus.Publish(progress.Tick{Elapsed: 1 * time.Second, Remote: progress.Side{Down: progress.Stream{Data: 0}}})
	})
	final := last(vms)
	assert.Equal(t, int64(0), final.EtaSeconds, "the first Tick of a beat only anchors elapsed+bytes; EtaSeconds stays 0 so the dial shows the decoder placeholder, not a number derived from a single sample (design-log/028 §Q5)")
}

func TestProjection_SecondTick_DerivesEtaFromBeatWideAverage(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 1000, FilesTotal: 1})
		bus.Publish(progress.Tick{Elapsed: 1 * time.Second, Remote: progress.Side{Down: progress.Stream{Data: 0}}})
		// +1s, +200B → beat-wide rate 200 B/s, 800 B remaining → 4s.
		bus.Publish(progress.Tick{Elapsed: 2 * time.Second, Remote: progress.Side{Down: progress.Stream{Data: 200}}})
	})
	final := last(vms)
	assert.Equal(t, int64(4), final.EtaSeconds, "EtaSeconds = remaining / beat-wide rate = 800 / (200B over 1s) = 4s; derived from the session average, never the rolling Average/DataAverage that swings (design-log/028 §Q2)")
}

func TestProjection_EtaNeverClimbsWithinABeat_MonotonicGuard(t *testing.T) {
	// FilesTotal=10 so avg=100B: 5 ops = 500B, gives integer progress mid-transfer.
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 10})
		bus.Publish(progress.Tick{Elapsed: 1 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 0}}})
		// Fast start: 5 ops confirmed at 2s → BytesDone=500, rate=500 B/s, 500 remaining → 1s.
		bus.Publish(progress.Tick{Elapsed: 2 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 500}}, Ops: progress.OpsTally{Done: 5}})
		// Link nearly dies: no new ops by 12s → beat-wide rate collapses; the naive
		// estimate would balloon to ~11s. The guard must hold it at the prior 1s.
		bus.Publish(progress.Tick{Elapsed: 12 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 510}}, Ops: progress.OpsTally{Done: 5}})
	})
	assert.Equal(t, int64(1), vms[len(vms)-2].EtaSeconds, "after the fast first second the estimate is 1s")
	assert.Equal(t, int64(1), last(vms).EtaSeconds, "a mid-beat slowdown must NOT push the estimate up — within a fixed plan EtaSeconds is monotone non-increasing so the countdown never reads as broken (design-log/028 §Q10)")
}

func TestProjection_PlanInfoReBaselinesEta_NotClampedByPriorBeat(t *testing.T) {
	// FilesTotal=10/50 keeps avg=100B for both plans so ops → bytes is simple arithmetic.
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 1000, FilesTotal: 10})
		bus.Publish(progress.Tick{Elapsed: 1 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 0}}})
		// 5 ops at 2s → BytesDone=500, rate=500 B/s, remaining=500 → ETA=1s.
		bus.Publish(progress.Tick{Elapsed: 2 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 500}}, Ops: progress.OpsTally{Done: 5}}) // eta now 1s
		// A new, larger plan arrives: there is literally more work, so the guard
		// must release and the beat re-anchor rather than clamp to the old 1s.
		bus.Publish(ritual.PlanInfo{Operation: "push", BytesTotal: 5000, FilesTotal: 50})
		// 5 ops at 3s → BytesDone=500, anchor for new beat.
		bus.Publish(progress.Tick{Elapsed: 3 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 500}}, Ops: progress.OpsTally{Done: 5}}) // anchor
		// 6 ops at 4s → BytesDone=600, dt=1s, flowed=100 B/s, remaining=4400 → 44s.
		bus.Publish(progress.Tick{Elapsed: 4 * time.Second, Remote: progress.Side{Up: progress.Stream{Data: 600}}, Ops: progress.OpsTally{Done: 6}}) // +100B/s, 4400 left → 44s
	})
	final := last(vms)
	assert.Equal(t, int64(44), final.EtaSeconds, "PlanInfo re-baselines the beat: 4400 remaining / 100 B/s = 44s. The monotonic guard must not clamp this to the prior beat's 1s — a bigger plan is genuinely more time (design-log/028 §Q4)")
}

func TestProjection_EmptyPlan_KeepsEtaZero(t *testing.T) {
	vms := runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePulling})
		bus.Publish(ritual.PlanInfo{Operation: "pull", BytesTotal: 0, FilesTotal: 0})
		bus.Publish(progress.Tick{Elapsed: 1 * time.Second, Remote: progress.Side{Down: progress.Stream{Data: 0}}})
		bus.Publish(progress.Tick{Elapsed: 2 * time.Second, Remote: progress.Side{Down: progress.Stream{Data: 0}}})
	})
	final := last(vms)
	assert.Equal(t, int64(0), final.EtaSeconds, "an empty delta (design-log/019) has no bytes to time; EtaSeconds stays 0 so the frontend shows no estimate instead of dividing by zero")
}
