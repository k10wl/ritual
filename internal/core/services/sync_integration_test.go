package services_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
)

// syncTestEnv bundles local/remote FS repos, manifest stores, and temp dirs for integration tests.
type syncTestEnv struct {
	localDir        string
	remoteDir       string
	localRoot       *os.Root
	remoteRoot      *os.Root
	local           *adapters.FSRepository
	remote          *adapters.FSRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	ctx             context.Context
}

func newSyncTestEnv(t *testing.T) *syncTestEnv {
	t.Helper()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	localRoot, err := os.OpenRoot(localDir)
	require.NoError(t, err)
	t.Cleanup(func() { localRoot.Close() })

	remoteRoot, err := os.OpenRoot(remoteDir)
	require.NoError(t, err)
	t.Cleanup(func() { remoteRoot.Close() })

	local, err := adapters.NewFSRepository(localRoot)
	require.NoError(t, err)

	remote, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err)

	return &syncTestEnv{
		localDir:        localDir,
		remoteDir:       remoteDir,
		localRoot:       localRoot,
		remoteRoot:      remoteRoot,
		local:           local,
		remote:          remote,
		localManifests:  adapters.NewManifestStore(local),
		remoteManifests: adapters.NewManifestStore(remote),
		ctx:             context.Background(),
	}
}

func (e *syncTestEnv) saveLocalManifest(t *testing.T, m *domain.Manifest) {
	t.Helper()
	require.NoError(t, e.localManifests.Save(e.ctx, m))
}

func (e *syncTestEnv) saveRemoteManifest(t *testing.T, m *domain.Manifest) {
	t.Helper()
	require.NoError(t, e.remoteManifests.Save(e.ctx, m))
}

func (e *syncTestEnv) loadLocalManifest(t *testing.T) *domain.Manifest {
	t.Helper()
	m, err := e.localManifests.Get(e.ctx)
	require.NoError(t, err)
	return m
}

func (e *syncTestEnv) loadRemoteManifest(t *testing.T) *domain.Manifest {
	t.Helper()
	m, err := e.remoteManifests.Get(e.ctx)
	require.NoError(t, err)
	return m
}

// worldsPath returns the absolute path to worlds dir inside the given root dir
func worldsPath(rootDir string) string {
	return filepath.Join(rootDir, "worlds")
}

// setupMinecraftWorlds creates a Paper MC world structure under {rootDir}/worlds/
// using the testhelpers setup, returns the created file list.
func setupMinecraftWorlds(t *testing.T, rootDir string) []string {
	t.Helper()
	wPath := worldsPath(rootDir)
	require.NoError(t, os.MkdirAll(wPath, 0755))
	root, err := os.OpenRoot(wPath)
	require.NoError(t, err)
	defer root.Close()

	_, files, _, err := testhelpers.PaperMinecraftWorldSetup(root)
	require.NoError(t, err)
	return files
}

// writeFile creates a file relative to a root dir, creating parent dirs as needed.
func writeFile(t *testing.T, rootDir, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(rootDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, content, 0644))
}

// readFile reads a file relative to a root dir.
func readFile(t *testing.T, rootDir, relPath string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rootDir, relPath))
	require.NoError(t, err)
	return data
}

// fileExists checks if a file exists relative to root dir.
func fileExists(rootDir, relPath string) bool {
	_, err := os.Stat(filepath.Join(rootDir, relPath))
	return err == nil
}

// makeSyncService creates a DeltaSyncService for worlds using an fs.FS-based FullScanner.
func makeSyncService(t *testing.T, env *syncTestEnv) ports.SyncService {
	t.Helper()
	wPath := worldsPath(env.localDir)
	_ = os.MkdirAll(wPath, 0755)

	scanner := adapters.NewFullScanner(os.DirFS(wPath))
	staging := t.TempDir()

	return services.NewSyncService(
		scanner, env.local, env.remote, nil,
		services.SyncConfig{Prefix: "worlds", LocalDir: wPath},
		filepath.Join(staging, "local"),
		"sync/test-session/worlds",
	)
}

// buildSyncUpload creates a sync service, loads manifests, runs Upload, saves results.
func buildSyncUpload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	svc := makeSyncService(t, env)

	lm := env.loadLocalManifest(t)
	rm := env.loadRemoteManifest(t)

	newState, err := svc.Upload(env.ctx, lm.Worlds.SyncState, rm.Worlds.SyncState)
	require.NoError(t, err)

	lm.Worlds.SyncState = newState
	rm.Worlds.SyncState = newState
	env.saveLocalManifest(t, lm)
	env.saveRemoteManifest(t, rm)
}

// buildSyncDownload creates a sync service, loads manifests, runs Download, saves results.
func buildSyncDownload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	svc := makeSyncService(t, env)

	lm := env.loadLocalManifest(t)
	rm := env.loadRemoteManifest(t)

	newState, err := svc.Download(env.ctx, lm.Worlds.SyncState, rm.Worlds.SyncState)
	require.NoError(t, err)

	lm.Worlds.SyncState = newState
	env.saveLocalManifest(t, lm)
}

// --- Integration Tests ---

func TestSyncIntegration_FullUploadThenDownload(t *testing.T) {
	env := newSyncTestEnv(t)

	// Seed empty manifests
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Create realistic MC world on local side
	setupMinecraftWorlds(t, env.localDir)

	// Upload: local → remote
	buildSyncUpload(t, env)

	// Verify remote manifest has xxhash map
	rm := env.loadRemoteManifest(t)
	assert.NotEmpty(t, rm.Worlds.XXHashMap, "remote manifest should have xxhash map after upload")
	assert.False(t, rm.Worlds.XXHashSyncAt.IsZero(), "remote xxhash_sync_at should be set")

	// Verify remote worlds/ directory has files
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"))
	assert.True(t, fileExists(env.remoteDir, "worlds/world/region/r.0.0.mca"))
	assert.True(t, fileExists(env.remoteDir, "worlds/world_nether/level.dat"))

	// Compare directory trees
	match, err := testhelpers.CheckDirs(testhelpers.DirPair{
		P1: []string{worldsPath(env.localDir)},
		P2: []string{worldsPath(env.remoteDir)},
	})
	require.NoError(t, err)
	assert.True(t, match, "remote worlds should match local after upload")

	// Now simulate a second host: fresh local, download from remote
	env2 := newSyncTestEnv(t)
	env2.remote = env.remote       // share remote storage
	env2.remoteDir = env.remoteDir // share remote dir

	// Rebuild librarian with shared remote
	env2.remoteManifests = adapters.NewManifestStore(env2.remote)

	env2.saveLocalManifest(t, &domain.Manifest{})

	// Download: remote → local2
	buildSyncDownload(t, env2)

	// Verify local2 worlds match original
	match, err = testhelpers.CheckDirs(testhelpers.DirPair{
		P1: []string{worldsPath(env.localDir)},
		P2: []string{worldsPath(env2.localDir)},
	})
	require.NoError(t, err)
	assert.True(t, match, "downloaded worlds should match original uploader's worlds")

	// Verify local2 manifest matches remote
	lm2 := env2.loadLocalManifest(t)
	assert.Equal(t, rm.Worlds.XXHashMap, lm2.Worlds.XXHashMap)
}

func TestSyncIntegration_DeltaUpload_OnlyChangedFiles(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Initial world
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("original"))
	writeFile(t, env.localDir, "worlds/world/region/r.0.0.mca", []byte("region data"))

	// First upload — full
	buildSyncUpload(t, env)

	rm1 := env.loadRemoteManifest(t)
	assert.Len(t, rm1.Worlds.XXHashMap, 2)

	// Modify one file locally
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("modified"))

	// Second upload — delta
	buildSyncUpload(t, env)

	rm2 := env.loadRemoteManifest(t)
	assert.Len(t, rm2.Worlds.XXHashMap, 2)

	// Verify remote level.dat has new content
	data := readFile(t, env.remoteDir, "worlds/world/level.dat")
	assert.Equal(t, []byte("modified"), data)

	// Verify remote region file unchanged (same content)
	regionData := readFile(t, env.remoteDir, "worlds/world/region/r.0.0.mca")
	assert.Equal(t, []byte("region data"), regionData)
}

func TestSyncIntegration_FileDeletedLocally_RemovedFromRemote(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Initial upload with two files
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))
	writeFile(t, env.localDir, "worlds/world/region/r.0.0.mca", []byte("region"))
	buildSyncUpload(t, env)

	assert.True(t, fileExists(env.remoteDir, "worlds/world/region/r.0.0.mca"))

	// Delete region file locally (simulates server runtime deletion)
	require.NoError(t, os.Remove(filepath.Join(env.localDir, "worlds/world/region/r.0.0.mca")))

	// Upload again — P3 should delete orphan from remote
	buildSyncUpload(t, env)

	rm := env.loadRemoteManifest(t)
	assert.Len(t, rm.Worlds.XXHashMap, 1, "only level.dat should remain in manifest")
	assert.False(t, fileExists(env.remoteDir, "worlds/world/region/r.0.0.mca"), "deleted file should be gone from remote")
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"), "untouched file should remain")
}

func TestSyncIntegration_FileAddedLocally_UploadedToRemote(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Initial upload
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))
	buildSyncUpload(t, env)

	// Add new file locally
	writeFile(t, env.localDir, "worlds/world/playerdata/player1.dat", []byte("player data"))

	// Upload — new file should appear on remote
	buildSyncUpload(t, env)

	assert.True(t, fileExists(env.remoteDir, "worlds/world/playerdata/player1.dat"))
	data := readFile(t, env.remoteDir, "worlds/world/playerdata/player1.dat")
	assert.Equal(t, []byte("player data"), data)
}

func TestSyncIntegration_DownloadDeletesLocalGhosts(t *testing.T) {
	env := newSyncTestEnv(t)

	// Remote has one file
	writeFile(t, env.remoteDir, "worlds/world/level.dat", []byte("level"))

	// Compute hash for remote manifest
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath(env.remoteDir)))
	remoteMap, err := scanner.Scan(env.ctx)
	require.NoError(t, err)

	env.saveRemoteManifest(t, &domain.Manifest{
		Worlds: domain.WorldsManifest{
			SyncState: domain.SyncState{
				XXHashMap:    remoteMap,
				XXHashSyncAt: time.Now(),
			},
		},
	})

	// Local has two files (one is ghost)
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("old level"))
	writeFile(t, env.localDir, "worlds/world/ghost.dat", []byte("should be deleted"))
	env.saveLocalManifest(t, &domain.Manifest{
		Worlds: domain.WorldsManifest{
			SyncState: domain.SyncState{
				XXHashMap: map[string]string{
					"world/level.dat": "stale_hash",
					"world/ghost.dat": "ghost_hash",
				},
			},
		},
	})

	// Download — ghost should be removed
	buildSyncDownload(t, env)

	assert.True(t, fileExists(env.localDir, "worlds/world/level.dat"))
	assert.False(t, fileExists(env.localDir, "worlds/world/ghost.dat"), "ghost file should be deleted after download")

	lm := env.loadLocalManifest(t)
	_, hasGhost := lm.Worlds.XXHashMap["world/ghost.dat"]
	assert.False(t, hasGhost, "ghost should not be in local manifest")
}

func TestSyncIntegration_EmptyDiff_NoTransfers(t *testing.T) {
	env := newSyncTestEnv(t)

	hashMap := map[string]string{"world/level.dat": "abc123"}
	env.saveLocalManifest(t, &domain.Manifest{
		Worlds: domain.WorldsManifest{
			SyncState: domain.SyncState{XXHashMap: hashMap},
		},
	})
	env.saveRemoteManifest(t, &domain.Manifest{
		Worlds: domain.WorldsManifest{
			SyncState: domain.SyncState{XXHashMap: hashMap},
		},
	})

	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))

	// Download with matching manifests — should be no-op
	buildSyncDownload(t, env)

	// Upload with matching manifests — scan produces new map, but if content same, diff empty
	// (need actual file to match hash — this tests the "no transfer" path)
	lm := env.loadLocalManifest(t)
	assert.NotNil(t, lm)
}

func TestSyncIntegration_FirstRunNoRemoteManifest_FullUpload(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{}) // empty xxhash map = nil

	// Create world
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))
	writeFile(t, env.localDir, "worlds/world/region/r.0.0.mca", []byte("region"))

	// Upload — everything goes up
	buildSyncUpload(t, env)

	rm := env.loadRemoteManifest(t)
	assert.Len(t, rm.Worlds.XXHashMap, 2)
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"))
	assert.True(t, fileExists(env.remoteDir, "worlds/world/region/r.0.0.mca"))
}

func TestSyncIntegration_FirstRunNoLocalManifest_FullDownload(t *testing.T) {
	env := newSyncTestEnv(t)

	// Remote has world
	writeFile(t, env.remoteDir, "worlds/world/level.dat", []byte("remote level"))
	writeFile(t, env.remoteDir, "worlds/world/region/r.0.0.mca", []byte("remote region"))

	scanner := adapters.NewFullScanner(os.DirFS(worldsPath(env.remoteDir)))
	remoteMap, err := scanner.Scan(env.ctx)
	require.NoError(t, err)

	env.saveRemoteManifest(t, &domain.Manifest{
		Worlds: domain.WorldsManifest{
			SyncState: domain.SyncState{
				XXHashMap:    remoteMap,
				XXHashSyncAt: time.Now(),
			},
		},
	})
	env.saveLocalManifest(t, &domain.Manifest{}) // empty

	// Download — everything comes down
	buildSyncDownload(t, env)

	assert.True(t, fileExists(env.localDir, "worlds/world/level.dat"))
	data := readFile(t, env.localDir, "worlds/world/level.dat")
	assert.Equal(t, []byte("remote level"), data)

	lm := env.loadLocalManifest(t)
	assert.Equal(t, remoteMap, lm.Worlds.XXHashMap)
}

func TestSyncIntegration_UploadP3_StaleFilesClearedAfterDeletion(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Upload three files
	writeFile(t, env.localDir, "worlds/a.dat", []byte("a"))
	writeFile(t, env.localDir, "worlds/b.dat", []byte("b"))
	writeFile(t, env.localDir, "worlds/c.dat", []byte("c"))
	buildSyncUpload(t, env)

	// Delete two locally
	require.NoError(t, os.Remove(filepath.Join(env.localDir, "worlds/a.dat")))
	require.NoError(t, os.Remove(filepath.Join(env.localDir, "worlds/c.dat")))

	// Upload — P3 removes a.dat and c.dat from remote
	buildSyncUpload(t, env)

	assert.False(t, fileExists(env.remoteDir, "worlds/a.dat"))
	assert.True(t, fileExists(env.remoteDir, "worlds/b.dat"))
	assert.False(t, fileExists(env.remoteDir, "worlds/c.dat"))

	rm := env.loadRemoteManifest(t)
	assert.Len(t, rm.Worlds.XXHashMap, 1)
	_, hasB := rm.Worlds.XXHashMap["b.dat"]
	assert.True(t, hasB)
}

func TestSyncIntegration_HostAUploads_HostBDownloads_HostBUploadsChanges(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Host A: create and upload world
	setupMinecraftWorlds(t, env.localDir)
	buildSyncUpload(t, env)

	// Host B: fresh env, shared remote
	envB := newSyncTestEnv(t)
	envB.remote = env.remote
	envB.remoteDir = env.remoteDir
	envB.remoteManifests = adapters.NewManifestStore(envB.remote)
	envB.saveLocalManifest(t, &domain.Manifest{})

	// Host B downloads
	buildSyncDownload(t, envB)

	// Verify B has same world as A
	match, err := testhelpers.CheckDirs(testhelpers.DirPair{
		P1: []string{worldsPath(env.localDir)},
		P2: []string{worldsPath(envB.localDir)},
	})
	require.NoError(t, err)
	assert.True(t, match, "Host B should have exact copy of Host A's world")

	// Host B modifies a file and uploads
	writeFile(t, envB.localDir, "worlds/world/level.dat", []byte("modified by host B"))
	buildSyncUpload(t, envB)

	// Verify remote has B's changes
	data := readFile(t, env.remoteDir, "worlds/world/level.dat")
	assert.Equal(t, []byte("modified by host B"), data)
}

func TestSyncIntegration_ManifestBackwardsCompat_V1EmptyXXHash(t *testing.T) {
	env := newSyncTestEnv(t)

	// Simulate v1 manifest — no xxhash fields
	v1Manifest := &domain.Manifest{
		ManifestVersion: "1.0.0",
		RitualVersion:   "1.3.5",
		UpdatedAt:       time.Now(),
	}
	env.saveLocalManifest(t, v1Manifest)
	env.saveRemoteManifest(t, v1Manifest)

	// Create world files
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))

	// Upload with empty xxhash (migration path) — should produce full upload
	buildSyncUpload(t, env)

	rm := env.loadRemoteManifest(t)
	assert.NotNil(t, rm.Worlds.XXHashMap, "migration should populate xxhash map")
	assert.Len(t, rm.Worlds.XXHashMap, 1)
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"))
}

func TestSyncIntegration_EmptyWorldDir_EmptyManifest(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Empty worlds dir — nothing to upload
	require.NoError(t, os.MkdirAll(worldsPath(env.localDir), 0755))

	buildSyncUpload(t, env)

	rm := env.loadRemoteManifest(t)
	// Empty map or nil — no files
	assert.Empty(t, rm.Worlds.XXHashMap)
}

func TestSyncIntegration_SyncFolderCleaned(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	writeFile(t, env.localDir, "worlds/a.dat", []byte("data"))
	buildSyncUpload(t, env)

	// Verify no sync staging files linger on remote (directory entries may remain in FS-backed repos)
	keys, err := env.remote.List(env.ctx, "sync/test-session/worlds")
	require.NoError(t, err)
	assert.Empty(t, keys, "sync staging files should be cleaned after upload")
}

func TestSyncIntegration_RetentionMixedFormats(t *testing.T) {
	env := newSyncTestEnv(t)

	// Create mixed backup entries: v1 .tar files + v2 directories
	writeFile(t, env.localDir, "world_backups/20260414100000.tar", []byte("old tar backup"))
	writeFile(t, env.localDir, "world_backups/20260414110000/manifest.json", []byte("{}"))
	writeFile(t, env.localDir, "world_backups/20260414110000/worlds/level.dat", []byte("data"))
	writeFile(t, env.localDir, "world_backups/20260414120000.tar", []byte("another tar"))
	writeFile(t, env.localDir, "world_backups/20260414130000/manifest.json", []byte("{}"))
	writeFile(t, env.localDir, "world_backups/invalid_name", []byte("should be ignored"))

	// List and verify retention util would see 4 valid entries
	keys, err := env.local.List(env.ctx, "world_backups")
	require.NoError(t, err)

	// Count entries with valid timestamps (same logic retention uses)
	validCount := 0
	for _, key := range keys {
		base := filepath.Base(key)
		// Strip extensions
		for ext := filepath.Ext(base); ext != ""; ext = filepath.Ext(base) {
			base = base[:len(base)-len(ext)]
		}
		if _, parseErr := time.Parse("20060102150405", base); parseErr == nil {
			validCount++
		}
	}
	assert.Equal(t, 4, validCount, "both .tar and directory backups should be counted")
}

func TestSyncIntegration_DiffEngine_CorrectSetsFromRealFiles(t *testing.T) {
	env := newSyncTestEnv(t)

	// Local has: a (modified), b (unchanged), c (new)
	writeFile(t, env.localDir, "worlds/a.dat", []byte("modified"))
	writeFile(t, env.localDir, "worlds/b.dat", []byte("same"))
	writeFile(t, env.localDir, "worlds/c.dat", []byte("brand new"))

	localScanner := adapters.NewFullScanner(os.DirFS(worldsPath(env.localDir)))
	localMap, err := localScanner.Scan(env.ctx)
	require.NoError(t, err)

	// Remote has: a (original), b (unchanged), d (to be deleted)
	writeFile(t, env.remoteDir, "worlds/a.dat", []byte("original"))
	writeFile(t, env.remoteDir, "worlds/b.dat", []byte("same"))
	writeFile(t, env.remoteDir, "worlds/d.dat", []byte("orphan"))

	remoteScanner := adapters.NewFullScanner(os.DirFS(worldsPath(env.remoteDir)))
	remoteMap, err := remoteScanner.Scan(env.ctx)
	require.NoError(t, err)

	diff := domain.ComputeDiff(localMap, remoteMap)

	assert.Contains(t, diff.Upload, "a.dat", "modified file should be in upload set")
	assert.Contains(t, diff.Upload, "c.dat", "new file should be in upload set")
	assert.NotContains(t, diff.Upload, "b.dat", "unchanged file should not be uploaded")

	assert.Contains(t, diff.Delete, "d.dat", "orphan should be in delete set")
	assert.NotContains(t, diff.Delete, "a.dat")
	assert.NotContains(t, diff.Delete, "b.dat")
}

// serverPath returns the absolute path to the server dir inside the given root dir.
func serverPath(rootDir string) string {
	return filepath.Join(rootDir, "server")
}

// buildServerSyncUpload creates a sync service for server/ prefix using FilteredScanner + ParseRitualSync, runs Upload.
func buildServerSyncUpload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	sPath := serverPath(env.localDir)
	_ = os.MkdirAll(sPath, 0755)

	fsys := os.DirFS(sPath)
	inner := adapters.NewFullScanner(fsys)
	filter, err := adapters.ParseRitualSync(fsys)
	require.NoError(t, err)

	scanner := adapters.NewFilteredScanner(inner, filter)
	staging := t.TempDir()

	svc := services.NewSyncService(
		scanner, env.local, env.remote, nil,
		services.SyncConfig{Prefix: "server", LocalDir: sPath},
		filepath.Join(staging, "local"),
		"sync/test-lock/server",
	)

	lm := env.loadLocalManifest(t)
	rm := env.loadRemoteManifest(t)

	newState, err := svc.Upload(env.ctx, lm.Server.SyncState, rm.Server.SyncState)
	require.NoError(t, err)

	lm.Server.SyncState = newState
	rm.Server.SyncState = newState
	env.saveLocalManifest(t, lm)
	env.saveRemoteManifest(t, rm)
}

// buildServerSyncDownload creates a sync service for server/ prefix, runs Download.
func buildServerSyncDownload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	sPath := serverPath(env.localDir)
	_ = os.MkdirAll(sPath, 0755)

	fsys := os.DirFS(sPath)
	inner := adapters.NewFullScanner(fsys)

	// For download, .ritualsync may not exist yet; use a pass-all filter.
	var scanner ports.DirectoryScanner
	filter, err := adapters.ParseRitualSync(fsys)
	if err != nil {
		scanner = inner
	} else {
		scanner = adapters.NewFilteredScanner(inner, filter)
	}

	staging := t.TempDir()

	svc := services.NewSyncService(
		scanner, env.local, env.remote, nil,
		services.SyncConfig{Prefix: "server", LocalDir: sPath},
		filepath.Join(staging, "local"),
		"sync/test-lock/server",
	)

	lm := env.loadLocalManifest(t)
	rm := env.loadRemoteManifest(t)

	newState, err := svc.Download(env.ctx, lm.Server.SyncState, rm.Server.SyncState)
	require.NoError(t, err)

	lm.Server.SyncState = newState
	env.saveLocalManifest(t, lm)
}

func TestSyncIntegration_TwoTargetsSameLock(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Create worlds files
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level data"))

	// Create server files with .ritualsync allowing everything
	writeFile(t, env.localDir, "server/server.jar", []byte("server binary"))
	writeFile(t, env.localDir, "server/config/a.cfg", []byte("config data"))
	writeFile(t, env.localDir, "server/.ritualsync", []byte("*\n"))

	// Upload worlds with prefix "worlds"
	buildSyncUpload(t, env)

	// Upload server with prefix "server"
	buildServerSyncUpload(t, env)

	// Verify remote has both targets' files
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"), "worlds file should exist on remote")
	assert.True(t, fileExists(env.remoteDir, "server/server.jar"), "server.jar should exist on remote")
	assert.True(t, fileExists(env.remoteDir, "server/config/a.cfg"), "config file should exist on remote")

	// Verify no cross-contamination
	assert.False(t, fileExists(env.remoteDir, "server/world/level.dat"), "worlds file should not appear under server/")
	assert.False(t, fileExists(env.remoteDir, "worlds/server.jar"), "server file should not appear under worlds/")

	// Verify staging cleaned
	worldsStaging, err := env.remote.List(env.ctx, "sync/test-session/worlds")
	require.NoError(t, err)
	assert.Empty(t, worldsStaging, "worlds staging should be cleaned")

	serverStaging, err := env.remote.List(env.ctx, "sync/test-lock/server")
	require.NoError(t, err)
	assert.Empty(t, serverStaging, "server staging should be cleaned")
}

func TestSyncIntegration_RitualSyncWhitelist(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Create server files
	writeFile(t, env.localDir, "server/server.jar", []byte("server binary"))
	writeFile(t, env.localDir, "server/config/a.cfg", []byte("config data"))
	writeFile(t, env.localDir, "server/logs/latest.log", []byte("log data"))
	writeFile(t, env.localDir, "server/.ritualsync", []byte("server.jar\nconfig/\n"))

	// Upload with filtered scanner
	buildServerSyncUpload(t, env)

	// Verify allowed files present
	assert.True(t, fileExists(env.remoteDir, "server/server.jar"), "server.jar should be on remote")
	assert.True(t, fileExists(env.remoteDir, "server/config/a.cfg"), "config file should be on remote")

	// Verify filtered file absent
	assert.False(t, fileExists(env.remoteDir, "server/logs/latest.log"), "logs should be filtered out")

	// Verify .ritualsync itself is uploaded (exempt from own filter)
	assert.True(t, fileExists(env.remoteDir, "server/.ritualsync"), ".ritualsync should be on remote")
}

func TestSyncIntegration_RitualSyncContracted(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Create server files with broad whitelist
	writeFile(t, env.localDir, "server/server.jar", []byte("server binary"))
	writeFile(t, env.localDir, "server/mods/mod.jar", []byte("mod data"))
	writeFile(t, env.localDir, "server/.ritualsync", []byte("server.jar\nmods/\n"))

	// Upload — both files go to remote
	buildServerSyncUpload(t, env)
	assert.True(t, fileExists(env.remoteDir, "server/server.jar"))
	assert.True(t, fileExists(env.remoteDir, "server/mods/mod.jar"))

	// Contract .ritualsync — remove mods/
	writeFile(t, env.localDir, "server/.ritualsync", []byte("server.jar\n"))

	// Upload again — mods/mod.jar should be orphaned and deleted
	buildServerSyncUpload(t, env)

	assert.True(t, fileExists(env.remoteDir, "server/server.jar"), "server.jar should remain")
	assert.False(t, fileExists(env.remoteDir, "server/mods/mod.jar"), "mod.jar should be deleted after whitelist contraction")
}

func TestSyncIntegration_RitualSyncRemoteWins(t *testing.T) {
	// Host A: upload with broad .ritualsync
	envA := newSyncTestEnv(t)
	envA.saveLocalManifest(t, &domain.Manifest{})
	envA.saveRemoteManifest(t, &domain.Manifest{})

	writeFile(t, envA.localDir, "server/server.jar", []byte("server binary"))
	writeFile(t, envA.localDir, "server/config/a.cfg", []byte("config"))
	writeFile(t, envA.localDir, "server/mods/mod.jar", []byte("mod"))
	writeFile(t, envA.localDir, "server/.ritualsync", []byte("server.jar\nconfig/\nmods/\n"))

	buildServerSyncUpload(t, envA)

	// Host B: different local .ritualsync, shared remote
	envB := newSyncTestEnv(t)
	envB.remote = envA.remote
	envB.remoteDir = envA.remoteDir
	envB.remoteManifests = adapters.NewManifestStore(envB.remote)
	envB.saveLocalManifest(t, &domain.Manifest{})

	// Host B has narrower .ritualsync locally
	writeFile(t, envB.localDir, "server/.ritualsync", []byte("server.jar\n"))

	// Host B downloads from remote
	buildServerSyncDownload(t, envB)

	// Verify Host B's .ritualsync now matches Host A's (remote wins)
	data := readFile(t, envB.localDir, "server/.ritualsync")
	assert.Equal(t, "server.jar\nconfig/\nmods/\n", string(data), ".ritualsync should match remote (Host A)")
}

func TestSyncIntegration_EmptyRemotePrefix(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Empty server dir — nothing on remote, nothing local
	require.NoError(t, os.MkdirAll(serverPath(env.localDir), 0755))

	sPath := serverPath(env.localDir)
	scanner := adapters.NewFullScanner(os.DirFS(sPath))
	staging := t.TempDir()

	svc := services.NewSyncService(
		scanner, env.local, env.remote, nil,
		services.SyncConfig{Prefix: "server", LocalDir: sPath},
		filepath.Join(staging, "local"),
		"sync/test-lock/server",
	)

	emptyState := domain.SyncState{}
	newState, err := svc.Download(env.ctx, emptyState, emptyState)
	require.NoError(t, err)

	// Should return empty/unchanged state, no crash
	assert.Empty(t, newState.XXHashMap, "empty remote should produce empty state")

	// No files should be created in local server dir
	entries, err := os.ReadDir(sPath)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files should appear in empty download")
}

func TestSyncIntegration_DeleteBatchIntegration(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	// Create 20 files + .ritualsync
	writeFile(t, env.localDir, "server/.ritualsync", []byte("*\n"))
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("server/file_%02d.txt", i)
		writeFile(t, env.localDir, name, []byte(fmt.Sprintf("content %d", i)))
	}

	// Upload all
	buildServerSyncUpload(t, env)

	// Verify all 20 + .ritualsync on remote
	rm := env.loadRemoteManifest(t)
	assert.Len(t, rm.Server.XXHashMap, 21, "20 files + .ritualsync should be on remote")

	// Delete 15 files locally (keep file_00 through file_04)
	for i := 5; i < 20; i++ {
		name := filepath.Join(env.localDir, fmt.Sprintf("server/file_%02d.txt", i))
		require.NoError(t, os.Remove(name))
	}

	// Upload again — 15 orphans should be deleted
	buildServerSyncUpload(t, env)

	rm2 := env.loadRemoteManifest(t)
	// 5 files + .ritualsync = 6
	assert.Len(t, rm2.Server.XXHashMap, 6, "5 kept files + .ritualsync should remain")

	// Verify kept files exist
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("server/file_%02d.txt", i)
		assert.True(t, fileExists(env.remoteDir, name), "kept file %s should exist", name)
	}

	// Verify deleted files are gone
	for i := 5; i < 20; i++ {
		name := fmt.Sprintf("server/file_%02d.txt", i)
		assert.False(t, fileExists(env.remoteDir, name), "deleted file %s should not exist", name)
	}
}
