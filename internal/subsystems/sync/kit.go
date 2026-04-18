// Package sync builds the scanner, filter, and sync-service wiring
// for both world and server directories. Returns pre-assembled
// updater slices for the state-machine stages.
package sync

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
	"time"
)

// Kit is the result of Build — holds everything the stage chain needs
// from the sync subsystem.
type Kit struct {
	Updaters     []ports.UpdaterService // check → fetch uses these
	ExitUpdaters []ports.UpdaterService // serve → publish uses these
	WorldSync    ports.SyncService      // heartbeat uses this for live sync during server running
}

// Build wires scanners, filters, sync services, and updaters. It reads
// the local manifest once to seed the mtime scanner. Errors during
// filter parsing or ritual-updater creation are returned; partial
// constructions never leak.
func Build(
	ctx context.Context,
	workRoot *os.Root,
	localStorage, remoteStorage ports.StorageRepository,
	localManifests, remoteManifests ports.ManifestStore,
	bus ports.EventBus,
) (Kit, error) {
	ritualUpdater, err := services.NewRitualUpdater(localManifests, remoteManifests, remoteStorage, config.AppVersion)
	if err != nil {
		return Kit{}, fmt.Errorf("ritual updater: %w", err)
	}

	hostname, _ := os.Hostname()
	sessionID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	localStaging := filepath.Join(config.TempRitualPath(), fmt.Sprintf(config.SyncStagingPattern, time.Now().UnixNano()))
	remoteStaging := "sync/" + sessionID

	worldsPath := filepath.Join(config.RootPath, config.WorldsDir)
	serverPath := filepath.Join(config.RootPath, config.ServerDir)
	_ = os.MkdirAll(worldsPath, config.DirPermission)
	_ = os.MkdirAll(serverPath, config.DirPermission)

	worldsFS, _ := fs.Sub(workRoot.FS(), config.WorldsDir)
	serverFS, _ := fs.Sub(workRoot.FS(), config.ServerDir)

	worldsFilter, err := adapters.ParseRitualSync(worldsFS)
	if err != nil {
		return Kit{}, fmt.Errorf("worlds .ritualsync: %w", err)
	}
	serverFilter, err := adapters.ParseRitualSync(serverFS)
	if err != nil {
		return Kit{}, fmt.Errorf("server .ritualsync: %w", err)
	}

	worldScanner := adapters.NewFilteredScanner(worldInnerScanner(ctx, worldsPath, localManifests, worldsFS), worldsFilter)
	serverScanner := adapters.NewFilteredScanner(adapters.NewFullScanner(serverFS), serverFilter)

	worldSync := services.NewSyncService(
		worldScanner, localStorage, remoteStorage, bus,
		services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
		filepath.Join(localStaging, config.WorldsDir),
		remoteStaging+"/"+config.WorldsDir,
	)
	serverSync := services.NewSyncService(
		serverScanner, localStorage, remoteStorage, bus,
		services.SyncConfig{Prefix: config.ServerDir, LocalDir: serverPath},
		filepath.Join(localStaging, config.ServerDir),
		remoteStaging+"/"+config.ServerDir,
	)

	worldDown := services.NewSyncDownloadUpdater(worldSync, localManifests, remoteManifests, func(m *domain.Manifest) *domain.SyncState {
		return &m.Worlds.SyncState
	})
	serverDown := services.NewSyncDownloadUpdater(serverSync, localManifests, remoteManifests, func(m *domain.Manifest) *domain.SyncState {
		return &m.Server.SyncState
	})
	worldUp := services.NewSyncUploader(worldSync, localManifests, remoteManifests, func(m *domain.Manifest) *domain.SyncState {
		return &m.Worlds.SyncState
	})

	_ = serverScanner
	return Kit{
		Updaters:     []ports.UpdaterService{ritualUpdater, serverDown, worldDown},
		ExitUpdaters: []ports.UpdaterService{worldUp},
		WorldSync:    worldSync,
	}, nil
}

func worldInnerScanner(ctx context.Context, worldsPath string, localManifests ports.ManifestStore, worldsFS fs.FS) ports.DirectoryScanner {
	m, err := localManifests.Get(ctx)
	if err != nil || m == nil || len(m.Worlds.XXHashMap) == 0 {
		return adapters.NewFullScanner(worldsFS)
	}
	mtime, err := adapters.NewMtimeScanner(worldsPath, m.Worlds.XXHashSyncAt, m.Worlds.XXHashMap)
	if err != nil {
		return adapters.NewFullScanner(worldsFS)
	}
	return mtime
}
