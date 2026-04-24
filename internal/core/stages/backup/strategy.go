// Package backup snapshots the post-publish canonical world state to
// the backups prefix on both sides. Runs after Publishing, so local and
// remote are in sync; each side backs up from its own worlds/ to its own
// backups/{ts}/ via same-storage Copy (no cross-storage bytes). Skipped
// when no mutation this session (manifest XXHashMap unchanged). Errors
// are reported as ErrorInfo on the bus; chain always forwards to onNext
// so Unlocking releases the lock.
package backup

import (
	"context"
	"maps"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/services"
)

// Strategy implements the Backup stage.
type Strategy struct {
	localStore     ports.StorageRepository
	remoteStore    ports.StorageRepository
	localManifests ports.ManifestStore
	onNext         machine.Strategy[ritual.RunState]
}

// New builds a Backup Strategy.
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

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageBackup }

// Run snapshots post-publish worlds on both sides. Skipped when the run
// acquired no lock, or when the post-publish manifest's XXHashMap matches
// the pre-run snapshot (no mutation this session).
func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "backup"})
	defer publish(rs.Bus, ritual.FinishInfo{Operation: "backup"})

	if rs.SessionID == "" || rs.LocalBefore == nil {
		return s.onNext, nil
	}

	ctx, done := ritual.WithLostLock(parentCtx, rs)
	defer done()

	current, err := s.localManifests.Get(ctx)
	if err != nil || current == nil {
		publish(rs.Bus, ritual.ErrorInfo{Operation: "backup", Err: err})
		return s.onNext, nil
	}
	if maps.Equal(rs.LocalBefore.Worlds.XXHashMap, current.Worlds.XXHashMap) {
		return s.onNext, nil
	}

	s.snapshot(ctx, rs, s.localStore, current)
	s.snapshot(ctx, rs, s.remoteStore, current)
	return s.onNext, nil
}

func (s *Strategy) snapshot(ctx context.Context, rs *ritual.RunState, store ports.StorageRepository, m *domain.Manifest) {
	if err := services.CreateBackup(ctx, store, config.WorldsDir, config.BackupsDir, m); err != nil {
		publish(rs.Bus, ritual.ErrorInfo{Operation: "backup", Err: err})
	}
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
