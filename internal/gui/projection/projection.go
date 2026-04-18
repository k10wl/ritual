package projection

import (
	"context"
	"ritual/internal/app"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/sync"
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
type Projection struct {
	ch        <-chan ports.Event
	unsub     func()
	emitter   Emitter
	addresses AddressProvider
	state     ViewModel
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
	case sync.SyncStageStartedInfo:
		p.state.Label = "Downloading…"
		p.state.FilesTotal, p.state.BytesTotal = e.Files, e.Bytes
		p.state.FilesDone, p.state.BytesDone, p.state.Progress = 0, 0, 0
	case sync.SyncStageProgressInfo:
		p.state.FilesDone, p.state.FilesTotal = e.FilesDone, e.FilesTotal
		p.state.BytesDone, p.state.BytesTotal = e.BytesDone, e.BytesTotal
		p.state.Progress = percent(e.BytesDone, e.BytesTotal)
	case sync.SyncCommitStartedInfo:
		p.state.FilesTotal, p.state.BytesTotal = e.Files, e.Bytes
		p.state.FilesDone, p.state.BytesDone, p.state.Progress = 0, 0, 0
	case sync.SyncCommitProgressInfo:
		p.state.FilesDone, p.state.FilesTotal = e.FilesDone, e.FilesTotal
		p.state.BytesDone, p.state.BytesTotal = e.BytesDone, e.BytesTotal
		p.state.Progress = percent(e.BytesDone, e.BytesTotal)
	case acquiring.LockHeldInfo:
		p.state.Stage = StageLocked
		p.state.LockHolder = e.Holder
		p.state.Label = e.Holder + " is playing"
		p.state.ErrorText = ""
	case app.StatusChanged:
		p.onStatusChanged(e)
	default:
		return false
	}
	return true
}

func (p *Projection) onStateChanged(to string) {
	switch to {
	case ritual.StageChecking, ritual.StageFetching, ritual.StageAcquiring:
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
	case ritual.StagePublishing, ritual.StageBackup, ritual.StageUnlocking, ritual.StageRetaining:
		p.state.Stage = StageUploading
		p.state.Label = uploadLabel(to)
		p.state.ReadyLight = false
	}
}

func (p *Projection) onStatusChanged(e app.StatusChanged) {
	switch e.Status {
	case app.Idle, app.Done:
		p.state = ViewModel{Stage: StageIdle}
	case app.Failed:
		if p.state.Stage == StageLocked {
			return
		}
		p.state.Stage = StageFailed
		if e.Err != nil {
			p.state.ErrorText = e.Err.Error()
		}
	case app.Running:
	}
}

func downloadLabel(stage string) string {
	switch stage {
	case ritual.StageChecking:
		return "Checking…"
	case ritual.StageFetching:
		return "Downloading…"
	case ritual.StageAcquiring:
		return "Acquiring lock…"
	}
	return "Preparing…"
}

func uploadLabel(stage string) string {
	switch stage {
	case ritual.StagePublishing:
		return "Uploading…"
	case ritual.StageBackup:
		return "Backing up…"
	case ritual.StageUnlocking:
		return "Releasing lock…"
	case ritual.StageRetaining:
		return "Pruning old backups…"
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
