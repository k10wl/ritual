package sync

import (
	"context"
	"errors"
	"ritual/internal/core/machine"
	"time"
)

// Failed is the failure terminal. Emits SyncFailedInfo, then runs the
// staging cleanup directly (without going through the cleanup strategy
// chain), then returns rs.Err so the caller propagates the error.
//
// Cleanup error during failure path is logged but does NOT override the
// original rs.Err — the original failure is the cause; cleanup is a
// best-effort follow-up.
type Failed struct{}

// NewFailed constructs a Failed terminal.
func NewFailed() *Failed { return &Failed{} }

// Run emits failure events and cleans staging.
func (Failed) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	err := rs.Err
	if err == nil {
		err = errors.New("sync failed without recorded error")
	}

	rs.Publish(SyncFailedInfo{
		syncBase: rs.envelope(),
		Phase:    rs.FailedPhase,
		Err:      err.Error(),
	})

	// Best-effort staging cleanup. Skipped if StageDirInit never ran.
	if rs.StagingPath != "" && rs.Dst != nil {
		start := time.Now()
		cerr := rs.Dst.Delete(ctx, rs.StagingPath)
		evt := SyncStagingDirCleanedInfo{
			syncBase:    rs.envelope(),
			StagingPath: rs.StagingPath,
			DurationMs:  time.Since(start).Milliseconds(),
			Outcome:     "success",
		}
		if cerr != nil {
			evt.Outcome = "failed"
			evt.Err = cerr.Error()
		}
		rs.Publish(evt)
	}

	return nil, err
}
