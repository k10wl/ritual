package sync

import (
	"context"
	"fmt"
	"time"

	"ritual/internal/core/machine"
)

// Committing moves each staged file to its final destination key by
// dst.Copy(staging→final) followed by dst.Delete(staging key). Emits the
// commit lifecycle events.
type Committing struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewCommitting constructs a Committing strategy.
func NewCommitting(onOK, onFail machine.Strategy[RunState]) *Committing {
	return &Committing{onOK: onOK, onFail: onFail}
}

// Run performs per-file copy + delete. Bytes accumulator mirrors Staging.
func (s *Committing) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	files := rs.Diff.Upload
	env := rs.envelope()
	start := time.Now()

	rs.Publish(SyncCommitStartedInfo{
		syncBase: env,
		Files:    len(files),
		Bytes:    rs.TransferBytes,
	})

	var bytesDone int64
	for i, path := range files {
		if err := ctx.Err(); err != nil {
			return s.fail(rs, path, err)
		}
		stagedKey := rs.StagingPath + "/" + path
		if err := rs.Dst.Copy(ctx, stagedKey, path); err != nil {
			return s.fail(rs, path, fmt.Errorf("copy %s: %w", path, err))
		}
		// Delete the staging key after successful copy. Failure here is
		// non-fatal — staging cleanup will sweep anything left.
		_ = rs.Dst.Delete(ctx, stagedKey)
		bytesDone += rs.SrcMap[path].Size
		rs.Publish(SyncCommitProgressInfo{
			syncBase:   env,
			File:       path,
			FilesDone:  i + 1,
			FilesTotal: len(files),
			BytesDone:  bytesDone,
			BytesTotal: rs.TransferBytes,
		})
	}

	rs.Publish(SyncCommitFinishedInfo{
		syncBase:   env,
		Files:      len(files),
		Bytes:      rs.TransferBytes,
		DurationMs: time.Since(start).Milliseconds(),
	})
	return s.onOK, nil
}

func (s *Committing) fail(rs *RunState, file string, err error) (machine.Strategy[RunState], error) {
	rs.Err = err
	rs.FailedPhase = PhaseCommit
	rs.Publish(SyncCommitFailedInfo{
		syncBase: rs.envelope(),
		File:     file,
		Err:      err.Error(),
	})
	return s.onFail, nil
}
