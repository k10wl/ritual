package sync

import (
	"context"
	"fmt"
	"ritual/internal/core/machine"
	"time"
)

// Staging copies each file in the diff from src into the destination's
// staging path. Emits Started, per-file Progress, Finished, or Failed.
type Staging struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewStaging constructs a Staging strategy.
func NewStaging(onOK, onFail machine.Strategy[RunState]) *Staging {
	return &Staging{onOK: onOK, onFail: onFail}
}

// Run iterates the upload set: src.Get → dst.Put(staging key).
func (s *Staging) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	files := rs.Diff.Upload
	env := rs.envelope()
	start := time.Now()

	rs.Publish(SyncStageStartedInfo{
		syncBase: env,
		Files:    len(files),
		Bytes:    rs.TransferBytes,
	})

	var bytesDone int64
	for i, path := range files {
		if err := ctx.Err(); err != nil {
			return s.fail(rs, path, err)
		}
		data, err := rs.Src.Get(ctx, path)
		if err != nil {
			return s.fail(rs, path, fmt.Errorf("get %s: %w", path, err))
		}
		stagedKey := rs.StagingPath + "/" + path
		if err := rs.Dst.Put(ctx, stagedKey, data); err != nil {
			return s.fail(rs, path, fmt.Errorf("put %s: %w", path, err))
		}
		bytesDone += rs.SrcMap[path].Size
		rs.Publish(SyncStageProgressInfo{
			syncBase:   env,
			File:       path,
			FilesDone:  i + 1,
			FilesTotal: len(files),
			BytesDone:  bytesDone,
			BytesTotal: rs.TransferBytes,
		})
	}

	rs.Publish(SyncStageFinishedInfo{
		syncBase:   env,
		Files:      len(files),
		Bytes:      rs.TransferBytes,
		DurationMs: time.Since(start).Milliseconds(),
	})
	return s.onOK, nil
}

func (s *Staging) fail(rs *RunState, file string, err error) (machine.Strategy[RunState], error) {
	rs.Err = err
	rs.FailedPhase = PhaseStage
	rs.Publish(SyncStageFailedInfo{
		syncBase: rs.envelope(),
		File:     file,
		Err:      err.Error(),
	})
	return s.onFail, nil
}
