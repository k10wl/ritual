package sync

import (
	"context"
	"ritual/internal/core/machine"
	"time"
)

// StagingDirCleanup removes the per-run staging area from the destination.
// Runs on BOTH success and failure paths — events are the forensic record;
// preserving filesystem leftovers would only cause ghost-hoarding.
//
// On success: cleanup error promotes the run to failure (cleanup must
// succeed for happy ending). On failure: cleanup error is logged via the
// event but never overrides rs.Err.
type StagingDirCleanup struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewStagingDirCleanup constructs a StagingDirCleanup strategy.
func NewStagingDirCleanup(onOK, onFail machine.Strategy[RunState]) *StagingDirCleanup {
	return &StagingDirCleanup{onOK: onOK, onFail: onFail}
}

// Run deletes the staging path. Adapter Delete handles the tree.
func (c *StagingDirCleanup) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	if rs.StagingPath == "" {
		// Nothing to clean (failure occurred before StageDirInit).
		return c.onOK, nil
	}

	start := time.Now()
	err := rs.Dst.Delete(ctx, rs.StagingPath)
	evt := SyncStagingDirCleanedInfo{
		syncBase:    rs.envelope(),
		StagingPath: rs.StagingPath,
		DurationMs:  time.Since(start).Milliseconds(),
		Outcome:     "success",
	}
	if err != nil {
		evt.Outcome = "failed"
		evt.Err = err.Error()
	}
	rs.Publish(evt)

	// On success path (rs.Err == nil), promote cleanup failure to a real
	// failure. On failure path, leave rs.Err alone.
	if err != nil && rs.Err == nil {
		rs.Err = err
		rs.FailedPhase = PhaseStagingDirCleanup
		return c.onFail, nil
	}
	return c.onOK, nil
}
