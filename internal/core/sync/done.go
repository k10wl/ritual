package sync

import (
	"context"
	"time"

	"ritual/internal/core/machine"
)

// Done is the success terminal. Emits SyncFinishedInfo with totals.
type Done struct{}

// NewDone constructs a Done terminal.
func NewDone() *Done { return &Done{} }

// Run emits the success event and terminates.
func (Done) Run(_ context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	rs.Publish(SyncFinishedInfo{
		syncBase:   rs.envelope(),
		Files:      len(rs.Diff.Upload),
		Bytes:      rs.TransferBytes,
		DurationMs: time.Since(rs.Started).Milliseconds(),
	})
	return nil, nil
}
