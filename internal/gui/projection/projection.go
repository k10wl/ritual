package projection

import (
	"context"
	"time"

	"ritual/internal/adapters/observed"
	"ritual/internal/adapters/progress"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
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

// Projection subscribes to the bus and folds events into a single ViewModel.
// pipelineStage tracks the most recent ritual stage name so onTick can gate
// network-progress writes: a late progress.Tick arriving after the pipeline
// moved on to Committing must not overwrite stage-set values.
// everReady tracks whether the current run has reached ServerReady; gates
// the Running-stage phase transitions (preparing → playing → wrapping).
type Projection struct {
	ch            <-chan ports.Event
	unsub         func()
	emitter       Emitter
	addresses     AddressProvider
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
}

// New subscribes to bus immediately and returns a Projection ready for Run.
// Subscribing here (not in Run) avoids the race where callers publish before
// the Run goroutine manages to attach. AddressProvider may be nil; Addresses
// stays empty if so.
func New(bus ports.EventBus, emitter Emitter, addresses AddressProvider) *Projection {
	ch, unsub := bus.Subscribe()
	return &Projection{
		ch:         ch,
		unsub:      unsub,
		emitter:    emitter,
		addresses:  addresses,
		state:      ViewModel{Stage: StageIdle, Phase: PhaseIdle},
		activeFlow: ritual.FlowSession,
	}
}

// Snapshot returns the current view model. Used by ControlService.GetSnapshot
// so the frontend has a value to render before the first Emit arrives.
func (p *Projection) Snapshot() ViewModel { return p.state }

// Run blocks until ctx is cancelled or the bus closes. Publishes an initial
// snapshot immediately so consumers never see an empty view.
func (p *Projection) Run(ctx context.Context) {
	defer p.unsub()
	p.emitter.Emit(p.state)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-p.ch:
			if !ok {
				return
			}
			if p.fold(evt) {
				p.emitter.Emit(p.state)
			}
		}
	}
}

// fold mutates p.state based on evt and reports whether the event was
// relevant. Irrelevant events return false to avoid emitting duplicates.
func (p *Projection) fold(evt ports.Event) bool {
	switch e := evt.(type) {
	case ritual.FlowStartedInfo:
		// Internal-only: records which flow is in flight for onStateChanged.
		// Paints nothing itself, so report no change (no redundant emit).
		p.activeFlow = e.Flow
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
		if p.addresses != nil {
			p.state.Addresses = p.addresses.Addresses()
		}
	case running.ServerStoppingInfo:
		// User-driven shutdown begun. Flip bucket to uploading + phase to
		// wrapping; addresses are no longer useful so drop them.
		p.state.Stage = StageUploading
		p.state.Phase = PhaseWrapping
		p.state.Addresses = nil
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
		if e.BytesTotal == 0 && e.FilesTotal == 0 {
			p.state.Progress = 100
		} else {
			p.state.Progress = 0
		}
	case lifecycle.StatusChanged:
		p.onStatusChanged(e)
	default:
		// Autoupdate (design-log/037) and anything else the projection ignores.
		return p.foldUpdate(evt)
	}
	return true
}

// foldUpdate folds the observed.Update* stream into the gray Preflight dial
// (design-log/037). Split out of fold so each stays under the complexity
// budget. Returns whether the event changed the ViewModel; unknown events
// return false (no redundant emit), matching fold's default.
func (p *Projection) foldUpdate(evt ports.Event) bool {
	switch e := evt.(type) {
	case observed.UpdateCheckStarted:
		// Autoupdate probe begins (launch or manual re-check). The dial boots
		// gray + inert — "Checking for updates···". Design-log/037 §Q2/Q3.
		p.state = ViewModel{Stage: StagePreflight, Phase: PhasePreflight}
	case observed.UpdateCheckInfo:
		// Errors route through UpdateFailed (a distinct event); here we only
		// act on the success verdict. Up-to-date → wake straight to IDLE (the
		// common path). Outdated → remember the target; the Updating beat
		// begins on UpdateApplyStarted. Design-log/037 §Q3/Q4.
		switch {
		case e.Err != nil:
			return false
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
		// Best-effort mandatory: a failed check/apply drops into 017's single
		// failure pathway — glyph x, "Tap to dismiss" → usable IDLE. The
		// frontend reads the update flavour from the error text. Design-log/037 §Q5.
		p.state.Stage = StageFailed
		p.state.Phase = PhaseFailed
		if e.Err != nil {
			p.state.ErrorText = e.Err.Error()
		}
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

// etaFromSessionAvg derives a stable remaining-time estimate from the beat-wide
// average rate rather than the volatile rolling rate (design-log/028 §Q2). The
// first Tick of a beat only anchors (elapsed + bytes already counted) and
// returns 0 — the dial shows the decoder placeholder until a second Tick gives
// a positive elapsed delta, the ~3-5s grace window of design-log/009 §Q5.
// Thereafter rate = (done - anchorBytes) / (elapsed - anchorElapsed); ETA =
// remaining / rate. Monotone non-increasing within a beat (§Q10): never climbs
// above the previous estimate — only PlanInfo/stage change (via resetEtaBeat)
// lets it grow, because then there is literally more work. Returns 0 (→ no
// estimate) for an empty plan, a completed beat, or a non-positive sample so
// the frontend never renders a fake or infinite number.
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
		// No measurable progress yet this beat — hold the current estimate
		// (still 0 right after the anchor tick) rather than divide by zero.
		return p.state.EtaSeconds
	}
	rateBytesPerSec := float64(flowed) / dt
	next := int64(float64(remaining) / rateBytesPerSec)
	if prev := p.state.EtaSeconds; prev > 0 && next > prev {
		return prev
	}
	return next
}

// onTick applies a network-progress snapshot to the ViewModel. Pure picker:
// the ticker already derived everything, projection chooses which series
// drives the visible state for the current pipeline stage. Gated on
// pipelineStage so a late Tick arriving after the pipeline moved on (e.g.
// during Committing) does not freeze a stale BytesDone value.
//
// BytesDone reads from Stream.Data (logical, matches PlanInfo.BytesTotal).
// SpeedMbps reads from Stream.Average — 5-second rolling wire rate, matches
// curl's --progress-bar convention (design-log/001 §Refinement).
// LogicalMbps reads from Stream.DataAverage — drives the chart's second
// series (decompress/install rate). EtaSeconds is derived separately from the
// beat-wide average (etaFromSessionAvg), not from any rolling rate above.
func (p *Projection) onTick(t progress.Tick) bool {
	var s progress.Stream
	switch p.pipelineStage {
	case ritual.StagePulling:
		s = t.Remote.Down
	case ritual.StagePushing:
		s = t.Remote.Up
	default:
		return false
	}
	p.state.BytesDone = s.Data
	p.state.SpeedMbps = s.Average
	p.state.LogicalMbps = s.DataAverage
	p.state.EtaSeconds = p.etaFromSessionAvg(t.Elapsed, s.Data)
	// Stalled: a heartbeat tick (Instant==0) arriving while bytes are still
	// owed (BytesDone < BytesTotal) means the transfer is mid-flight but the
	// link has gone quiet — an R2 PutStream blocked on a TCP retransmit. The
	// frontend turns this into "Stalled — waiting on R2…". A zero-rate tick
	// once BytesDone>=BytesTotal is the trailing completion marker, not a
	// stall, so it must leave Stalled false. Design-log/022 #2.
	p.state.Stalled = s.Instant == 0 && p.state.BytesTotal > 0 && p.state.BytesDone < p.state.BytesTotal
	return true
}

// onStateChanged maps ritual stage transitions to (Stage, Phase) per the
// design-log/017 §Bucket × Phase mapping table. Server-lifecycle events
// (ServerReady/Stopping) and pulling.ApplyStartedInfo refine Phase further
// within the Running and Pulling windows respectively.
func (p *Projection) onStateChanged(to string) {
	p.pipelineStage = to
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
	case ritual.StageRunning:
		// Stay in downloading bucket / preparing phase until ServerReady
		// actually fires — see design-log/017 §Prep→Run boundary. The bucket
		// flip happens in onFold for ServerReadyInfo, not here.
		p.state.Stage = StageDownloading
		p.state.Phase = PhasePreparing
		p.state.Progress, p.state.BytesDone, p.state.BytesTotal = 0, 0, 0
		p.state.FilesDone, p.state.FilesTotal = 0, 0
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
func (p *Projection) onStatusChanged(e lifecycle.StatusChanged) {
	switch e.Status {
	case lifecycle.Idle, lifecycle.Done, lifecycle.Dismissed:
		p.state = ViewModel{Stage: StageIdle, Phase: PhaseIdle}
		p.everReady = false
		p.pipelineStage = ""
		p.activeFlow = ritual.FlowSession
	case lifecycle.Failed:
		p.state.Stage = StageFailed
		p.state.Phase = PhaseFailed
		if e.Err != nil {
			p.state.ErrorText = e.Err.Error()
		}
	case lifecycle.Running:
	}
}
