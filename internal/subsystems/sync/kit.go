// Package sync builds the scanner, filter, and sync-service wiring
// for both world and server directories. Returns pre-assembled
// updater slices for the state-machine stages.
package sync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/services"
	"ritual/internal/core/stages/pulling"
	"strings"
	"time"
)

// Kit is the result of Build — holds everything the stage chain needs
// from the sync subsystem.
type Kit struct {
	Puller        ports.Puller         // pulling stage: download refs + blobs
	Applier       ports.Applier        // pulling stage: materialise ref into workdir
	HeadResolver  pulling.HeadResolver // pulling stage: pick HEAD ref id from remote
	Committer     ports.Committer      // committing stage: stage workdir into local refs
	Pusher        ports.Pusher         // pushing stage: mirror local refs to remote
	CommitTargets []string             // committing stage: glob targets for ref selection
	WorldSync     ports.SyncService    // heartbeat uses this for live sync during server running
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

	// Refs V2 pipeline: ParallelRunner(10) weight-desc dispatch shared by
	// Pull (remote → local) and Apply (local blobs → workdir). Workdir
	// storage targets the worlds directory — Apply materialises there.
	const pullConcurrency = 10
	runner := adapters.NewParallelRunner(pullConcurrency)
	worldsRoot, err := os.OpenRoot(worldsPath)
	if err != nil {
		return Kit{}, fmt.Errorf("open worlds root: %w", err)
	}
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "workdir")
	if err != nil {
		return Kit{}, fmt.Errorf("workdir storage: %w", err)
	}
	puller := refs.NewPuller(remoteStorage, localStorage, runner)
	applier := refs.NewApplier(localStorage, workdirStorage, worldScanner, runner)
	committer := refs.NewCommitter(worldScanner, workdirStorage, localStorage, runner)
	pusher := refs.NewPusher(localStorage, remoteStorage, runner)
	headResolver := newRemoteHeadResolver(remoteStorage)

	_ = serverScanner
	_ = serverSync
	return Kit{
		Puller:        puller,
		Applier:       applier,
		HeadResolver:  headResolver,
		Committer:     committer,
		Pusher:        pusher,
		CommitTargets: []string{"**"},
		WorldSync:     worldSync,
	}, nil
}

// newRemoteHeadResolver lists remote refs/ and returns the lexicographic
// max timestamp as the HEAD ref id. Empty list → explicit error so the
// stage chain routes to onFail with a human-readable cause.
func newRemoteHeadResolver(remote ports.StorageRepository) pulling.HeadResolver {
	return func(ctx context.Context) (domain.RefID, error) {
		keys, err := remote.List(ctx, "refs/")
		if err != nil {
			return "", fmt.Errorf("list refs: %w", err)
		}
		var head string
		for _, key := range keys {
			name := strings.TrimPrefix(key, "refs/")
			name = strings.TrimSuffix(name, ".json")
			if name == "" {
				continue
			}
			if name > head {
				head = name
			}
		}
		if head == "" {
			return "", errors.New("no refs on remote")
		}
		return domain.RefID(head), nil
	}
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
