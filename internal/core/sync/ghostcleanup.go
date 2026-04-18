package sync

import (
	"context"
	"fmt"
	"ritual/internal/core/machine"
)

// GhostCleanup deletes destination files that no longer exist on the
// source side, one file at a time. Used on the download direction (the
// destination is the local tree; per-file remove mirrors the os.Remove
// semantics natural to local FS).
type GhostCleanup struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewGhostCleanup constructs a GhostCleanup strategy.
func NewGhostCleanup(onOK, onFail machine.Strategy[RunState]) *GhostCleanup {
	return &GhostCleanup{onOK: onOK, onFail: onFail}
}

// Run iterates the delete set with per-file events.
func (g *GhostCleanup) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	env := rs.envelope()
	for _, path := range rs.Diff.Delete {
		if err := ctx.Err(); err != nil {
			return g.fail(rs, path, err)
		}
		if err := rs.Dst.Delete(ctx, path); err != nil {
			return g.fail(rs, path, fmt.Errorf("ghost delete %s: %w", path, err))
		}
		rs.Publish(SyncGhostDeletedInfo{syncBase: env, File: path})
	}
	return g.onOK, nil
}

func (g *GhostCleanup) fail(rs *RunState, file string, err error) (machine.Strategy[RunState], error) {
	rs.Err = err
	rs.FailedPhase = PhaseGhostCleanup
	rs.Publish(SyncGhostCleanupFailedInfo{
		syncBase: rs.envelope(),
		File:     file,
		Err:      err.Error(),
	})
	return g.onFail, nil
}
