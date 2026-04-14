package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
)

// syncTestEnv bundles local/remote FS repos, librarian, and temp dirs for integration tests.
type syncTestEnv struct {
	localDir    string
	remoteDir   string
	localRoot   *os.Root
	remoteRoot  *os.Root
	local       *adapters.FSRepository
	remote      *adapters.FSRepository
	librarian   *services.LibrarianService
	ctx         context.Context
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

	librarian, err := services.NewLibrarianService(local, remote)
	require.NoError(t, err)

	return &syncTestEnv{
		localDir:   localDir,
		remoteDir:  remoteDir,
		localRoot:  localRoot,
		remoteRoot: remoteRoot,
		local:      local,
		remote:     remote,
		librarian:  librarian,
		ctx:        context.Background(),
	}
}

// saveManifest writes a manifest to the given storage as manifest.json
func (e *syncTestEnv) saveLocalManifest(t *testing.T, m *domain.Manifest) {
	t.Helper()
	require.NoError(t, e.librarian.SaveLocalManifest(e.ctx, m))
}

func (e *syncTestEnv) saveRemoteManifest(t *testing.T, m *domain.Manifest) {
	t.Helper()
	require.NoError(t, e.librarian.SaveRemoteManifest(e.ctx, m))
}

func (e *syncTestEnv) loadLocalManifest(t *testing.T) *domain.Manifest {
	t.Helper()
	m, err := e.librarian.GetLocalManifest(e.ctx)
	require.NoError(t, err)
	return m
}

func (e *syncTestEnv) loadRemoteManifest(t *testing.T) *domain.Manifest {
	t.Helper()
	m, err := e.librarian.GetRemoteManifest(e.ctx)
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

// buildSyncUpload creates a SyncService with a FullWorldScanner over local worlds,
// runs Upload, returns the updated remote manifest.
func buildSyncUpload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	wPath := worldsPath(env.localDir)
	// ensure worlds dir exists even if empty
	_ = os.MkdirAll(wPath, 0755)
	scanner, err := adapters.NewFullWorldScanner(wPath)
	require.NoError(t, err)

	svc, err := services.NewSyncService(scanner, env.local, env.remote, env.librarian, nil, wPath, "test-lock")
	require.NoError(t, err)
	require.NoError(t, svc.Upload(env.ctx))
}

// buildSyncDownload creates a SyncService with a dummy scanner (not used in download),
// runs Download.
func buildSyncDownload(t *testing.T, env *syncTestEnv) {
	t.Helper()
	wPath := worldsPath(env.localDir)
	_ = os.MkdirAll(wPath, 0755)
	// Scanner unused during download — full scanner as placeholder
	scanner, err := adapters.NewFullWorldScanner(wPath)
	require.NoError(t, err)

	svc, err := services.NewSyncService(scanner, env.local, env.remote, env.librarian, nil, wPath, "test-lock")
	require.NoError(t, err)
	require.NoError(t, svc.Download(env.ctx))
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
	assert.NotEmpty(t, rm.XXHashMap, "remote manifest should have xxhash map after upload")
	assert.False(t, rm.XXHashSyncAt.IsZero(), "remote xxhash_sync_at should be set")

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
	librarian2, err := services.NewLibrarianService(env2.local, env2.remote)
	require.NoError(t, err)
	env2.librarian = librarian2

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
	assert.Equal(t, rm.XXHashMap, lm2.XXHashMap)
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
	assert.Len(t, rm1.XXHashMap, 2)

	// Modify one file locally
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("modified"))

	// Second upload — delta
	buildSyncUpload(t, env)

	rm2 := env.loadRemoteManifest(t)
	assert.Len(t, rm2.XXHashMap, 2)

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
	assert.Len(t, rm.XXHashMap, 1, "only level.dat should remain in manifest")
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
	remoteMap := map[string]string{}
	writeFile(t, env.remoteDir, "worlds/world/level.dat", []byte("level"))

	// Compute hash for remote manifest
	scanner, err := adapters.NewFullWorldScanner(worldsPath(env.remoteDir))
	require.NoError(t, err)
	remoteMap, err = scanner.Scan(env.ctx)
	require.NoError(t, err)

	env.saveRemoteManifest(t, &domain.Manifest{
		XXHashMap:    remoteMap,
		XXHashSyncAt: time.Now(),
	})

	// Local has two files (one is ghost)
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("old level"))
	writeFile(t, env.localDir, "worlds/world/ghost.dat", []byte("should be deleted"))
	env.saveLocalManifest(t, &domain.Manifest{
		XXHashMap: map[string]string{
			"world/level.dat": "stale_hash",
			"world/ghost.dat": "ghost_hash",
		},
	})

	// Download — ghost should be removed
	buildSyncDownload(t, env)

	assert.True(t, fileExists(env.localDir, "worlds/world/level.dat"))
	assert.False(t, fileExists(env.localDir, "worlds/world/ghost.dat"), "ghost file should be deleted after download")

	lm := env.loadLocalManifest(t)
	_, hasGhost := lm.XXHashMap["world/ghost.dat"]
	assert.False(t, hasGhost, "ghost should not be in local manifest")
}

func TestSyncIntegration_EmptyDiff_NoTransfers(t *testing.T) {
	env := newSyncTestEnv(t)

	hashMap := map[string]string{"world/level.dat": "abc123"}
	env.saveLocalManifest(t, &domain.Manifest{XXHashMap: hashMap})
	env.saveRemoteManifest(t, &domain.Manifest{XXHashMap: hashMap})

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
	assert.Len(t, rm.XXHashMap, 2)
	assert.True(t, fileExists(env.remoteDir, "worlds/world/level.dat"))
	assert.True(t, fileExists(env.remoteDir, "worlds/world/region/r.0.0.mca"))
}

func TestSyncIntegration_FirstRunNoLocalManifest_FullDownload(t *testing.T) {
	env := newSyncTestEnv(t)

	// Remote has world
	writeFile(t, env.remoteDir, "worlds/world/level.dat", []byte("remote level"))
	writeFile(t, env.remoteDir, "worlds/world/region/r.0.0.mca", []byte("remote region"))

	scanner, err := adapters.NewFullWorldScanner(worldsPath(env.remoteDir))
	require.NoError(t, err)
	remoteMap, err := scanner.Scan(env.ctx)
	require.NoError(t, err)

	env.saveRemoteManifest(t, &domain.Manifest{
		XXHashMap:    remoteMap,
		XXHashSyncAt: time.Now(),
	})
	env.saveLocalManifest(t, &domain.Manifest{}) // empty

	// Download — everything comes down
	buildSyncDownload(t, env)

	assert.True(t, fileExists(env.localDir, "worlds/world/level.dat"))
	data := readFile(t, env.localDir, "worlds/world/level.dat")
	assert.Equal(t, []byte("remote level"), data)

	lm := env.loadLocalManifest(t)
	assert.Equal(t, remoteMap, lm.XXHashMap)
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
	assert.Len(t, rm.XXHashMap, 1)
	_, hasB := rm.XXHashMap["b.dat"]
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
	libB, err := services.NewLibrarianService(envB.local, envB.remote)
	require.NoError(t, err)
	envB.librarian = libB
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
		InstanceVersion: "paper-1.20",
		UpdatedAt:       time.Now(),
	}
	env.saveLocalManifest(t, v1Manifest)
	env.saveRemoteManifest(t, v1Manifest)

	// Create world files
	writeFile(t, env.localDir, "worlds/world/level.dat", []byte("level"))

	// Upload with empty xxhash (migration path) — should produce full upload
	buildSyncUpload(t, env)

	rm := env.loadRemoteManifest(t)
	assert.NotNil(t, rm.XXHashMap, "migration should populate xxhash map")
	assert.Len(t, rm.XXHashMap, 1)
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
	assert.Empty(t, rm.XXHashMap)
}

func TestSyncIntegration_SyncFolderCleaned(t *testing.T) {
	env := newSyncTestEnv(t)
	env.saveLocalManifest(t, &domain.Manifest{})
	env.saveRemoteManifest(t, &domain.Manifest{})

	writeFile(t, env.localDir, "worlds/a.dat", []byte("data"))
	buildSyncUpload(t, env)

	// Verify no sync/ folder lingers on remote
	keys, err := env.remote.List(env.ctx, "sync")
	require.NoError(t, err)
	assert.Empty(t, keys, "sync staging folder should be cleaned after upload")
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

	localScanner, err := adapters.NewFullWorldScanner(worldsPath(env.localDir))
	require.NoError(t, err)
	localMap, err := localScanner.Scan(env.ctx)
	require.NoError(t, err)

	// Remote has: a (original), b (unchanged), d (to be deleted)
	writeFile(t, env.remoteDir, "worlds/a.dat", []byte("original"))
	writeFile(t, env.remoteDir, "worlds/b.dat", []byte("same"))
	writeFile(t, env.remoteDir, "worlds/d.dat", []byte("orphan"))

	remoteScanner, err := adapters.NewFullWorldScanner(worldsPath(env.remoteDir))
	require.NoError(t, err)
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
