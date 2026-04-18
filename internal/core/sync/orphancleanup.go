package sync

import (
	"context"
	"fmt"
	"ritual/internal/core/machine"
	"time"
)

// OrphanCleanup batch-deletes destination keys that no longer exist in the
// source. Used on the upload direction. Emits a single SyncOrphanCleanupInfo
// with per-key results from the adapter (R2 may report partial failures).
type OrphanCleanup struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewOrphanCleanup constructs an OrphanCleanup strategy.
func NewOrphanCleanup(onOK, onFail machine.Strategy[RunState]) *OrphanCleanup {
	return &OrphanCleanup{onOK: onOK, onFail: onFail}
}

// Run sends one DeleteBatch when there are orphans; no-op when empty.
func (o *OrphanCleanup) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	if len(rs.Diff.Delete) == 0 {
		return o.onOK, nil
	}
	start := time.Now()
	err := rs.Dst.DeleteBatch(ctx, rs.Diff.Delete)
	evt := SyncOrphanCleanupInfo{
		syncBase:   rs.envelope(),
		Keys:       rs.Diff.Delete,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		evt.Err = err.Error()
		rs.Publish(evt)
		rs.Err = fmt.Errorf("orphan cleanup: %w", err)
		rs.FailedPhase = PhaseOrphanCleanup
		return o.onFail, nil
	}
	rs.Publish(evt)
	return o.onOK, nil
}
