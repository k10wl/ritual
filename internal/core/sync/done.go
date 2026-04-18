package sync

import (
	"context"
	"ritual/internal/core/machine"
	"time"
)

// Done is the success terminal. Emits SyncFinishedInfo with totals.
type Done struct{}

// NewDone constructs a Done terminal.
func NewDone() *Done { return &Done{} }

// Run emits the success event and terminates.
//
//nolint:nilnil // (nil, nil) is the canonical machine.Strategy terminal return.
func (Done) Run(_ context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	rs.Publish(SyncFinishedInfo{
		syncBase:   rs.envelope(),
		Files:      len(rs.Diff.Upload),
		Bytes:      rs.TransferBytes,
		DurationMs: time.Since(rs.Started).Milliseconds(),
	})
	return nil, nil
}
