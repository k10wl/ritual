// Package ritual carries the run-scoped value types shared across the
// state-machine stages. RunState is the sole cross-stage payload; stages
// read and write it instead of threading data through constructor args
// or back-references.
package ritual

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// RunState is the per-run value shared by every stage. It carries:
//   - services whose scope is the run (Bus, RunID)
//   - data produced by one stage and consumed by later stages
//     (SessionID, LocalBefore, RemoteBefore)
//   - the last error, for Failed to report
//
// Lease state lives outside this struct; the heartbeat supervisor owns it
// via bus events. SessionID is the UUIDv4 fencing token minted by
// lock.Locker.Acquire and carried through the run.
type RunState struct {
	RunID        string
	Bus          ports.EventBus
	SessionID    string
	LocalBefore  *domain.Manifest
	RemoteBefore *domain.Manifest
	Err          error
	FailedStage  string
}
