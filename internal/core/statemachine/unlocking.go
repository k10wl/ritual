package statemachine

import (
	"context"

	"ritual/internal/core/ports"
)

// UnlockingState rolls back Locking when the second Save (remote) failed.
// Idempotent: only clears manifests whose LockedBy still matches our lockID.
// Always routes to Failed(Locking, cause) regardless of rollback result.
type UnlockingState struct {
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	factory         StateFactory
	lockID          string
	cause           error
}

func NewUnlockingState(
	local, remote ports.ManifestStore,
	bus ports.EventBus,
	f StateFactory,
	lockID string,
	cause error,
) *UnlockingState {
	return &UnlockingState{
		localManifests:  local,
		remoteManifests: remote,
		bus:             bus,
		factory:         f,
		lockID:          lockID,
		cause:           cause,
	}
}

func (*UnlockingState) Name() StateName { return Unlocking }

func (s *UnlockingState) Handle(ctx context.Context) (Handler, error) {
	publish(s.bus, ports.StartInfo{Operation: "unlock-rollback"})

	if local, err := s.localManifests.Get(ctx); err == nil && local != nil && local.LockedBy == s.lockID {
		local.Unlock()
		_ = s.localManifests.Save(ctx, local)
	}
	if remote, err := s.remoteManifests.Get(ctx); err == nil && remote != nil && remote.LockedBy == s.lockID {
		remote.Unlock()
		_ = s.remoteManifests.Save(ctx, remote)
	}
	publish(s.bus, ports.FinishInfo{Operation: "unlock-rollback"})
	return s.factory.Failed(Locking, s.cause), nil
}

func (f *factory) Unlocking(lockID string, cause error) Handler {
	return NewUnlockingState(f.d.LocalManifests, f.d.RemoteManifests, f.d.Bus, f, lockID, cause)
}
