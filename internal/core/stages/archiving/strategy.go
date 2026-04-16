// Package archiving snapshots the world to the backups prefix on both
// storage slots, gated by services.ShouldBackup. Runs under
// context.WithoutCancel. Any error is recorded in rs.Err; chain always
// forwards to onNext so the lock-release path continues.
package archiving

import (
	"context"
	"fmt"

	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/services"
)

type Strategy struct {
	localStore     ports.StorageRepository
	remoteStore    ports.StorageRepository
	localManifests ports.ManifestStore
	onNext         machine.Strategy[ritual.RunState]
}

func New(
	localStore, remoteStore ports.StorageRepository,
	localManifests ports.ManifestStore,
	onNext machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{
		localStore:     localStore,
		remoteStore:    remoteStore,
		localManifests: localManifests,
		onNext:         onNext,
	}
}

func (*Strategy) Name() string { return ritual.StageArchiving }

func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "archive"})

	if rs.LockID == "" || !shouldBackup(rs) {
		publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
		return s.onNext, nil
	}

	ctx, done := ritual.WithLostLock(parentCtx, rs)
	defer done()
	localAfter, err := s.localManifests.Get(ctx)
	if err != nil || localAfter == nil {
		publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
		return s.onNext, nil
	}

	if err := services.CreateBackup(ctx, s.localStore, config.WorldsDir, config.BackupsDir, localAfter); err != nil {
		rs.Err = fmt.Errorf("local backup: %w", err)
		return s.onNext, nil
	}
	if err := services.CreateBackup(ctx, s.remoteStore, config.WorldsDir, config.BackupsDir, localAfter); err != nil {
		rs.Err = fmt.Errorf("remote backup: %w", err)
		return s.onNext, nil
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
	return s.onNext, nil
}

func shouldBackup(rs *ritual.RunState) bool {
	if rs.LocalBefore == nil || rs.RemoteBefore == nil {
		return false
	}
	return services.ShouldBackup(rs.LocalBefore.Worlds.SyncState, rs.RemoteBefore.Worlds.SyncState)
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
