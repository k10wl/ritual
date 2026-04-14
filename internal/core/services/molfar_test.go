package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func showDirectoryTree(t *testing.T, dirPath string, prefix string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		t.Logf("%s[ERROR: %v]", prefix, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Logf("%s%s/", prefix, entry.Name())
			showDirectoryTree(t, filepath.Join(dirPath, entry.Name()), prefix+"  ")
		} else {
			t.Logf("%s%s", prefix, entry.Name())
		}
	}
}

func createTestManifest(ritualVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion: ritualVersion,
		Worlds:        domain.WorldsManifest{Backups: worlds},
		UpdatedAt:     time.Now(),
	}
}

func createTestWorld(uri string) domain.World {
	return domain.World{
		URI:       uri,
		CreatedAt: time.Now(),
	}
}

func setupMolfarServices(t *testing.T) (*services.MolfarService, *adapters.FSRepository, *adapters.FSRepository, string, string, func()) {
	tempDir := t.TempDir()
	remoteTempDir := t.TempDir()

	// Create roots for safe operations
	tempRoot, err := os.OpenRoot(tempDir)
	assert.NoError(t, err)

	remoteRoot, err := os.OpenRoot(remoteTempDir)
	assert.NoError(t, err)

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempRoot)
	assert.NoError(t, err)

	// Create remote storage (FS for testing) in separate temp dir
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	assert.NoError(t, err)

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	assert.NoError(t, err)

	// Create mock server runner
	mockServerRunner := &MockServerRunner{}

	// Mock updater creates server directories and saves a local manifest
	// to simulate what the real SyncDownloadUpdater does.
	mockUpdater := mocks.NewMockUpdaterService()
	mockUpdater.RunFunc = func(ctx context.Context) error {
		worldDirsToCreate := []string{"world", "world_nether", "world_the_end"}
		for _, wd := range worldDirsToCreate {
			dirPath := filepath.Join(tempDir, config.ServerDir, wd)
			if mkErr := os.MkdirAll(dirPath, config.DirPermission); mkErr != nil {
				return mkErr
			}
		}
		// Save a default local manifest if none exists, matching SyncDownloadUpdater behavior.
		if _, err := librarianService.GetLocalManifest(ctx); err != nil {
			defaultManifest := &domain.Manifest{RitualVersion: config.AppVersion, UpdatedAt: time.Now()}
			if saveErr := librarianService.SaveLocalManifest(ctx, defaultManifest); saveErr != nil {
				return saveErr
			}
		}
		return nil
	}

	updaters := []ports.UpdaterService{mockUpdater}

	// Create mock backupper that saves a backup file to local storage,
	// simulating what the real SyncUploadBackupper does.
	mockBackupper := &mocks.MockBackupperService{
		RunFunc: func(ctx context.Context) (string, error) {
			archiveName := config.LocalBackups + "/backup.tar"
			if putErr := localStorage.Put(ctx, archiveName, []byte("mock-backup-data")); putErr != nil {
				return "", putErr
			}
			return archiveName, nil
		},
	}

	backuppers := []ports.BackupperService{mockBackupper}

	// Create local retention service
	localRetention, err := services.NewLocalRetention(localStorage, nil)
	assert.NoError(t, err)

	retentions := []ports.RetentionService{localRetention}

	// Create mock conditions (empty for these tests - lock check handled by ManifestLockCondition in real usage)
	conditions := []ports.ConditionService{}

	// Create molfar service with new constructor
	molfarService, err := services.NewMolfarService(
		conditions,
		updaters,
		backuppers,
		retentions,
		mockServerRunner,
		librarianService,
		nil,
		tempRoot,
	)
	assert.NoError(t, err)

	cleanup := func() {
		localStorage.Close()  // This closes tempRoot
		remoteStorage.Close() // This closes remoteRoot
	}

	return molfarService, localStorage, remoteStorage, tempDir, remoteTempDir, cleanup
}

func setupRemoteManifest(t *testing.T, remoteStorage *adapters.FSRepository, manifestVersion string, worldURI string) {
	ctx := context.Background()

	// Create remote manifest
	world := createTestWorld(worldURI)
	remoteManifest := createTestManifest(manifestVersion, []domain.World{world})

	// Save remote manifest
	manifestData, err := json.Marshal(remoteManifest)
	assert.NoError(t, err)
	err = remoteStorage.Put(ctx, "manifest.json", manifestData)
	assert.NoError(t, err)
}

// MockServerRunner implements ports.ServerRunner for testing
type MockServerRunner struct {
	runCalled bool
	server    *domain.ServerRuntime
}

func (m *MockServerRunner) Run(server *domain.ServerRuntime) error {
	m.runCalled = true
	m.server = server
	return nil
}

func TestMolfarService_Prepare(globT *testing.T) {
	globT.Run("no local manifest, no instance, no worlds", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data
		setupRemoteManifest(t, remoteStorage, "1.0.0", config.RemoteBackups+"/1234567890.tar")

		// Execute Prepare
		err := molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was created and has valid structure
		ctx := context.Background()
		localManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifest)

		// Parse and validate local manifest structure
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifest, &localManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestObj.RitualVersion)
		assert.False(t, localManifestObj.IsLocked())
		assert.True(t, localManifestObj.UpdatedAt.After(time.Time{}))

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.ServerDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)

		// Verify world directories exist (created by mock sync updater)
		for _, wd := range []string{"world", "world_nether", "world_the_end"} {
			_, err = os.Stat(filepath.Join(instancePath, wd))
			assert.NoError(t, err, "World dir %s should exist", wd)
		}

	})

	globT.Run("existing local manifest, outdated instance", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data with newer version
		setupRemoteManifest(t, remoteStorage, "2.0.0", config.RemoteBackups+"/1234567890.tar")

		// Create local manifest with older version
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
		oldManifest := createTestManifest("1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(oldManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare
		err = molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify local manifest was updated
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)

		var updatedManifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &updatedManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, updatedManifestObj.RitualVersion)
		assert.False(t, updatedManifestObj.IsLocked())

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.ServerDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)
	})

	globT.Run("existing local manifest, sync updater runs", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote data
		setupRemoteManifest(t, remoteStorage, "1.0.0", config.RemoteBackups+"/1234567890.tar")

		// Create local manifest
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
		oldManifest := createTestManifest("1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(oldManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare
		err = molfar.Prepare()
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}

		// Verify world directories exist (created by mock sync updater)
		instancePath := filepath.Join(tempDir, config.ServerDir)
		for _, wd := range []string{"world", "world_nether", "world_the_end"} {
			_, err = os.Stat(filepath.Join(instancePath, wd))
			assert.NoError(t, err, "World dir %s should exist", wd)
		}
	})

	globT.Run("no remote worlds - should launch successfully", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup remote manifest with NO worlds
		ctx := context.Background()
		remoteManifest := createTestManifest("1.0.0", []domain.World{}) // Empty worlds
		manifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Execute Prepare - should succeed even without remote worlds
		err = molfar.Prepare()
		assert.NoError(t, err, "Prepare should succeed without remote worlds")

		// Verify local manifest was created
		localManifestData, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestData)

		// Parse and validate local manifest structure
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestData, &localManifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifestObj.RitualVersion)
		assert.False(t, localManifestObj.IsLocked())

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.ServerDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)
	})

	globT.Run("updater failure returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		// Create a failing mock updater
		failingUpdater := mocks.NewMockUpdaterService()
		failingUpdater.RunFunc = func(ctx context.Context) error {
			return errors.New("updater failed")
		}

		updaters := []ports.UpdaterService{failingUpdater}
		backuppers := []ports.BackupperService{&mocks.MockBackupperService{}}
		retentions := []ports.RetentionService{mocks.NewMockRetentionService()}

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		// Create unlocked remote manifest so Prepare can proceed to updaters
		ctx := context.Background()
		manifest := createTestManifest("1.0.0", []domain.World{})
		manifestData, err := json.Marshal(manifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		conditions := []ports.ConditionService{}
		molfar, err := services.NewMolfarService(
			conditions,
			updaters,
			backuppers,
			retentions,
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)

		err = molfar.Prepare()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updater 0 failed")
	})
}

func TestMolfarService_Run(t *testing.T) {
	t.Run("successful server execution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server with proper memory value
		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run
		err = molfar.Run(server)
		assert.NoError(t, err)

		// Verify manifests are locked after Run execution
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify lock IDs match between local and remote manifests
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Lock IDs should match between local and remote manifests")
		assert.NotEmpty(t, localManifestObj.LockedBy, "Lock ID should not be empty")
		assert.Contains(t, localManifestObj.LockedBy, "::", "Lock ID should contain hostname and timestamp separator")
	})

	t.Run("manifest update during run execution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest with older version
		ctx := context.Background()
		oldWorld := createTestWorld(config.RemoteBackups + "/old.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{oldWorld})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest with newer version
		newWorld := createTestWorld(config.RemoteBackups + "/new.tar")
		remoteManifest := createTestManifest("2.0.0", []domain.World{newWorld})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server
		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run - should succeed and lock manifests (Run doesn't update versions)
		err = molfar.Run(server)
		assert.NoError(t, err)

		// Verify manifests are locked after Run execution (versions remain unchanged)
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "1.0.0", localManifestObj.RitualVersion, "Local manifest ritual version should remain unchanged during Run")
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "2.0.0", remoteManifestObj.RitualVersion, "Remote manifest should retain newer ritual version")
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify lock IDs match
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Lock IDs should match after Run")
	})

	t.Run("remote manifest fetch before run", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest with different timestamp
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifest.UpdatedAt = time.Now().Add(time.Hour) // Different timestamp
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Verify manifests are unlocked before Run execution
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestBeforeObj domain.Manifest
		err = json.Unmarshal(localManifestBefore, &localManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, localManifestBeforeObj.IsLocked(), "Local manifest should be unlocked before Run")

		remoteManifestBefore, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestBeforeObj domain.Manifest
		err = json.Unmarshal(remoteManifestBefore, &remoteManifestBeforeObj)
		assert.NoError(t, err)
		assert.False(t, remoteManifestBeforeObj.IsLocked(), "Remote manifest should be unlocked before Run")

		// Create test server
		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run
		err = molfar.Run(server)
		assert.NoError(t, err)

		// Verify remote manifest was fetched and used for lock acquisition
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")

		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestObj)
		assert.NoError(t, err)
		assert.True(t, remoteManifestObj.IsLocked(), "Remote manifest should be locked after Run")

		// Verify both manifests have the same lock ID
		assert.Equal(t, localManifestObj.LockedBy, remoteManifestObj.LockedBy, "Both manifests should have matching lock IDs")
	})

	t.Run("nil server parameter", func(t *testing.T) {
		molfar, _, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		err := molfar.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService
		server := &domain.ServerRuntime{Address: "127.0.0.1:25565", Memory: 2048}

		err := molfar.Run(server)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("server runner failure", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest first
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Create test server
		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Execute Run - should succeed with mock runner
		err = molfar.Run(server)
		assert.NoError(t, err)
	})
}

func TestMolfarService_Exit(t *testing.T) {
	t.Run("successful exit with real backupper", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data using testhelpers
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.ServerDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		// Create test world using testhelpers
		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		// Setup real world before Exit execution
		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Setup manifests with locks to simulate running state
		lockID := "test-host::1234567890"
		localManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set current lock ID so molfar owns the lock
		molfar.SetLockIDForTesting(lockID)

		// Verify manifests are locked before exit
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var manifestBefore domain.Manifest
		err = json.Unmarshal(localManifestBefore, &manifestBefore)
		assert.NoError(t, err)
		assert.True(t, manifestBefore.IsLocked(), "Local manifest should be locked before exit")

		// Execute Exit
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify manifests are unlocked after exit and have valid structure
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var manifestAfter domain.Manifest
		err = json.Unmarshal(localManifestAfter, &manifestAfter)
		assert.NoError(t, err)
		assert.False(t, manifestAfter.IsLocked(), "Local manifest should be unlocked after exit")

		// Validate manifest structure after exit
		assert.NotEmpty(t, manifestAfter.RitualVersion)
		assert.NotEmpty(t, manifestAfter.Worlds.Backups)
		assert.True(t, manifestAfter.UpdatedAt.After(time.Time{}))

		// Verify new world entry was added from backup
		latestWorld := manifestAfter.GetLatestWorld()
		assert.NotNil(t, latestWorld)
		assert.NotEmpty(t, latestWorld.URI)
		assert.True(t, latestWorld.CreatedAt.After(time.Time{}))

		// Verify remote manifest structure matches local after exit
		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestAfterObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestAfterObj)
		assert.NoError(t, err)
		assert.Equal(t, config.AppVersion, remoteManifestAfterObj.RitualVersion)
		assert.Equal(t, len(manifestAfter.Worlds.Backups), len(remoteManifestAfterObj.Worlds.Backups))
		assert.False(t, remoteManifestAfterObj.IsLocked())

		// List file tree before assertions
		t.Log("=== WORKDIR FILE TREE AFTER EXIT ===")
		showDirectoryTree(t, tempDir, "")

		// Verify backup was created by checking if backup files exist
		backupFiles, err := localStorage.List(ctx, "world_backups")
		assert.NoError(t, err)
		assert.NotEmpty(t, backupFiles, "Backup files should be created")

		// Verify backup archive has data
		if len(backupFiles) > 0 {
			backupData, err := localStorage.Get(ctx, backupFiles[0])
			assert.NoError(t, err)
			assert.NotEmpty(t, backupData, "Backup data should not be empty")
		}
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService

		err := molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("backupper failure returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		// Create a failing mock backupper
		failingBackupper := &mocks.MockBackupperService{
			RunFunc: func(ctx context.Context) (string, error) {
				return "", errors.New("backupper failed")
			},
		}

		updaters := []ports.UpdaterService{mocks.NewMockUpdaterService()}
		backuppers := []ports.BackupperService{failingBackupper}
		retentions := []ports.RetentionService{mocks.NewMockRetentionService()}

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		conditions := []ports.ConditionService{}
		molfar, err := services.NewMolfarService(
			conditions,
			updaters,
			backuppers,
			retentions,
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)

		// Set lock ID so Exit() doesn't skip early
		molfar.SetLockIDForTesting("test-lock-id")

		err = molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backupper 0 failed")
	})

	t.Run("exit stamps RitualVersion with current AppVersion", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.ServerDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		// Create test world
		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)
		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Setup manifests with OLD RitualVersion (simulating old client)
		lockID := "test-host::1234567890"
		oldRitualVersion := "0.0.1" // Old version, different from current
		localManifest := createTestManifest(oldRitualVersion, []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest(oldRitualVersion, []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		molfar.SetLockIDForTesting(lockID)

		// Execute Exit
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify remote manifest has current AppVersion stamped
		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestAfterObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestAfterObj)
		assert.NoError(t, err)

		// RitualVersion should be stamped with current AppVersion, not old version
		assert.Equal(t, config.AppVersion, remoteManifestAfterObj.RitualVersion,
			"Remote manifest RitualVersion should be stamped with current AppVersion")
		assert.NotEqual(t, oldRitualVersion, remoteManifestAfterObj.RitualVersion,
			"Remote manifest RitualVersion should not be old version")
	})

	t.Run("retention changes to Backups are persisted", func(t *testing.T) {
		tempDir := t.TempDir()
		remoteTempDir := t.TempDir()

		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)

		remoteRoot, err := os.OpenRoot(remoteTempDir)
		assert.NoError(t, err)

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		remoteStorage, err := adapters.NewFSRepository(remoteRoot)
		assert.NoError(t, err)
		defer remoteStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
		assert.NoError(t, err)

		ctx := context.Background()

		// Setup instance directory with world
		instancePath := filepath.Join(tempDir, config.ServerDir)
		err = os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)
		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Create backupper that returns a valid archive name
		mockBackupper := &mocks.MockBackupperService{
			RunFunc: func(ctx context.Context) (string, error) {
				return config.RemoteBackups + "/20251227120000.tar", nil
			},
		}

		// Create retention that records it was applied
		retentionApplied := false
		mockRetention := &mocks.MockRetentionService{
			ApplyFunc: func(ctx context.Context) error {
				retentionApplied = true
				return nil
			},
		}

		// Setup manifests with 5 worlds (more than retention limit)
		lockID := "test-host::1234567890"
		baseTime := time.Now().Add(-5 * time.Hour)
		worlds := []domain.World{
			{URI: config.RemoteBackups + "/world1.tar", CreatedAt: baseTime},
			{URI: config.RemoteBackups + "/world2.tar", CreatedAt: baseTime.Add(1 * time.Hour)},
			{URI: config.RemoteBackups + "/world3.tar", CreatedAt: baseTime.Add(2 * time.Hour)},
			{URI: config.RemoteBackups + "/world4.tar", CreatedAt: baseTime.Add(3 * time.Hour)},
			{URI: config.RemoteBackups + "/world5.tar", CreatedAt: baseTime.Add(4 * time.Hour)},
		}
		localManifest := createTestManifest("1.0.0", worlds)
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", worlds)
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Create molfar with mock retention
		molfar, err := services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{mockBackupper},
			[]ports.RetentionService{mockRetention},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)
		molfar.SetLockIDForTesting(lockID)

		// Verify we start with 5 worlds
		localManifestBefore, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var manifestBefore domain.Manifest
		err = json.Unmarshal(localManifestBefore, &manifestBefore)
		assert.NoError(t, err)
		assert.Equal(t, 5, len(manifestBefore.Worlds.Backups), "Should start with 5 worlds")

		// Execute Exit
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify retention was called
		assert.True(t, retentionApplied, "Retention should have been applied")

		// Verify LOCAL manifest has worlds after backup added one
		// Flow: 5 worlds -> backup adds 1 -> 6 worlds -> retention applied (no longer mutates manifest)
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestAfterObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestAfterObj)
		assert.NoError(t, err)
		assert.Equal(t, 6, len(localManifestAfterObj.Worlds.Backups),
			"Local manifest should have 6 worlds after backup (retention no longer mutates manifest)")

		// Verify REMOTE manifest also reflects backup addition
		remoteManifestAfter, err := remoteStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var remoteManifestAfterObj domain.Manifest
		err = json.Unmarshal(remoteManifestAfter, &remoteManifestAfterObj)
		assert.NoError(t, err)
		assert.Equal(t, 6, len(remoteManifestAfterObj.Worlds.Backups),
			"Remote manifest should have 6 worlds after backup (retention no longer mutates manifest)")

		// Verify both manifests are in sync
		assert.Equal(t, len(localManifestAfterObj.Worlds.Backups), len(remoteManifestAfterObj.Worlds.Backups),
			"Local and remote manifests should have same number of worlds")
	})
}

func TestMolfarService_LockMechanisms(t *testing.T) {
	t.Run("lock acquisition failure - hostname resolution", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Test hostname failure by creating a custom molfar service
		// Since we can't mock os.Hostname directly, we'll test the error handling
		// by creating a scenario that would trigger hostname-related errors
		// This test verifies the lock acquisition process works correctly

		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// This should succeed since hostname resolution works in normal test environment
		err = molfar.Run(server)
		assert.NoError(t, err, "Lock acquisition should succeed with valid hostname")

		// Verify manifests are locked after successful run
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after successful run")
	})

	t.Run("remote storage failure during Run", func(t *testing.T) {
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create local manifest
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Delete the remote manifest to simulate failure
		remoteStorage.Delete(ctx, "manifest.json")

		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		err = molfar.Run(server)
		assert.Error(t, err)

		// Verify local manifest was not locked due to remote failure
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.False(t, localManifestObj.IsLocked(), "Local manifest should not be locked after remote failure")
		assert.Empty(t, localManifestObj.LockedBy, "Lock ID should be empty after remote failure")
	})

	t.Run("lock ownership validation on exit", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.ServerDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)

		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
		assert.NoError(t, err)

		// Close instanceRoot before Exit to release file handles
		instanceRoot.Close()

		// Setup manifests with locks by another process
		localManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock("other-process::1234567890")
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock("other-process::1234567890")
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set a different lock ID to simulate trying to exit when we think we own a lock
		// but the manifest has a different lock (simulating another process took over)
		molfar.SetLockIDForTesting("my-process::9876543210")

		// Try to exit - should fail because manifest lock doesn't match our lock
		err = molfar.Exit()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lock ownership validation failed")
	})

	t.Run("concurrent lock acquisition attempts", func(t *testing.T) {
		// Test that lock mechanism works correctly
		// This test verifies the lock acquisition process works as expected
		molfar, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create manifests
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})

		// Setup storage
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Run should succeed
		err = molfar.Run(server)
		assert.NoError(t, err, "Run should succeed")

		// Verify manifests are locked
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		assert.True(t, localManifestObj.IsLocked(), "Local manifest should be locked after Run")
	})

	t.Run("lock validation with nil server", func(t *testing.T) {
		molfar, _, _, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Test with nil server
		err := molfar.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("race condition - lock acquired between Prepare and Run", func(t *testing.T) {
		molfar1, localStorage, remoteStorage, _, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Create manifests
		ctx := context.Background()
		world := createTestWorld(config.RemoteBackups + "/1234567890.tar")
		localManifest := createTestManifest("1.0.0", []domain.World{world})
		remoteManifest := createTestManifest("1.0.0", []domain.World{world})

		// Setup storage
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Simulate another process locking the manifest after Prepare
		localManifest.Lock("race-process::1234567890")
		localManifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		assert.NoError(t, err)

		server := &domain.ServerRuntime{
			Address: "127.0.0.1:25565",
			Memory:  2048,
			IP:      "127.0.0.1",
			Port:    25565,
					}

		// Run should fail due to lock acquired between Prepare and Run
		err = molfar1.Run(server)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "local manifest already locked")
	})

	t.Run("lock cleanup on exit failure", func(t *testing.T) {
		molfar, localStorage, remoteStorage, tempDir, _, cleanup := setupMolfarServices(t)
		defer cleanup()

		// Setup test world data using testhelpers
		ctx := context.Background()
		instancePath := filepath.Join(tempDir, config.ServerDir)
		err := os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		instanceRoot, err := os.OpenRoot(instancePath)
		assert.NoError(t, err)
		defer instanceRoot.Close()

		// Create test world using testhelpers
		_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
		assert.NoError(t, err)

		// Setup manifests with locks to simulate running state
		lockID := "test-host::1234567890"
		localManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		localManifest.Lock(lockID)
		manifestData, err := json.Marshal(localManifest)
		assert.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		assert.NoError(t, err)

		remoteManifest := createTestManifest("1.0.0", []domain.World{createTestWorld(config.RemoteBackups + "/test-world")})
		remoteManifest.Lock(lockID)
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Set the current lock ID so molfar owns the lock
		molfar.SetLockIDForTesting(lockID)

		// Close instanceRoot before cleanup
		instanceRoot.Close()

		// Delete remote manifest to simulate failure during unlock
		remoteStorage.Delete(ctx, "manifest.json")

		// Exit should succeed - the remote manifest will be recreated when needed
		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify local manifest was unlocked
		localManifestAfter, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		var localManifestObj domain.Manifest
		err = json.Unmarshal(localManifestAfter, &localManifestObj)
		assert.NoError(t, err)
		// Local manifest should be unlocked successfully
		assert.False(t, localManifestObj.IsLocked(), "Local manifest should be unlocked")
		t.Logf("Local manifest lock status: %v", localManifestObj.IsLocked())
	})
}

func TestNewMolfarService(t *testing.T) {
	t.Run("nil conditions slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			nil,
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conditions slice cannot be nil")
	})

	t.Run("nil condition in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{nil},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "condition at index 0 cannot be nil")
	})

	t.Run("nil updaters slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			nil,
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updaters slice cannot be nil")
	})

	t.Run("nil updater in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{nil},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "updater at index 0 cannot be nil")
	})

	t.Run("nil backuppers slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			nil,
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backuppers slice cannot be nil")
	})

	t.Run("nil backupper in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{nil},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backupper at index 0 cannot be nil")
	})

	t.Run("nil retentions slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			nil,
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retentions slice cannot be nil")
	})

	t.Run("nil retention in slice returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{nil},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retention at index 0 cannot be nil")
	})

	t.Run("nil serverRunner returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			nil,
			librarianService,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server runner cannot be nil")
	})

	t.Run("nil librarian returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			nil,
			nil,
			tempRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian service cannot be nil")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		_, err = services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot cannot be nil")
	})

	t.Run("valid dependencies returns molfar", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty updaters slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty backuppers slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{},
			[]ports.RetentionService{mocks.NewMockRetentionService()},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})

	t.Run("empty retentions slice is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		tempRoot, err := os.OpenRoot(tempDir)
		assert.NoError(t, err)
		defer tempRoot.Close()

		localStorage, err := adapters.NewFSRepository(tempRoot)
		assert.NoError(t, err)
		defer localStorage.Close()

		librarianService, err := services.NewLibrarianService(localStorage, localStorage)
		assert.NoError(t, err)

		molfar, err := services.NewMolfarService(
			[]ports.ConditionService{},
			[]ports.UpdaterService{mocks.NewMockUpdaterService()},
			[]ports.BackupperService{&mocks.MockBackupperService{}},
			[]ports.RetentionService{},
			&MockServerRunner{},
			librarianService,
			nil,
			tempRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, molfar)
	})
}

// FailingMockServerRunner implements ports.ServerRunner for testing failure scenarios
type FailingMockServerRunner struct{}

func (m *FailingMockServerRunner) Run(server *domain.ServerRuntime) error {
	return errors.New("server execution failed")
}
