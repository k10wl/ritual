package sync

import "fmt"

// Phase identifiers used across Sync*FailedInfo events. Stable strings —
// downstream subscribers (logs, GUI) match on these.
const (
	PhaseScan              = "scan"
	PhasePlan              = "plan"
	PhaseStageDirInit      = "stage-dir-init"
	PhaseStage             = "stage"
	PhaseCommit            = "commit"
	PhaseOrphanCleanup     = "orphan-cleanup"
	PhaseGhostCleanup      = "ghost-cleanup"
	PhaseStagingDirCleanup = "staging-dir-cleanup"
)

// syncBase is the common envelope embedded by every sync event.
type syncBase struct {
	Source      string
	Destination string
	StagingID   string
}

func (b syncBase) tail() string {
	if b.StagingID != "" {
		return fmt.Sprintf("src=%s→dst=%s staging=%s", b.Source, b.Destination, b.StagingID)
	}
	return fmt.Sprintf("src=%s→dst=%s", b.Source, b.Destination)
}

// SyncStartedInfo is published when Scanning begins.
type SyncStartedInfo struct{ syncBase }

func (e SyncStartedInfo) String() string { return "sync.started " + e.tail() }

// SyncFinishedInfo is the success terminal event.
type SyncFinishedInfo struct {
	syncBase
	Files      int
	Bytes      int64
	DurationMs int64
}

func (e SyncFinishedInfo) String() string {
	return fmt.Sprintf("sync.finished files=%d bytes=%d dur=%dms %s", e.Files, e.Bytes, e.DurationMs, e.tail())
}

// SyncFailedInfo is the failure terminal event. Phase identifies which
// strategy returned the error.
type SyncFailedInfo struct {
	syncBase
	Phase string
	Err   string
}

func (e SyncFailedInfo) String() string {
	return fmt.Sprintf("sync.failed phase=%s err=%s %s", e.Phase, e.Err, e.tail())
}

// SyncPlanInfo is the summary plan event (counts + byte totals).
type SyncPlanInfo struct {
	syncBase
	Adds        int
	Updates     int
	Deletes     int
	AddBytes    int64
	UpdateBytes int64
	DeleteBytes int64
}

func (e SyncPlanInfo) String() string {
	return fmt.Sprintf("sync.plan adds=%d updates=%d deletes=%d transferBytes=%d deleteBytes=%d %s",
		e.Adds, e.Updates, e.Deletes, e.AddBytes+e.UpdateBytes, e.DeleteBytes, e.tail())
}

// Action identifies what Planning will do with the file.
const (
	ActionAdd    = "add"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// SyncPlanFileInfo is published once per file in the diff.
type SyncPlanFileInfo struct {
	syncBase
	Path   string
	Action string
	Size   int64
}

func (e SyncPlanFileInfo) String() string {
	return fmt.Sprintf("sync.plan.file %s action=%s size=%d %s", e.Path, e.Action, e.Size, e.tail())
}

// SyncStagingDirCreatedInfo is published by StageDirInit on success.
type SyncStagingDirCreatedInfo struct {
	syncBase
	StagingPath string
}

func (e SyncStagingDirCreatedInfo) String() string {
	return fmt.Sprintf("sync.staging-dir.created path=%s %s", e.StagingPath, e.tail())
}

// SyncStageStartedInfo brackets the per-file stage loop.
type SyncStageStartedInfo struct {
	syncBase
	Files int
	Bytes int64
}

func (e SyncStageStartedInfo) String() string {
	return fmt.Sprintf("sync.stage.started files=%d bytes=%d %s", e.Files, e.Bytes, e.tail())
}

// SyncStageProgressInfo is published after each file's stage round-trip.
type SyncStageProgressInfo struct {
	syncBase
	File       string
	FilesDone  int
	FilesTotal int
	BytesDone  int64
	BytesTotal int64
}

func (e SyncStageProgressInfo) String() string {
	return fmt.Sprintf("sync.stage.progress %d/%d files %d/%d bytes file=%s %s",
		e.FilesDone, e.FilesTotal, e.BytesDone, e.BytesTotal, e.File, e.tail())
}

// SyncStageFinishedInfo closes the stage phase on success.
type SyncStageFinishedInfo struct {
	syncBase
	Files      int
	Bytes      int64
	DurationMs int64
}

func (e SyncStageFinishedInfo) String() string {
	return fmt.Sprintf("sync.stage.finished files=%d bytes=%d dur=%dms %s", e.Files, e.Bytes, e.DurationMs, e.tail())
}

// SyncStageFailedInfo halts the run from the stage phase.
type SyncStageFailedInfo struct {
	syncBase
	File string
	Err  string
}

func (e SyncStageFailedInfo) String() string {
	return fmt.Sprintf("sync.stage.failed file=%s err=%s %s", e.File, e.Err, e.tail())
}

// SyncCommitStartedInfo brackets the per-file commit loop.
type SyncCommitStartedInfo struct {
	syncBase
	Files int
	Bytes int64
}

func (e SyncCommitStartedInfo) String() string {
	return fmt.Sprintf("sync.commit.started files=%d bytes=%d %s", e.Files, e.Bytes, e.tail())
}

// SyncCommitProgressInfo is published after each file's commit copy+delete.
type SyncCommitProgressInfo struct {
	syncBase
	File       string
	FilesDone  int
	FilesTotal int
	BytesDone  int64
	BytesTotal int64
}

func (e SyncCommitProgressInfo) String() string {
	return fmt.Sprintf("sync.commit.progress %d/%d files %d/%d bytes file=%s %s",
		e.FilesDone, e.FilesTotal, e.BytesDone, e.BytesTotal, e.File, e.tail())
}

// SyncCommitFinishedInfo closes the commit phase on success.
type SyncCommitFinishedInfo struct {
	syncBase
	Files      int
	Bytes      int64
	DurationMs int64
}

func (e SyncCommitFinishedInfo) String() string {
	return fmt.Sprintf("sync.commit.finished files=%d bytes=%d dur=%dms %s", e.Files, e.Bytes, e.DurationMs, e.tail())
}

// SyncCommitFailedInfo halts the run from the commit phase.
type SyncCommitFailedInfo struct {
	syncBase
	File string
	Err  string
}

func (e SyncCommitFailedInfo) String() string {
	return fmt.Sprintf("sync.commit.failed file=%s err=%s %s", e.File, e.Err, e.tail())
}

// SyncOrphanCleanupInfo is the single batch event for orphan delete on
// the destination (upload direction). Failed lists keys that the adapter
// reported as failures (R2 batch may be partial).
type SyncOrphanCleanupInfo struct {
	syncBase
	Keys       []string
	Failed     []string
	DurationMs int64
	Err        string
}

func (e SyncOrphanCleanupInfo) String() string {
	if e.Err != "" {
		return fmt.Sprintf("sync.orphan-cleanup count=%d err=%s dur=%dms %s",
			len(e.Keys), e.Err, e.DurationMs, e.tail())
	}
	return fmt.Sprintf("sync.orphan-cleanup count=%d failed=%d dur=%dms %s",
		len(e.Keys), len(e.Failed), e.DurationMs, e.tail())
}

// SyncGhostDeletedInfo is per-file delete on download direction.
type SyncGhostDeletedInfo struct {
	syncBase
	File string
}

func (e SyncGhostDeletedInfo) String() string {
	return fmt.Sprintf("sync.ghost.deleted file=%s %s", e.File, e.tail())
}

// SyncGhostCleanupFailedInfo halts the run when a ghost delete fails.
type SyncGhostCleanupFailedInfo struct {
	syncBase
	File string
	Err  string
}

func (e SyncGhostCleanupFailedInfo) String() string {
	return fmt.Sprintf("sync.ghost.failed file=%s err=%s %s", e.File, e.Err, e.tail())
}

// SyncStagingDirCleanedInfo is published by StagingDirCleanup on every run
// (success AND failure paths). Outcome is "success" when Err is nil.
type SyncStagingDirCleanedInfo struct {
	syncBase
	StagingPath string
	Outcome     string
	DurationMs  int64
	Err         string
}

func (e SyncStagingDirCleanedInfo) String() string {
	if e.Err != "" {
		return fmt.Sprintf("sync.staging-dir.cleaned outcome=%s path=%s err=%s dur=%dms %s",
			e.Outcome, e.StagingPath, e.Err, e.DurationMs, e.tail())
	}
	return fmt.Sprintf("sync.staging-dir.cleaned outcome=%s path=%s dur=%dms %s",
		e.Outcome, e.StagingPath, e.DurationMs, e.tail())
}

// envelope builds the syncBase for a given RunState.
func (rs *RunState) envelope() syncBase {
	return syncBase{
		Source:      stringStore(rs.Src, rs.SrcLabel),
		Destination: stringStore(rs.Dst, rs.DstLabel),
		StagingID:   rs.StagingID,
	}
}
