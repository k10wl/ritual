// Package unlocking releases the remote lease at session end. The
// release callback (typically lock.Locker.Release) silently no-ops on
// foreign/absent leases, so Unlocking is always safe to run. Runs under
// context.WithoutCancel so shutdown cannot abort release.
package unlocking

import (
	"context"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// ReleaseFn deletes the remote lease iff the supplied sessionId still
// owns it. Injected so unlocking stays decoupled from the Locker
// concrete type.
type ReleaseFn func(ctx context.Context, sessionID string) error

// Strategy implements the Unlocking stage.
type Strategy struct {
	release ReleaseFn
	onNext  machine.Strategy[ritual.RunState]
}

// New constructs the release strategy.
func New(release ReleaseFn, onNext machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{release: release, onNext: onNext}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageUnlocking }

// Run deletes the remote lease iff this session still owns it.
func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "unlock"})
	ctx := context.WithoutCancel(parentCtx)

	if rs.SessionID != "" {
		_ = s.release(ctx, rs.SessionID)
		publish(rs.Bus, ritual.LockReleasedInfo{RunID: rs.RunID})
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "unlock"})
	return s.onNext, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
