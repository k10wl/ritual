package statemachine

import (
	"context"
	"fmt"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// ExitingState runs exit updaters, takes backups if the session dirtied the
// world, applies retentions, and releases locks. It is the documented
// exception to ctx-discipline — runs with context.WithoutCancel so that
// upstream cancellation (GUI close, ctrl-C) cannot abort unlock.
type ExitingState struct {
	exitUpdaters    []ports.UpdaterService
	retentions      []ports.RetentionService
	localStore      ports.StorageRepository
	remoteStore     ports.StorageRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	factory         StateFactory
	lockID          string
	localBefore     *domain.Manifest
	remoteBefore    *domain.Manifest
}

func NewExitingState(
	exitUpdaters []ports.UpdaterService,
	retentions []ports.RetentionService,
	localStore, remoteStore ports.StorageRepository,
	local, remote ports.ManifestStore,
	bus ports.EventBus,
	f StateFactory,
	lockID string,
	localBefore, remoteBefore *domain.Manifest,
) *ExitingState {
	return &ExitingState{
		exitUpdaters:    exitUpdaters,
		retentions:      retentions,
		localStore:      localStore,
		remoteStore:     remoteStore,
		localManifests:  local,
		remoteManifests: remote,
		bus:             bus,
		factory:         f,
		lockID:          lockID,
		localBefore:     localBefore,
		remoteBefore:    remoteBefore,
	}
}

func (*ExitingState) Name() StateName { return Exiting }

func (s *ExitingState) Handle(parentCtx context.Context) (Handler, error) {
	publish(s.bus, ports.StartInfo{Operation: "exit"})

	if s.lockID == "" {
		publish(s.bus, ports.FinishInfo{Operation: "exit"})
		return nil, nil
	}

	ctx := context.WithoutCancel(parentCtx)

	for i, u := range s.exitUpdaters {
		if err := u.Run(ctx); err != nil {
			return s.factory.Failed(Exiting, fmt.Errorf("exit updater %d: %w", i, err)), nil
		}
	}

	if s.shouldBackup() {
		localAfter, err := s.localManifests.Get(ctx)
		if err == nil && localAfter != nil {
			if err := services.CreateBackup(ctx, s.localStore, config.WorldsDir, config.BackupsDir, localAfter); err != nil {
				return s.factory.Failed(Exiting, fmt.Errorf("local backup: %w", err)), nil
			}
			if err := services.CreateBackup(ctx, s.remoteStore, config.WorldsDir, config.BackupsDir, localAfter); err != nil {
				return s.factory.Failed(Exiting, fmt.Errorf("remote backup: %w", err)), nil
			}
		}
	}

	for i, r := range s.retentions {
		if err := r.Apply(ctx); err != nil {
			return s.factory.Failed(Exiting, fmt.Errorf("retention %d: %w", i, err)), nil
		}
	}

	s.releaseLocks(ctx)
	publish(s.bus, ports.FinishInfo{Operation: "exit"})
	return nil, nil
}

func (s *ExitingState) shouldBackup() bool {
	if s.localBefore == nil || s.remoteBefore == nil {
		return false
	}
	return services.ShouldBackup(s.localBefore.Worlds.SyncState, s.remoteBefore.Worlds.SyncState)
}

// releaseLocks clears LockedBy on either manifest if it still owns our lockID,
// stamps RitualVersion, and saves. Best-effort — errors are swallowed so
// exit flow always completes.
func (s *ExitingState) releaseLocks(ctx context.Context) {
	if local, err := s.localManifests.Get(ctx); err == nil && local != nil && local.LockedBy == s.lockID {
		local.Unlock()
		local.RitualVersion = config.AppVersion
		_ = s.localManifests.Save(ctx, local)
	}
	if remote, err := s.remoteManifests.Get(ctx); err == nil && remote != nil && remote.LockedBy == s.lockID {
		remote.Unlock()
		remote.RitualVersion = config.AppVersion
		_ = s.remoteManifests.Save(ctx, remote)
	}
}

func (f *factory) Exiting(lockID string, localBefore, remoteBefore *domain.Manifest) Handler {
	return NewExitingState(
		f.d.ExitUpdaters, f.d.Retentions,
		f.d.LocalStore, f.d.RemoteStore,
		f.d.LocalManifests, f.d.RemoteManifests,
		f.d.Bus, f,
		lockID, localBefore, remoteBefore,
	)
}
