package projection

import (
	"context"
	"errors"
	"ritual/internal/adapters/observed"
	"ritual/internal/adapters/progress"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/relocating"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
	"sync"
	"time"
)

// Emitter receives a snapshot after every fold that changes the ViewModel.
// cmd/gui implements this with a Wails typed event; tests use a slice.
type Emitter interface {
	Emit(vm ViewModel)
}

// AddressProvider returns the list of join addresses to render on the
// Playing phase. Called once per ServerReady transition.
type AddressProvider interface {
	Addresses() []JoinAddress
}

// Estimator supplies history-derived prep/wrap duration estimates
// (internal/subsystems/preprundup, design-log/058). Optional — nil-safe,
// same shape as AddressProvider — so a caller with no history substrate
// wired up yet still gets a working projection with both ETAs at 0.
type Estimator interface {
	PrepEta() time.Duration
	WrapEta() time.Duration
}

// Projection subscribes to the bus and folds events into a single ViewModel.
// pipelineStage tracks the most recent ritual stage name so onTick can gate
// network-progress writes: a late progress.Tick arriving after the pipeline
// moved on to Committing must not overwrite stage-set values.
// everReady tracks whether the current run has reached ServerReady; gates
// the Running-stage phase transitions (preparing → playing → wrapping).
type Projection struct {
	ch        <-chan ports.Event
	unsub     func()
	emitter   Emitter
	publish   func(ports.Event)
	addresses AddressProvider
	// mu guards state (and nextSeq, always touched together with it) against
	// the pre-existing cross-goroutine race between Run's single mutating
	// goroutine and Snapshot, which ControlService.GetSnapshot calls from
	// whatever goroutine services that RPC. Only Run ever mutates state — mu
	// is single-writer/many-reader, not mutual exclusion between writers.
	mu            sync.RWMutex
	state         ViewModel
	pipelineStage string
	everReady     bool
	// activeFlow disambiguates the sync flows from the session (design-log/031):
	// Download and Upload reuse the session's stage nodes, so onStateChanged
	// keys on this — not stage name alone — to render Download as one honest
	// "downloading" beat and Upload as one "saving" beat. Set by FlowStartedInfo,
	// reset to FlowSession on the idle transition.
	activeFlow ritual.Flow

	// ETA beat anchors. A "beat" is one transfer window with a fixed plan; ETA
	// is the beat-wide average (flowed since beat start / elapsed since beat
	// start), which by construction cannot swing the way a 5-second rolling
	// rate does (design-log/028 §Q2). etaBeatStarted is false until the first
	// Tick of a beat anchors etaBeatElapsed (the cumulative ticker Elapsed at
	// that moment) and etaBeatBytes (logical bytes already counted — non-zero
	// on resume). resetEtaBeat clears them on every stage change and PlanInfo so
	// a new plan re-baselines and the monotonic guard never bleeds across beats.
	etaBeatStarted bool
	etaBeatElapsed time.Duration
	etaBeatBytes   int64

	// Per-flow baselines for BytesDone (design-log/050 §A). The underlying
	// counters (progress.Tick.Remote.Down.Data / .Ops.Done) are process-
	// lifetime cumulative atomics (internal/adapters/counter.go) — never
	// reset between flows — while PlanInfo.BytesTotal is freshly scoped to
	// just the current plan's delta. Without a baseline, a second pull/push
	// inside one running process (chained dial restarts) opens with
	// BytesDone already carrying every byte moved by the *previous* flow,
	// which can exceed the new, smaller BytesTotal before anything has
	// streamed. lastRemoteDown/lastRemoteOps are updated on every Tick
	// regardless of pipelineStage (onTick's stage gate returns early
	// otherwise) so a baseline is available the instant the stage flips —
	// a Tick can arrive before that flow's own PlanInfo (see design-log/050
	// §A Q5). pullBaseline/pushOpsBaseline are captured in onStateChanged on
	// entry to StagePulling/StagePushing.
	lastRemoteDown  int64
	lastRemoteOps   int64
	pullBaseline    int64
	pushOpsBaseline int64

	// playingStartedAt anchors the backend-driven uptime ticker (design-log/050):
	// set on running.ServerReadyInfo, read by Run's 1Hz uptimeTicker while
	// Phase stays PhasePlaying. The frontend runs no clock of its own — the
	// displayed uptime only changes because the backend pushed a new value.
	playingStartedAt time.Time

	// estimator supplies history-derived durations (design-log/058); nil
	// when no history substrate is wired up (tests, or a caller that opts
	// out), in which case every *EtaEstimate stays 0 and both countdown
	// fields never populate — the frontend's zero-fallback copy covers it.
	//
	// prepEtaEstimate is snapshotted once per FlowSession run (at
	// FlowStartedInfo) rather than read live at Acquiring, so it's already
	// available to add onto the download beat's EtaSeconds (design-log/058
	// §Q2) before the prep beat itself even begins. prepEtaStartedAt anchors
	// the countdown once StateChangedInfo{To: Acquiring} actually arrives;
	// zero means "countdown not active" (guards the Run ticker branch).
	//
	// wrapEtaEstimate is fetched at ServerReadyInfo — a full beat ahead of
	// ServerStoppingInfo, mirroring how prepEtaEstimate is fetched a full
	// beat ahead of Acquiring — so a concurrent history write can't leave it
	// stale mid-wrap.
	estimator        Estimator
	prepEtaEstimate  time.Duration
	prepEtaStartedAt time.Time
	wrapEtaEstimate  time.Duration
	wrapEtaStartedAt time.Time

	// nextSeq stamps a strictly increasing ViewModel.Seq on every emit
	// (design-log/051 Q11). ExecuteScript(script, nil) — the Wails/WebView2
	// call that delivers each emit to the frontend — is fire-and-forget and
	// does not guarantee execution order matches submission order under load;
	// a stale duplicate can finish executing after a later, correct snapshot
	// and silently overwrite it. Seq lets the frontend detect and drop any
	// snapshot older than what it already applied, without needing an
	// acknowledgment/retry round-trip.
	nextSeq int64
}

// New subscribes to bus immediately and returns a Projection ready for Run.
// Subscribing here (not in Run) avoids the race where callers publish before
// the Run goroutine manages to attach. AddressProvider and Estimator may
// both be nil; Addresses stays empty and both ETA fields stay 0 if so.
func New(bus ports.EventBus, emitter Emitter, addresses AddressProvider, estimator Estimator) *Projection {
	ch, unsub := bus.Subscribe()
	return &Projection{
		ch:         ch,
		unsub:      unsub,
		emitter:    emitter,
		publish:    bus.Publish,
		addresses:  addresses,
		estimator:  estimator,
		state:      ViewModel{Stage: StageIdle, Phase: PhaseIdle},
		activeFlow: ritual.FlowSession,
	}
}

// Snapshot returns the current view model. Used by ControlService.GetSnapshot
// so the frontend has a value to render before the first Emit arrives. Called
// from whatever goroutine services that RPC — guarded by mu against Run's
// concurrent mutation.
func (p *Projection) Snapshot() ViewModel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// emit stamps the next sequence number onto p.state (design-log/051 Q11) and
// dispatches it through both the Wails-facing emitter and the bus (for the
// file logger). The single call site here is what guarantees Seq is unique
// and strictly increasing — every place that used to call
// `p.emitter.Emit(p.state); p.publish(Snap{p.state})` directly now goes
// through this instead. Takes a copy under mu so Emit/publish (which may be
// slow — Wails IPC, subscriber fan-out) never run while holding the lock.
func (p *Projection) emit() {
	p.mu.Lock()
	p.nextSeq++
	p.state.Seq = p.nextSeq
	vm := p.state
	p.mu.Unlock()
	p.emitter.Emit(vm)
	p.publish(Snap{vm})
}

// Run blocks until ctx is cancelled or the bus closes. Publishes an initial
// snapshot immediately so consumers never see an empty view.
//
// uptimeTicker (design-log/050) is the ONLY reason a fresh ViewModel is ever
// emitted without a bus event driving it: the "Live" uptime caption must
// visibly advance once a second, and the frontend runs no clock of its own,
// so the backend has to be the one ticking. It's a no-op outside
// PhasePlaying — cheap to leave running unconditionally rather than starting
// and stopping it per-run.
func (p *Projection) Run(ctx context.Context) {
	defer p.unsub()
	p.emit()

	uptimeTicker := time.NewTicker(time.Second)
	defer uptimeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-uptimeTicker.C:
			if !p.tickEtaCountdowns() {
				continue
			}
			p.emit()
		case evt, ok := <-p.ch:
			if !ok {
				return
			}
			p.mu.Lock()
			changed := p.fold(evt)
			p.mu.Unlock()
			if changed {
				p.emit()
			}
		}
	}
}

// tickEtaCountdowns is the uptimeTicker's 1Hz callback (design-log/050
// pattern, extended by design-log/058): advances whichever backend-driven
// countdown applies to the current phase — UptimeSeconds while
// PhasePlaying, PrepEtaSeconds while PhasePreparing, WrapEtaSeconds while
// PhaseWrapping — and reports whether anything changed so Run only emits
// when a ticker branch actually applies. A phase with no active countdown
// (e.g. PhaseDownloading, PhaseIdle) is a no-op tick, same cost the
// original PhasePlaying-only gate already paid.
func (p *Projection) tickEtaCountdowns() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.state.Phase == PhasePlaying:
		p.state.UptimeSeconds = int64(time.Since(p.playingStartedAt).Seconds())
	case p.state.Phase == PhasePreparing && !p.prepEtaStartedAt.IsZero():
		p.state.PrepEtaSeconds = remainingSeconds(p.prepEtaEstimate, p.prepEtaStartedAt)
		p.state.Progress = prepProgress(int64(p.prepEtaEstimate.Seconds()), p.state.PrepEtaSeconds)
	case p.state.Phase == PhaseWrapping && !p.wrapEtaStartedAt.IsZero():
		p.state.WrapEtaSeconds = remainingSeconds(p.wrapEtaEstimate, p.wrapEtaStartedAt)
	default:
		return false
	}
	return true
}

// remainingSeconds floors at 0 rather than going negative — once the real
// event (ServerReadyInfo / StatusChanged{Done}) fires the beat ends anyway,
// so a countdown that runs past its estimate just sits at 0 until then.
func remainingSeconds(estimate time.Duration, startedAt time.Time) int64 {
	remaining := estimate - time.Since(startedAt)
	if remaining < 0 {
		return 0
	}
	return int64(remaining.Seconds())
}

// fold mutates p.state based on evt and reports whether the event was
// relevant. Irrelevant events return false to avoid emitting duplicates.
func (p *Projection) fold(evt ports.Event) bool {
	switch e := evt.(type) {
	case ritual.FlowStartedInfo:
		// Internal-only: records which flow is in flight for onStateChanged.
		// Paints nothing itself, so report no change (no redundant emit).
		p.activeFlow = e.Flow
		p.onFlowStarted(e.Flow)
		return false
	case ritual.StateChangedInfo:
		p.onStateChanged(e.To)
	case pulling.ApplyStartedInfo:
		// Pulling's network phase is done; apply is running invisibly. Phase
		// flips from `downloading` to `preparing` so the dial swaps glyph
		// (download → brain-cog) and drops ETA. See design-log/017 §Q1. Only
		// the session does this server-prep flip — a Download stays in its one
		// honest `downloading` beat (design-log/031).
		if p.activeFlow == ritual.FlowSession && p.pipelineStage == ritual.StagePulling {
			p.state.Phase = PhasePreparing
		}
	case running.ServerReadyInfo:
		// Address reachable. Flip bucket to running + phase to playing; load
		// addresses once now that the server is actually listening.
		p.everReady = true
		p.state.Stage = StageRunning
		p.state.Phase = PhasePlaying
		// Anchor for the backend-driven uptime ticker (design-log/050) — Run's
		// 1Hz uptimeTicker reads time.Since(playingStartedAt) on every tick
		// while Phase stays PhasePlaying. No frontend clock of any kind.
		p.playingStartedAt = time.Now()
		if p.addresses != nil {
			p.state.Addresses = p.addresses.Addresses()
		}
		p.onPrepBeatOver()
	case running.ServerStoppingInfo:
		// User-driven shutdown begun. Flip bucket to uploading + phase to
		// wrapping; addresses are no longer useful so drop them.
		p.state.Stage = StageUploading
		p.state.Phase = PhaseWrapping
		p.state.Addresses = nil
		p.state.UptimeSeconds = 0
		// Seed the backend-driven wrap countdown (design-log/058).
		p.wrapEtaStartedAt = time.Now()
		p.state.WrapEtaSeconds = int64(p.wrapEtaEstimate.Seconds())
	case acquiring.LockHeldInfo:
		// Lock-held merely records the holder; the actual UI transition fires
		// when lifecycle reports Failed (the acquiring stage routes to failed
		// after LockHeldInfo). Frontend reads lockHolder during PhaseFailed
		// to pick the friendly "{holder} is playing" copy. Design-log/017
		// folded PhaseLocked into PhaseFailed for a single failure pathway.
		p.state.LockHolder = e.Holder
	case progress.Tick:
		return p.onTick(e)
	case ritual.PlanInfo:
		p.onPlanInfo(e)
	case lifecycle.StatusChanged:
		p.onStatusChanged(e)
	default:
		if p.foldRelocate(evt) {
			return true
		}
		// Autoupdate (design-log/037) and anything else the projection ignores.
		return p.foldUpdate(evt)
	}
	return true
}

// onPlanInfo folds a fresh ritual.PlanInfo — split out of fold to keep fold's
// own cyclomatic complexity under budget (gocyclo).
func (p *Projection) onPlanInfo(e ritual.PlanInfo) {
	p.state.BytesTotal = e.BytesTotal
	p.state.FilesTotal = e.FilesTotal
	// A fresh plan means more (or different) work: re-baseline the ETA beat
	// so the monotonic guard re-anchors against the new BytesTotal instead
	// of clamping to a stale estimate. Design-log/028 §Q4/Q10.
	p.resetEtaBeat()
	// Empty delta — everything already present at destination per the
	// pre-flight list (design-log/019). No Ticks will fire because no
	// blob streams, so without this anchor the bar would sit at 0/0 for
	// the duration of the transfer stage. Setting Progress = 100
	// here gives arcFromBytes a value to fall back on when bytesTotal
	// is zero, so the dial reads complete-on-arrival immediately.
	//
	// FlowSession with a real prep estimate (design-log/058 addendum): land
	// at prepSplitPercent instead of 100 — the download beat is "complete"
	// but the ring still owes the prep beat's 20%, and prepProgress picks
	// up from here once Acquiring seeds the countdown. No Tick will ever
	// correct this (empty delta means none fire), so this is the only
	// place that can anchor it.
	switch {
	case e.BytesTotal != 0 || e.FilesTotal != 0:
		p.state.Progress = 0
	case p.pipelineStage == ritual.StagePulling && p.activeFlow == ritual.FlowSession && p.prepEtaEstimate > 0:
		p.state.Progress = prepSplitPercent
	default:
		p.state.Progress = 100
	}
	// BytesTotal can change here independent of any Tick — the very first
	// render after a plan arrives, before the first Tick fires. Refresh so
	// SizeTotalText isn't stale against the new plan (design-log/050).
	p.refreshFormattedFields()
}

// foldUpdate folds the observed.Update* stream into the gray Preflight dial
// (design-log/037). Split out of fold so each stays under the complexity
// budget. Returns whether the event changed the ViewModel; unknown events
// return false (no redundant emit), matching fold's default.
func (p *Projection) foldUpdate(evt ports.Event) bool {
	switch e := evt.(type) {
	case observed.UpdateCheckStarted:
		// Autoupdate probe begins (launch or manual re-check). The dial boots
		// gray + inert — "Checking for updates···". Fresh ViewModel clears any
		// prior update-failure hint. Design-log/037 §Q2/Q3.
		p.state = ViewModel{Stage: StagePreflight, Phase: PhasePreflight}
	case observed.UpdateCheckInfo:
		// Errors route through UpdateFailed (a distinct event); here we only
		// act on the success verdict. Up-to-date → wake straight to IDLE (the
		// common path). Outdated → remember the target; the Updating beat
		// begins on UpdateApplyStarted. Design-log/037 §Q3/Q4.
		// Context errors (deadline/cancel) mean the check was aborted — not a
		// failure; wake to IDLE so the user can proceed. UpdateFailed is NOT
		// published for these (see observed.Updater.Check).
		switch {
		case e.Err != nil && !errors.Is(e.Err, context.Canceled) && !errors.Is(e.Err, context.DeadlineExceeded):
			return false
		case e.Err != nil: // context timeout/cancel → treat as no update, proceed
			p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle}
		case e.Outdated:
			p.state.TargetVersion = e.To
		default:
			p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle}
		}
	case observed.UpdateApplyStarted:
		// The new binary is downloading. Gray dial, "Updating → vN". No byte
		// denominator is carried (design-log/037 §Q7), so the ring is the
		// frontend's indeterminate fill, not a percentage.
		p.state.Stage = StagePreflight
		p.state.Phase = PhaseUpdating
		p.state.TargetVersion = e.Version
	case observed.UpdateFailed:
		// Update errors (check or apply) are non-blocking: drop to idle so the
		// user can keep using the app. Hint surfaces under the dial; detail in the log.
		p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle, ErrorText: "Couldn't update — Advanced › Check for update"}
	default:
		return false
	}
	return true
}

// foldRelocate folds relocating's own event stream (internal/core/stages/
// relocating/events.go) into StageRelocating/PhaseRelocating (design-log/055
// addendum). Split out of fold the same way foldUpdate is, and reached via
// the same default-branch delegate chain — relocate publishes dedicated
// Relocate* types rather than the generic ritual.StartInfo/PlanInfo/
// UpdateInfo/FinishInfo/ErrorInfo pull/push use, so this can never collide
// with (or need to filter) the session-flow cases above it.
func (p *Projection) foldRelocate(evt ports.Event) bool {
	switch e := evt.(type) {
	case relocating.RelocateStarted:
		p.state = ViewModel{Stage: StageRelocating, Phase: PhaseRelocating}
		p.resetEtaBeat()
	case relocating.RelocatePlanned:
		p.state.BytesTotal = e.BytesTotal
		p.state.FilesTotal = e.FilesTotal
		p.state.FilesDone = 0
		// Empty relocate (nothing to copy — e.g. a fresh install with no
		// content yet): no RelocateProgress will ever fire, so plateau
		// complete-on-arrival the same way PlanInfo's empty-delta branch
		// does for pull/push.
		if e.BytesTotal == 0 && e.FilesTotal == 0 {
			p.state.Progress = 100
		} else {
			p.state.Progress = 0
		}
		p.refreshFormattedFields()
	case relocating.RelocateProgress:
		p.state.FilesDone = e.FilesDone
		p.state.FilesTotal = e.FilesTotal
		// BytesDone reads straight off the event's live CounterStorage tap
		// (copy.go) rather than an estimate — real bytes actually flushed to
		// disk, so arcFromBytes/SizeDoneText/EtaSeconds all keep moving
		// mid-file instead of jumping only at file boundaries (design-log/056
		// follow-up, 2026-08-15).
		p.state.BytesDone = e.BytesDone
		if e.FilesTotal > 0 {
			p.state.Progress = e.FilesDone * 100 / e.FilesTotal
		}
		p.state.EtaSeconds = p.etaFromSessionAvg(e.Elapsed, p.state.BytesDone)
		p.refreshFormattedFields()
	case relocating.RelocateVerifying, relocating.RelocateCommitting:
		// Fixed-cost tail work with no counter of its own — the frontend
		// detects the copying→tail handoff via FilesDone>=FilesTotal, the
		// same pattern PhaseSaving already uses for bytesDone>=bytesTotal.
		// Plateau explicitly rather than relying on the last
		// RelocateProgress having landed exactly at 100/FilesTotal.
		p.state.Progress = 100
		p.state.FilesDone = p.state.FilesTotal
		p.state.BytesDone = p.state.BytesTotal
		p.refreshFormattedFields()
	case relocating.RelocateFinished:
		p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle}
	case relocating.RelocateFailed:
		p.state = ViewModel{Stage: StageFailed, Phase: PhaseFailed, ErrorText: e.Err.Error()}
	default:
		return false
	}
	return true
}

// resetEtaBeat re-anchors the ETA beat and clears the current estimate. Called
// on every stage change and PlanInfo so the next Tick re-baselines the
// beat-wide average against the current plan; clearing EtaSeconds to 0 also
// resets the monotonic guard (etaFromSessionAvg only clamps against a positive
// previous value) so an estimate from a finished beat can't pin a new one.
func (p *Projection) resetEtaBeat() {
	p.etaBeatStarted = false
	p.state.EtaSeconds = 0
}

// etaWindowSeconds bounds how far back the beat-wide average in
// etaFromSessionAvg looks. Chosen to match progress.Ticker's own
// DefaultWindowN=5 at its 1-tick-per-second cadence — the same "5-ish
// seconds of history" the rest of the app already treats as the rolling
// window. Rather than a hard cliff (drop everything older than N seconds),
// the anchor slides forward once dt crosses this so recent throughput still
// gets folded in smoothly on the next sample instead of being discarded.
const etaWindowSeconds = 5.0

// etaFromSessionAvg derives a stable remaining-time estimate from a beat-wide
// average rate rather than the volatile rolling rate (design-log/028 §Q2). The
// first Tick of a beat only anchors (elapsed + bytes already counted) and
// returns 0 — the dial shows the decoder placeholder until a second Tick gives
// a positive elapsed delta, the ~3-5s grace window of design-log/009 §Q5.
// Thereafter rate = (done - anchorBytes) / (elapsed - anchorElapsed); ETA =
// remaining / rate.
//
// The anchor is NOT the beat's absolute start — see design-log/056 follow-up
// (2026-08-15): relocate's local-disk copy showed the estimate frozen at the
// same integer for 30+ real seconds while hundreds of MB visibly flowed. Root
// cause was purely mathematical, not a stale-event bug: an average taken
// since the operation's absolute start becomes less sensitive to new samples
// as elapsed grows (each additional second is a shrinking fraction of the
// total), and a local copy's throughput is naturally bursty — an initial
// burst into the OS write-back cache reads as a high rate, then real disk I/O
// throttles it — so the lifetime average decayed slowly enough that
// remaining/rate kept rounding to the same second for a long stretch. Once
// accumulated elapsed-since-anchor (dt) crosses etaWindowSeconds, the anchor
// slides forward to the current (elapsed, done) so the NEXT sample's rate
// reflects recent throughput, not the whole history — pull/push aren't
// visibly affected by this (network transfers are comparatively uniform,
// rarely span the width of one window this unevenly), so widening this to a
// window rather than a hard lifetime average is a strict improvement shared
// by both without changing their observed behavior.
//
// Monotone non-increasing within a window (§Q10): never climbs above the
// previous estimate — only PlanInfo/stage change (via resetEtaBeat) lets it
// grow, because then there is literally more work. Returns 0 (→ no estimate)
// for an empty plan, a completed beat, or a non-positive sample so the
// frontend never renders a fake or infinite number.
func (p *Projection) etaFromSessionAvg(elapsed time.Duration, done int64) int64 {
	if !p.etaBeatStarted {
		p.etaBeatStarted = true
		p.etaBeatElapsed = elapsed
		p.etaBeatBytes = done
		return 0
	}
	remaining := p.state.BytesTotal - done
	if p.state.BytesTotal <= 0 || remaining <= 0 {
		return 0
	}
	dt := (elapsed - p.etaBeatElapsed).Seconds()
	flowed := done - p.etaBeatBytes
	if dt <= 0 || flowed <= 0 {
		// No measurable progress yet this window — hold the current estimate
		// (still 0 right after the anchor tick) rather than divide by zero.
		return p.state.EtaSeconds
	}
	rateBytesPerSec := float64(flowed) / dt
	next := int64(float64(remaining) / rateBytesPerSec)
	if dt >= etaWindowSeconds {
		p.etaBeatElapsed = elapsed
		p.etaBeatBytes = done
	}
	if prev := p.state.EtaSeconds; prev > 0 && next > prev {
		return prev
	}
	return next
}

// refreshFormattedFields recomputes SizeDoneText/SizeTotalText/SizeUnit and
// SpeedText/SpeedUnit from the current BytesDone/BytesTotal/LogicalMbps
// (design-log/050) — the frontend's former unit-conversion math, moved here
// so it's computed exactly once. Called wherever those raw fields change.
func (p *Projection) refreshFormattedFields() {
	p.state.SizeDoneText, p.state.SizeTotalText, p.state.SizeUnit = formatSize(p.state.BytesDone, p.state.BytesTotal)
	p.state.SpeedText, p.state.SpeedUnit = formatSpeed(mbpsToBps(p.state.LogicalMbps))
}

// onTick applies a network-progress snapshot to the ViewModel. Pure picker:
// the ticker already derived everything, projection chooses which series
// drives the visible state for the current pipeline stage. Gated on
// pipelineStage so a late Tick arriving after the pipeline moved on (e.g.
// during Committing) does not freeze a stale BytesDone value.
//
// BytesDone reads from Stream.Data (logical, matches PlanInfo.BytesTotal)
// for pull. For push, BytesDone and ETA use Ops.Done × avg-blob-size instead:
// Logical.BytesOut counts bytes as they enter the compressor (fast local SSD),
// not when R2 confirms delivery, so s.Data bursts far ahead of actual R2
// progress and the monotone guard locks ETA at an optimistic early estimate
// for the remainder of the transfer. OpsComplete fires on PutStream return
// (R2 confirmation), which is the correct completion signal.
// SpeedMbps reads from Stream.Average — 5-second rolling wire rate, matches
// curl's --progress-bar convention (design-log/001 §Refinement).
// LogicalMbps reads from Stream.DataAverage — drives the chart's second
// series (decompress/install rate). EtaSeconds is derived separately from the
// beat-wide average (etaFromSessionAvg), not from any rolling rate above.
func (p *Projection) onTick(t progress.Tick) bool {
	// Unconditional raw-counter tracking (design-log/050 §A Q5): a Tick can
	// arrive before the stage that will consume it has even started (a
	// stray heartbeat during Checking), so onStateChanged needs the latest
	// raw value ready the instant the stage flips into Pulling/Pushing —
	// this must run before the pipelineStage gate below, which returns
	// early for every other stage.
	p.lastRemoteDown = t.Remote.Down.Data
	p.lastRemoteOps = t.Ops.Done

	var s progress.Stream
	var done int64
	switch p.pipelineStage {
	case ritual.StagePulling:
		s = t.Remote.Down
		// Baseline-subtract the process-lifetime cumulative counter down to
		// this flow's own delta (design-log/050 §A) — pullBaseline is the
		// counter's value at the moment this flow entered Pulling.
		done = s.Data - p.pullBaseline
		if done < 0 {
			done = 0
		}
	case ritual.StagePushing:
		s = t.Remote.Up
		// Ops-based progress: confirmed R2 deliveries × average blob size.
		// Falls back to s.Data when FilesTotal is zero (shouldn't happen after
		// a PlanInfo, but avoids a divide-by-zero on unexpected paths).
		// Ops.Done is baseline-subtracted the same way as the pull counter —
		// it's one shared cumulative counter across both directions and
		// every flow in the process (design-log/050 §A).
		if p.state.FilesTotal > 0 {
			avg := p.state.BytesTotal / int64(p.state.FilesTotal)
			ops := t.Ops.Done - p.pushOpsBaseline
			if ops < 0 {
				ops = 0
			}
			done = ops * avg
			if done > p.state.BytesTotal {
				done = p.state.BytesTotal
			}
		} else {
			done = s.Data
		}
	default:
		return false
	}
	p.state.BytesDone = done
	p.state.SpeedMbps = s.Average
	p.state.LogicalMbps = s.DataAverage
	p.state.EtaSeconds = p.etaFromSessionAvg(t.Elapsed, done)
	// Fold the upcoming prep beat's estimate onto the download beat's own
	// ETA (design-log/058 §Q2), so what's shown while bytes are still
	// flowing already reads "time until playable," not just "time until
	// bytes land." FlowSession only — a standalone FlowDownload never
	// proceeds to a prep beat, and prepEtaEstimate is 0 for every other
	// flow anyway (FlowStartedInfo only populates it for FlowSession).
	if p.pipelineStage == ritual.StagePulling && p.state.Phase == PhaseDownloading &&
		p.activeFlow == ritual.FlowSession && p.state.EtaSeconds > 0 {
		p.state.EtaSeconds += int64(p.prepEtaEstimate.Seconds())
	}
	// Combined download+prep ring fill (design-log/058 addendum): Progress
	// is normally frontend-derived from BytesDone/BytesTotal for a transfer
	// beat (design-log/050 §C), but the 0→80→100 handoff across two phases
	// needs the prep estimate, which only the backend has — so this one
	// case computes the percentage here instead, and the frontend just
	// reads vm.progress directly (dumb projection, no ratio math for this).
	// StagePulling only: StagePushing's Progress stays frontend-derived,
	// unchanged.
	if p.pipelineStage == ritual.StagePulling && p.state.BytesTotal > 0 {
		p.state.Progress = downloadProgress(float64(done)/float64(p.state.BytesTotal), p.prepEtaEstimate > 0)
	}
	// Stalled: a heartbeat tick (Instant==0) arriving while bytes are still
	// owed (BytesDone < BytesTotal) means the transfer is mid-flight but the
	// link has gone quiet — an R2 PutStream blocked on a TCP retransmit. The
	// frontend turns this into "Stalled — waiting on R2…". A zero-rate tick
	// once BytesDone>=BytesTotal is the trailing completion marker, not a
	// stall, so it must leave Stalled false. Design-log/022 #2.
	p.state.Stalled = s.Instant == 0 && p.state.BytesTotal > 0 && p.state.BytesDone < p.state.BytesTotal
	p.refreshFormattedFields()
	return true
}

// baselineOnStageEntry captures the process-lifetime cumulative counters'
// current value against their value at this flow's stage-entry
// (design-log/050 §A) — onTick subtracts these so BytesDone starts at 0 for
// this flow instead of carrying over bytes/ops a previous flow in the same
// running process already moved. Split out of onStateChanged to keep that
// function's cyclomatic complexity under the lint budget.
func (p *Projection) baselineOnStageEntry(to string) {
	switch to {
	case ritual.StagePulling:
		p.pullBaseline = p.lastRemoteDown
	case ritual.StagePushing:
		p.pushOpsBaseline = p.lastRemoteOps
	}
}

// onStateChanged maps ritual stage transitions to (Stage, Phase) per the
// design-log/017 §Bucket × Phase mapping table. Server-lifecycle events
// (ServerReady/Stopping) and pulling.ApplyStartedInfo refine Phase further
// within the Running and Pulling windows respectively.
func (p *Projection) onStateChanged(to string) {
	p.pipelineStage = to
	p.baselineOnStageEntry(to)
	// A stage transition is a fresh beat: clear any stall caption so it never
	// bleeds from the transfer window into Apply/Unlocking/Retaining. onTick
	// re-derives it from the next tick's rate if the new stage still transfers.
	p.state.Stalled = false
	// Same for ETA: each stage is its own beat (download then save are not one
	// continuous transfer), so re-anchor the beat-wide average. Design-log/028.
	p.resetEtaBeat()
	// Sync flows render as a single honest beat regardless of which shared
	// stage is running (design-log/031): Download is always `downloading` (⬇),
	// Upload is always `saving` (⬆). This skips the session-only Preparing /
	// Wrapping beats whose copy ("Spinning up", "Spinning down") assumes a
	// server. The terminal Done/Failed transitions fall through to the
	// lifecycle's StatusChanged reset, so don't paint them here.
	switch p.activeFlow {
	case ritual.FlowDownload:
		if to != ritual.StageDone && to != ritual.StageFailed {
			p.state.Stage = StageDownloading
			p.state.Phase = PhaseDownloading
		}
		return
	case ritual.FlowUpload:
		if to != ritual.StageDone && to != ritual.StageFailed {
			p.state.Stage = StageUploading
			p.state.Phase = PhaseSaving
		}
		return
	case ritual.FlowLocalSession:
		// Local-only session (design-log/036): chain is Checking → Running →
		// Done — no Committing/Retaining (skip-sync saves nothing). Checking is
		// honest prep, not "downloading" (nothing is pulled). Running falls
		// through to the session map below (→ preparing, then ServerReady flips
		// to playing, ServerStopping to wrapping); the run terminates straight to
		// Done with no saving beat — there is no ref write to narrate.
		if to == ritual.StageChecking {
			p.state.Stage = StageDownloading
			p.state.Phase = PhasePreparing
			return
		}
	case ritual.FlowSession, ritual.FlowRestore, ritual.FlowRevert, ritual.FlowRetentionApply:
		// No flow-specific stage overrides; fall through to the stage-based switch below.
	}
	switch to {
	case ritual.StageChecking, ritual.StagePulling:
		p.state.Stage = StageDownloading
		p.state.Phase = PhaseDownloading
	case ritual.StageAcquiring, ritual.StageProbing:
		// Acquiring (both flows) and Probing (Upload's head-only resolve,
		// design-log/031) are invisible prep work — brain-cog + "Preparing…",
		// no ETA. Probing does no byte transfer, so it never reaches the
		// downloading phase.
		p.state.Stage = StageDownloading
		p.state.Phase = PhasePreparing
		p.seedPrepCountdownIfAcquiring(to)
	case ritual.StageRunning:
		// Stay in downloading bucket / preparing phase until ServerReady
		// actually fires — see design-log/017 §Prep→Run boundary. The bucket
		// flip happens in onFold for ServerReadyInfo, not here.
		p.state.Stage = StageDownloading
		p.state.Phase = PhasePreparing
		p.state.BytesDone, p.state.BytesTotal = 0, 0
		p.state.FilesDone, p.state.FilesTotal = 0, 0
		// Progress is NOT zeroed here (unlike Bytes*/Files* above): Acquiring
		// already seeded it to prepSplitPercent moments earlier via
		// seedPrepCountdownIfAcquiring, and Running fires right after —
		// zeroing it here would flash the ring back to 0% for one frame
		// before the next tick recomputed it (design-log/058 addendum, the
		// exact "gitter" this recompute avoids). Recompute from the current
		// countdown state instead of leaving the seeded value untouched, so
		// this stays correct even if some time already elapsed between the
		// two events.
		p.state.Progress = prepProgress(int64(p.prepEtaEstimate.Seconds()), p.state.PrepEtaSeconds)
		p.refreshFormattedFields()
	case ritual.StageCommitting:
		p.state.Stage = StageUploading
		p.state.Phase = PhaseWrapping
	case ritual.StagePushing, ritual.StageUnlocking, ritual.StageRetaining:
		p.state.Stage = StageUploading
		p.state.Phase = PhaseSaving
	}
}

// onStatusChanged maps run-level lifecycle outcomes to the ViewModel.
// Failed populates ErrorText; Dismissed clears it and returns to Idle;
// Done resets to a clean Idle. Lock-held state survives a subsequent Failed
// because lifecycle resolves an acquisition-conflict as Failed even though
// the UI must stay on the friendly "someone is playing" screen.
//
// Failed is terminal but NOT gated behind Dismissed before a fresh Start can
// fire (lifecycle.start only rejects a second Start while already Running;
// Running itself is a no-op here) — so BytesDone/BytesTotal/Progress/etc.
// must be zeroed on Failed too, exactly like Idle/Done/Dismissed, or a retry
// right after a failure inherits stale near-100% numbers and the dial reads
// "Almost done" through the entire Checking/Probing/Acquiring/Committing
// prefix of the new attempt, before its own PlanInfo ever arrives.
func (p *Projection) onStatusChanged(e lifecycle.StatusChanged) {
	switch e.Status {
	case lifecycle.Idle, lifecycle.Done, lifecycle.Dismissed:
		p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle}
		p.everReady = false
		p.pipelineStage = ""
		p.activeFlow = ritual.FlowSession
		p.resetEtaEstimates()
	case lifecycle.Failed:
		lockHolder := p.state.LockHolder
		errText := ""
		if e.Err != nil {
			errText = e.Err.Error()
		}
		p.state = ViewModel{
			Stage:      StageFailed,
			Phase:      PhaseFailed,
			ErrorText:  errText,
			LockHolder: lockHolder,
		}
		p.everReady = false
		p.pipelineStage = ""
		p.activeFlow = ritual.FlowSession
		p.resetEtaEstimates()
	case lifecycle.Running:
	}
}

// seedPrepCountdownIfAcquiring starts the backend-driven prep countdown
// (design-log/058) when the stage transition landing on PhasePreparing is
// specifically Acquiring (not Probing) for FlowSession — the only flow that
// reaches this call site with a real prep beat ahead of it (FlowUpload's own
// Acquiring returns early in onStateChanged's activeFlow switch, above,
// before ever reaching this branch) and the only flow prepEtaEstimate is
// ever populated for (onFlowStarted). Split out of onStateChanged's shared
// Acquiring/Probing case to keep its cyclomatic complexity down.
func (p *Projection) seedPrepCountdownIfAcquiring(to string) {
	if to != ritual.StageAcquiring || p.activeFlow != ritual.FlowSession {
		return
	}
	p.prepEtaStartedAt = time.Now()
	p.state.PrepEtaSeconds = int64(p.prepEtaEstimate.Seconds())
	// Seed the ring fill at exactly prepSplitPercent (remaining==total here
	// -> elapsedFraction 0) so it continues smoothly from wherever the
	// download beat left off, with no reset/flash (design-log/058 addendum).
	p.state.Progress = prepProgress(int64(p.prepEtaEstimate.Seconds()), p.state.PrepEtaSeconds)
}

// onFlowStarted snapshots the prep estimate a full beat ahead of Acquiring
// (design-log/058 §Q2) so it's already available to add onto the download
// beat's EtaSeconds in onTick. FlowSession only — no other flow proceeds to
// a real prep beat afterward. Split out of fold to keep its cyclomatic
// complexity down.
func (p *Projection) onFlowStarted(flow ritual.Flow) {
	p.prepEtaEstimate = 0
	p.prepEtaStartedAt = time.Time{}
	if flow == ritual.FlowSession && p.estimator != nil {
		p.prepEtaEstimate = p.estimator.PrepEta()
	}
}

// onPrepBeatOver stops the prep countdown and fetches the wrap estimate a
// full beat ahead of ServerStoppingInfo, mirroring onFlowStarted's prep
// fetch (design-log/058). Split out of fold's ServerReadyInfo case to keep
// its cyclomatic complexity down.
func (p *Projection) onPrepBeatOver() {
	p.state.PrepEtaSeconds = 0
	p.prepEtaStartedAt = time.Time{}
	p.wrapEtaEstimate = 0
	if p.estimator != nil {
		p.wrapEtaEstimate = p.estimator.WrapEta()
	}
}

// prepSplitPercent is where the download beat's Progress fill hands off to
// the prep beat's fill, so the ring reads as one continuous 0→100 climb
// across both beats instead of two separate plateaus (design-log/058
// addendum). This whole computation is backend-only — the frontend reads
// vm.progress as a plain percentage and does no ratio math of its own
// (user directive: "frontend is just a dumb projection"). It only ever
// takes effect when p.prepEtaEstimate is a real, previously-measured
// duration for this machine (never a fabricated placeholder): with no
// history yet — first FlowSession run ever, or a standalone FlowDownload
// that never proceeds to a prep beat at all — downloadProgress and
// prepProgress both fall back to their pre-058 behavior (download climbs
// the full 0→100 itself; prep holds flat at 100, matching the old
// `arc: () => 1` plateau).
const prepSplitPercent = 80

// downloadProgress scales a raw bytesDone/bytesTotal fraction (already
// clamped to [0,1] by the caller) into the download beat's share of the
// combined ring.
func downloadProgress(fraction float64, hasPrepEstimate bool) int {
	if hasPrepEstimate {
		return int(fraction * prepSplitPercent)
	}
	return int(fraction * 100)
}

// prepProgress derives the prep beat's fill from the same countdown state
// PrepEtaSeconds already drives: totalSeconds is the beat's fixed starting
// estimate (0 = no real history), remainingSeconds ticks down to 0 as the
// beat proceeds (both already computed elsewhere — this is pure arithmetic
// on numbers the projection already owns, not a new data source).
func prepProgress(totalSeconds, remainingSeconds int64) int {
	if totalSeconds <= 0 {
		return 100
	}
	elapsedFraction := 1.0
	if remainingSeconds > 0 {
		elapsedFraction = 1 - float64(remainingSeconds)/float64(totalSeconds)
	}
	if elapsedFraction < 0 {
		elapsedFraction = 0
	}
	if elapsedFraction > 1 {
		elapsedFraction = 1
	}
	return prepSplitPercent + int(float64(100-prepSplitPercent)*elapsedFraction)
}

// resetEtaEstimates clears the prep/wrap countdown anchors alongside the
// terminal ViewModel resets above — the ViewModel fields already zero
// through the fresh ViewModel{} literal; these are the internal-only
// (non-ViewModel) anchors that must not survive into the next run.
func (p *Projection) resetEtaEstimates() {
	p.prepEtaEstimate = 0
	p.prepEtaStartedAt = time.Time{}
	p.wrapEtaEstimate = 0
	p.wrapEtaStartedAt = time.Time{}
}
