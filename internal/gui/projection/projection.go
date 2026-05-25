package projection

import (
	"context"
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
}

// New subscribes to bus immediately and returns a Projection ready for Run.
// Subscribing here (not in Run) avoids the race where callers publish before
// the Run goroutine manages to attach. AddressProvider may be nil; Addresses
// stays empty if so.
func New(bus ports.EventBus, emitter Emitter, addresses AddressProvider) *Projection {
	ch, unsub := bus.Subscribe()
	return &Projection{
		ch:        ch,
		unsub:     unsub,
		emitter:   emitter,
		addresses: addresses,
		state:     ViewModel{Stage: StageIdle, Phase: PhaseIdle},
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
	case ritual.StateChangedInfo:
		p.onStateChanged(e.To)
	case pulling.ApplyStartedInfo:
		// Pulling's network phase is done; apply is running invisibly. Phase
		// flips from `downloading` to `preparing` so the dial swaps glyph
		// (download → brain-cog) and drops ETA. See design-log/017 §Q1.
		if p.pipelineStage == ritual.StagePulling {
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
	case lifecycle.StatusChanged:
		p.onStatusChanged(e)
	default:
		return false
	}
	return true
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
// series (decompress/install rate).
func (p *Projection) onTick(t progress.Tick) bool {
	switch p.pipelineStage {
	case ritual.StagePulling:
		p.state.BytesDone = t.Remote.Down.Data
		p.state.SpeedMbps = t.Remote.Down.Average
		p.state.LogicalMbps = t.Remote.Down.DataAverage
	case ritual.StagePushing:
		p.state.BytesDone = t.Remote.Up.Data
		p.state.SpeedMbps = t.Remote.Up.Average
		p.state.LogicalMbps = t.Remote.Up.DataAverage
	default:
		return false
	}
	return true
}

// onStateChanged maps ritual stage transitions to (Stage, Phase) per the
// design-log/017 §Bucket × Phase mapping table. Server-lifecycle events
// (ServerReady/Stopping) and pulling.ApplyStartedInfo refine Phase further
// within the Running and Pulling windows respectively.
func (p *Projection) onStateChanged(to string) {
	p.pipelineStage = to
	switch to {
	case ritual.StageChecking, ritual.StagePulling:
		p.state.Stage = StageDownloading
		p.state.Phase = PhaseDownloading
	case ritual.StageAcquiring:
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
	case lifecycle.Failed:
		p.state.Stage = StageFailed
		p.state.Phase = PhaseFailed
		if e.Err != nil {
			p.state.ErrorText = e.Err.Error()
		}
	case lifecycle.Running:
	}
}
