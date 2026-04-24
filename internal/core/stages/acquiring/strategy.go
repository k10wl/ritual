// Package acquiring takes the remote lease. On an empty or expired
// remote slot it claims the lease and records the fresh sessionId on
// RunState. On a live foreign lease it routes to onFail and surfaces the
// holder via LockHeldInfo. The lease lives in a single self-describing
// object at {prefix}/lock — acquire is one PUT, so there is no
// partial-state rollback path.
package acquiring

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"time"
)

// AcquireFn claims the remote lease and returns the fresh sessionId.
// Injected so acquiring stays decoupled from the Locker concrete type.
type AcquireFn func(ctx context.Context) (sessionID string, err error)

// InspectFn returns a snapshot of the current holder. Nil means slot
// free. Used only for the LockHeldInfo diagnostic on ErrLocked.
type InspectFn func(ctx context.Context) (*lock.Holder, error)

// SnapshotLocalFn captures the local manifest at acquire time so Backup
// can diff post-run XXHashMap against the pre-session baseline. Acquiring
// runs after Pull/Apply, so the snapshot is taken immediately before the
// server session begins.
type SnapshotLocalFn func(ctx context.Context) (*domain.Manifest, error)

// Strategy implements the Acquiring stage.
type Strategy struct {
	acquire       AcquireFn
	inspect       InspectFn
	snapshotLocal SnapshotLocalFn
	interval      time.Duration
	onOK          machine.Strategy[ritual.RunState]
	onFail        machine.Strategy[ritual.RunState]
}

// New builds an Acquiring Strategy.
func New(
	acquire AcquireFn,
	inspect InspectFn,
	snapshotLocal SnapshotLocalFn,
	interval time.Duration,
	onOK, onFail machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{
		acquire:       acquire,
		inspect:       inspect,
		snapshotLocal: snapshotLocal,
		interval:      interval,
		onOK:          onOK,
		onFail:        onFail,
	}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageAcquiring }

// Run claims the remote lease.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "acquire"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		publish(rs.Bus, ritual.ErrorInfo{Operation: "acquire", Err: err})
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}

	sessionID, err := s.acquire(ctx)
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			if h, inspectErr := s.inspect(ctx); inspectErr == nil && h != nil {
				publish(rs.Bus, LockHeldInfo{
					Holder:      h.Owner,
					SessionID:   h.SessionID,
					AcquiredAt:  h.AcquiredAt,
					HeartbeatAt: h.HeartbeatAt,
					ExpiresAt:   h.ExpiresAt,
					Stale:       h.Stale,
				})
			}
		}
		rs.Err = err
		publish(rs.Bus, ritual.ErrorInfo{Operation: "acquire", Err: err})
		return s.onFail, nil
	}

	rs.SessionID = sessionID
	if s.snapshotLocal != nil {
		snap, snapErr := s.snapshotLocal(ctx)
		if snapErr != nil {
			publish(rs.Bus, ritual.ErrorInfo{Operation: "acquire.snapshot", Err: snapErr})
		} else {
			rs.LocalBefore = snap
		}
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "acquire"})
	publish(rs.Bus, ritual.LockAcquiredInfo{
		RunID:     rs.RunID,
		SessionID: sessionID,
		Interval:  s.interval,
	})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
