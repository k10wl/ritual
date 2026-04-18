// Package unlocking rolls back a partial lock acquisition. Idempotent:
// only clears manifest slots whose LockedBy still matches rs.LockID.
// Runs under context.WithoutCancel so shutdown cannot abort rollback.
package unlocking

import (
	"context"
	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Unlocking stage.
type Strategy struct {
	local  ports.ManifestStore
	remote ports.ManifestStore
	onNext machine.Strategy[ritual.RunState]
}

// New constructs the rollback strategy. onNext is typically a Failed
// strategy bound to the origin stage (commonly ritual.StageLocking).
func New(local, remote ports.ManifestStore, onNext machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{local: local, remote: remote, onNext: onNext}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageUnlocking }

// Run clears the lock slots on local + remote manifests if still held by rs.LockID.
func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "unlock"})
	// Unlocking ignores lost-lock: if we already lost the lease, the
	// manifest's LockedBy check below will skip the Save, so the call
	// is idempotent and safe even under a hostile takeover.
	ctx := context.WithoutCancel(parentCtx)

	if local, err := s.local.Get(ctx); err == nil && local != nil && local.LockedBy == rs.LockID {
		local.Unlock()
		local.RitualVersion = config.AppVersion
		_ = s.local.Save(ctx, local)
	}
	if remote, err := s.remote.Get(ctx); err == nil && remote != nil && remote.LockedBy == rs.LockID {
		remote.Unlock()
		remote.RitualVersion = config.AppVersion
		_ = s.remote.Save(ctx, remote)
	}
	if rs.LockID != "" {
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
