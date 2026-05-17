package projection

import (
	"context"
	"fmt"
	"ritual/internal/adapters/progress"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/running"
)

// Emitter receives a snapshot after every fold that changes the ViewModel.
// cmd/gui implements this with a Wails typed event; tests use a slice.
type Emitter interface {
	Emit(vm ViewModel)
}

// AddressProvider returns the list of join addresses to render on the
// Running stage. Called once per StageRunning entry.
type AddressProvider interface {
	Addresses() []JoinAddress
}

// Projection subscribes to the bus and folds events into a single ViewModel.
// pipelineStage tracks the most recent ritual stage name so onTick can gate
// label mutation: a late progress.Tick arriving after the pipeline moved on
// to Committing must not flip the label back to "Uploading — N Mbps".
type Projection struct {
	ch            <-chan ports.Event
	unsub         func()
	emitter       Emitter
	addresses     AddressProvider
	state         ViewModel
	pipelineStage string
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
		state:     ViewModel{Stage: StageIdle},
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
	case running.ServerStartingInfo:
		p.state.Label = "Starting server…"
		p.state.ReadyLight = false
	case running.ServerReadyInfo:
		p.state.Label = "Ready"
		p.state.ReadyLight = true
	case running.ServerStoppingInfo:
		p.state.Label = "Stopping…"
		p.state.ReadyLight = false
	case running.ServerStoppedInfo:
		p.state.ReadyLight = false
	case acquiring.LockHeldInfo:
		p.state.Stage = StageLocked
		p.state.LockHolder = e.Holder
		p.state.Label = e.Holder + " is playing"
		p.state.ErrorText = ""
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
// during Committing) does not overwrite the stage-set label or freeze a
// stale BytesDone value.
//
// BytesDone reads from Stream.Data (logical, matches PlanInfo.BytesTotal).
// Label and SpeedMbps read from Stream.Average — 5-second rolling wire
// rate, matches curl's --progress-bar convention (design-log/001
// §Refinement). LogicalMbps reads from Stream.DataAverage — drives the
// chart's second series (decompress/install rate).
//
// The 0.5 Mbps floor on the LABEL suppresses "0.0 Mbps" flicker when
// nothing is moving; the stage-set label ("Downloading…") stays visible
// until real activity crosses the floor. SpeedMbps / LogicalMbps are
// always set so frontend charts have the raw numbers regardless of label
// state.
const speedLabelFloorMbps = 0.5

func (p *Projection) onTick(t progress.Tick) bool {
	switch p.pipelineStage {
	case ritual.StagePulling:
		p.state.BytesDone = t.Remote.Down.Data
		p.state.SpeedMbps = t.Remote.Down.Average
		p.state.LogicalMbps = t.Remote.Down.DataAverage
		if t.Remote.Down.Average > speedLabelFloorMbps {
			p.state.Label = fmt.Sprintf("Downloading — %.1f Mbps", t.Remote.Down.Average)
		}
	case ritual.StagePushing:
		p.state.BytesDone = t.Remote.Up.Data
		p.state.SpeedMbps = t.Remote.Up.Average
		p.state.LogicalMbps = t.Remote.Up.DataAverage
		if t.Remote.Up.Average > speedLabelFloorMbps {
			p.state.Label = fmt.Sprintf("Uploading — %.1f Mbps", t.Remote.Up.Average)
		}
	default:
		return false
	}
	return true
}

func (p *Projection) onStateChanged(to string) {
	p.pipelineStage = to
	switch to {
	case ritual.StageChecking, ritual.StagePulling, ritual.StageAcquiring:
		p.state.Stage = StageDownloading
		p.state.Label = downloadLabel(to)
	case ritual.StageRunning:
		p.state.Stage = StageRunning
		p.state.Label = "Starting server…"
		p.state.Progress, p.state.BytesDone, p.state.BytesTotal = 0, 0, 0
		p.state.FilesDone, p.state.FilesTotal = 0, 0
		if p.addresses != nil {
			p.state.Addresses = p.addresses.Addresses()
		}
	case ritual.StageCommitting, ritual.StagePushing, ritual.StageUnlocking, ritual.StageRetaining:
		p.state.Stage = StageUploading
		p.state.Label = uploadLabel(to)
		p.state.ReadyLight = false
	}
}

func (p *Projection) onStatusChanged(e lifecycle.StatusChanged) {
	switch e.Status {
	case lifecycle.Idle, lifecycle.Done:
		p.state = ViewModel{Stage: StageIdle}
	case lifecycle.Failed:
		if p.state.Stage == StageLocked {
			return
		}
		p.state.Stage = StageFailed
		if e.Err != nil {
			p.state.ErrorText = e.Err.Error()
		}
	case lifecycle.Running:
	}
}

func downloadLabel(stage string) string {
	switch stage {
	case ritual.StageChecking:
		return "Checking…"
	case ritual.StagePulling:
		return "Downloading…"
	case ritual.StageAcquiring:
		return "Acquiring lock…"
	}
	return "Preparing…"
}

func uploadLabel(stage string) string {
	switch stage {
	case ritual.StageCommitting:
		return "Snapshotting…"
	case ritual.StagePushing:
		return "Uploading…"
	case ritual.StageUnlocking:
		return "Releasing lock…"
	case ritual.StageRetaining:
		return "Pruning old refs…"
	}
	return "Finishing…"
}

func percent(done, total int64) int {
	if total <= 0 {
		return 0
	}
	p := int((done * 100) / total)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
