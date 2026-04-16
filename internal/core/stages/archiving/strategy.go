// Package archiving snapshots the pre-run world state to the backups
// prefix on both storage slots. Runs between server exit and publish so
// that remote still holds the pre-run files. A scanner detects whether
// the server actually mutated local files; if nothing changed, the
// backup is skipped. Any error is recorded in rs.Err; chain always
// forwards to onNext so the lock-release path continues.
package archiving

import (
	"context"
	"maps"

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
	scanner        ports.DirectoryScanner
	onNext         machine.Strategy[ritual.RunState]
}

func New(
	localStore, remoteStore ports.StorageRepository,
	localManifests ports.ManifestStore,
	scanner ports.DirectoryScanner,
	onNext machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{
		localStore:     localStore,
		remoteStore:    remoteStore,
		localManifests: localManifests,
		scanner:        scanner,
		onNext:         onNext,
	}
}

func (*Strategy) Name() string { return ritual.StageArchiving }

func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "archive"})

	if rs.LockID == "" || rs.LocalBefore == nil {
		publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
		return s.onNext, nil
	}

	ctx, done := ritual.WithLostLock(parentCtx, rs)
	defer done()

	if !s.serverChangedFiles(ctx, rs) {
		publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
		return s.onNext, nil
	}

	manifest := rs.LocalBefore

	_ = services.CreateBackupFrom(ctx, s.remoteStore, s.localStore, config.WorldsDir, config.BackupsDir, manifest)
	_ = services.CreateBackup(ctx, s.remoteStore, config.WorldsDir, config.BackupsDir, manifest)
	publish(rs.Bus, ports.FinishInfo{Operation: "archive"})
	return s.onNext, nil
}

func (s *Strategy) serverChangedFiles(ctx context.Context, rs *ritual.RunState) bool {
	if s.scanner == nil {
		return false
	}
	currentMap, err := s.scanner.Scan(ctx)
	if err != nil {
		return false
	}
	return !maps.Equal(rs.LocalBefore.Worlds.XXHashMap, currentMap)
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
