package services_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func createTestZipData() []byte {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	file, err := zipWriter.Create("test.txt")
	assert.NoError(nil, err)
	_, err = file.Write([]byte("test content"))
	assert.NoError(nil, err)

	err = zipWriter.Close()
	assert.NoError(nil, err)
	return buf.Bytes()
}

func setupIntegrationTest(t *testing.T) (*services.MolfarService, ports.LibrarianService, ports.StorageRepository, ports.StorageRepository, string, func()) {
	local := t.TempDir()
	remote := t.TempDir()

	localStorage, err := adapters.NewFSRepository(local)
	assert.NoError(t, err)

	remoteStorage, err := adapters.NewFSRepository(remote)
	assert.NoError(t, err)

	librarian, err := services.NewLibrarianService(localStorage, remoteStorage)
	assert.NoError(t, err)

	validator, err := services.NewValidatorService()
	assert.NoError(t, err)

	archive, err := services.NewArchiveService("/test/base")
	assert.NoError(t, err)

	mockServerRunner := &mocks.MockServerRunner{}
	mockServerRunner.On("Run", mock.AnythingOfType("*domain.Server")).Return(nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	molfar, err := services.NewMolfarService(
		librarian,
		validator,
		archive,
		localStorage,
		remoteStorage,
		mockServerRunner,
		logger,
		local,
	)
	assert.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return molfar, librarian, localStorage, remoteStorage, local, cleanup
}

func createTestManifest(instanceVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   "1.0.0",
		InstanceVersion: instanceVersion,
		StoredWorlds:    worlds,
		UpdatedAt:       time.Now(),
	}
}

func createTestWorld(uri string) domain.World {
	return domain.World{
		URI:       uri,
		CreatedAt: time.Now(),
	}
}

func TestMolfarService_Prepare(t *testing.T) {
	t.Run("no local manifest, no instance, no worlds", func(t *testing.T) {
		molfar, librarian, _, remoteStorage, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		// Setup remote manifest with instance and world data
		ctx := context.Background()
		testWorld := createTestWorld("worlds/test-world.zip")
		remoteManifest := createTestManifest("v1.0.0", []domain.World{testWorld})

		// Save remote manifest
		remoteManifestData, err := json.Marshal(remoteManifest)
		assert.NoError(t, err)

		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		assert.NoError(t, err)

		// Create test instance ZIP data - create a minimal valid ZIP
		instanceZipData := createTestZipData()
		err = remoteStorage.Put(ctx, "instance.zip", instanceZipData)
		assert.NoError(t, err)

		// Create test world ZIP data
		worldZipData := createTestZipData()
		err = remoteStorage.Put(ctx, "worlds/test-world.zip", worldZipData)
		assert.NoError(t, err)

		err = molfar.Prepare()
		assert.NoError(t, err)

		// Verify local manifest was created
		localManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.Equal(t, remoteManifest.InstanceVersion, localManifest.InstanceVersion)
		assert.Equal(t, remoteManifest.RitualVersion, localManifest.RitualVersion)
		assert.Empty(t, localManifest.LockedBy)

		// Verify instance directory was created
		instancePath := filepath.Join(workdir, "instance")
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)
	})

	t.Run("existing local manifest, outdated instance", func(t *testing.T) {
		molfar, librarian, _, remoteStorage, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		testWorld := createTestWorld("worlds/test-world.zip")

		// Create outdated local manifest
		localManifest := createTestManifest("v0.9.0", []domain.World{testWorld})
		err := librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		// Create updated remote manifest
		remoteManifest := createTestManifest("v1.0.0", []domain.World{testWorld})
		err = librarian.SaveRemoteManifest(ctx, remoteManifest)
		assert.NoError(t, err)

		// Create updated instance ZIP data
		instanceZipData := createTestZipData()
		err = remoteStorage.Put(ctx, "instance.zip", instanceZipData)
		assert.NoError(t, err)

		// Execute Prepare - should update local instance
		err = molfar.Prepare()
		assert.NoError(t, err)

		// Verify local manifest was updated
		updatedLocalManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", updatedLocalManifest.InstanceVersion)
		assert.Empty(t, updatedLocalManifest.LockedBy)
	})

	t.Run("existing local manifest, outdated worlds", func(t *testing.T) {
		molfar, librarian, _, remoteStorage, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()

		// Create local manifest with old world
		oldWorld := createTestWorld("worlds/old-world.zip")
		localManifest := createTestManifest("v1.0.0", []domain.World{oldWorld})
		err := librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		// Create instance directory structure with old world
		instancePath := filepath.Join(workdir, "instance")
		err = os.MkdirAll(instancePath, 0755)
		assert.NoError(t, err)

		// Create old world directory
		oldWorldPath := filepath.Join(instancePath, "world")
		err = os.MkdirAll(oldWorldPath, 0755)
		assert.NoError(t, err)

		// Create a test file in the old world
		testFile := filepath.Join(oldWorldPath, "test.txt")
		err = os.WriteFile(testFile, []byte("old world content"), 0644)
		assert.NoError(t, err)

		// Create remote manifest with new world
		newWorld := createTestWorld("worlds/new-world.zip")
		remoteManifest := createTestManifest("v1.0.0", []domain.World{newWorld})
		err = librarian.SaveRemoteManifest(ctx, remoteManifest)
		assert.NoError(t, err)

		// Create instance ZIP data
		instanceZipData := createTestZipData()
		err = remoteStorage.Put(ctx, "instance.zip", instanceZipData)
		assert.NoError(t, err)

		// Create new world ZIP data
		worldZipData := createTestZipData()
		err = remoteStorage.Put(ctx, "worlds/new-world.zip", worldZipData)
		assert.NoError(t, err)

		// Execute Prepare - should update local worlds
		err = molfar.Prepare()
		assert.NoError(t, err)

		// Verify local manifest was updated with new world
		updatedLocalManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.Len(t, updatedLocalManifest.StoredWorlds, 1)
		assert.Equal(t, "worlds/new-world.zip", updatedLocalManifest.StoredWorlds[0].URI)
		assert.Empty(t, updatedLocalManifest.LockedBy)
	})

	t.Run("lock conflict scenario", func(t *testing.T) {
		molfar, librarian, _, _, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		testWorld := createTestWorld("worlds/test-world.zip")

		// Create local manifest with lock
		localManifest := createTestManifest("v1.0.0", []domain.World{testWorld})
		localManifest.LockedBy = "other-host__1234567890"
		err := librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		// Create remote manifest
		remoteManifest := createTestManifest("v1.0.0", []domain.World{testWorld})
		err = librarian.SaveRemoteManifest(ctx, remoteManifest)
		assert.NoError(t, err)

		// Execute Prepare - should fail due to lock conflict
		err = molfar.Prepare()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lock conflict")
	})
}

func TestMolfarService_Run(t *testing.T) {
	t.Run("successful lock acquisition and server execution", func(t *testing.T) {
		molfar, librarian, _, _, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		testWorld := createTestWorld("worlds/test-world.zip")
		localManifest := createTestManifest("v1.0.0", []domain.World{testWorld})
		err := librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		server, err := domain.NewServer("127.0.0.1:25565", 1024)
		assert.NoError(t, err)

		err = molfar.Run(server)
		assert.NoError(t, err)

		// Verify manifest is locked after Run
		lockedManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.NotEmpty(t, lockedManifest.LockedBy)
		assert.Contains(t, lockedManifest.LockedBy, "__")
	})

	t.Run("nil server argument", func(t *testing.T) {
		molfar, _, _, _, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		err := molfar.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server cannot be nil")
	})

	t.Run("nil molfar service", func(t *testing.T) {
		var molfar *services.MolfarService
		server, err := domain.NewServer("127.0.0.1:25565", 1024)
		assert.NoError(t, err)

		err = molfar.Run(server)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "molfar service cannot be nil")
	})

	t.Run("local manifest already locked", func(t *testing.T) {
		molfar, librarian, _, _, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		testWorld := createTestWorld("worlds/test-world.zip")
		localManifest := createTestManifest("v1.0.0", []domain.World{testWorld})
		localManifest.LockedBy = "other-host__1234567890"
		err := librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		server, err := domain.NewServer("127.0.0.1:25565", 1024)
		assert.NoError(t, err)

		err = molfar.Run(server)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "local manifest already locked")
	})
}

func TestMolfarService_Exit(t *testing.T) {
	t.Run("successful exit sequence", func(t *testing.T) {
		molfar, librarian, localStorage, remoteStorage, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()

		// Setup test world directories
		instancePath := filepath.Join(workdir, "instance")
		err := os.MkdirAll(filepath.Join(instancePath, "world"), 0755)
		assert.NoError(t, err)
		err = os.MkdirAll(filepath.Join(instancePath, "world_nether"), 0755)
		assert.NoError(t, err)
		err = os.MkdirAll(filepath.Join(instancePath, "world_the_end"), 0755)
		assert.NoError(t, err)

		// Create test files in world directories
		err = os.WriteFile(filepath.Join(instancePath, "world", "level.dat"), []byte("test data"), 0644)
		assert.NoError(t, err)

		// Setup manifest with lock
		localManifest := createTestManifest("v1.0.0", []domain.World{})
		localManifest.LockedBy = "test-host__1234567890"
		err = librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		err = molfar.Exit()
		assert.NoError(t, err)

		// Verify manifest is unlocked
		updatedManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.Empty(t, updatedManifest.LockedBy)
		assert.Len(t, updatedManifest.StoredWorlds, 1)

		// Verify local and remote manifests are synchronized
		remoteManifest, err := librarian.GetRemoteManifest(ctx)
		assert.NoError(t, err)
		assert.Equal(t, updatedManifest.RitualVersion, remoteManifest.RitualVersion)
		assert.Equal(t, updatedManifest.InstanceVersion, remoteManifest.InstanceVersion)
		assert.Equal(t, updatedManifest.StoredWorlds, remoteManifest.StoredWorlds)
		assert.Equal(t, updatedManifest.LockedBy, remoteManifest.LockedBy)

		// Verify world is stored in remote storage
		assert.Len(t, updatedManifest.StoredWorlds, 1)
		worldURI := updatedManifest.StoredWorlds[0].URI
		assert.Contains(t, worldURI, "worlds/")
		assert.Contains(t, worldURI, ".zip")

		// Verify world data exists in remote storage
		worldData, err := remoteStorage.Get(ctx, worldURI)
		assert.NoError(t, err)
		assert.NotEmpty(t, worldData)

		// Verify local backup was created
		backupKeys, err := localStorage.List(ctx, "local-backups/")
		assert.NoError(t, err)
		assert.Len(t, backupKeys, 1)
		assert.Contains(t, backupKeys[0], "local-backups/")
		assert.Contains(t, backupKeys[0], ".zip")
	})

	t.Run("nil service error handling", func(t *testing.T) {
		t.Run("nil molfar", func(t *testing.T) {
			var molfar *services.MolfarService
			err := molfar.Exit()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "molfar service cannot be nil")
		})

		t.Run("nil librarian", func(t *testing.T) {
			_, _, _, _, _, cleanup := setupIntegrationTest(t)
			defer cleanup()

			molfar := &services.MolfarService{}
			err := molfar.Exit()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "librarian service cannot be nil")
		})
	})
}

func TestMolfarService_copyWorldsToTemp(t *testing.T) {
	t.Run("world directory operations", func(t *testing.T) {
		molfar, librarian, _, _, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		instancePath := filepath.Join(workdir, "instance")

		// Setup test world directories
		err := os.MkdirAll(filepath.Join(instancePath, "world"), 0755)
		assert.NoError(t, err)
		err = os.MkdirAll(filepath.Join(instancePath, "world_nether"), 0755)
		assert.NoError(t, err)
		err = os.MkdirAll(filepath.Join(instancePath, "world_the_end"), 0755)
		assert.NoError(t, err)

		// Create test files
		err = os.WriteFile(filepath.Join(instancePath, "world", "level.dat"), []byte("world data"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(instancePath, "world_nether", "level.dat"), []byte("nether data"), 0644)
		assert.NoError(t, err)

		// Setup manifest with lock
		localManifest := createTestManifest("v1.0.0", []domain.World{})
		localManifest.LockedBy = "test-host__1234567890"
		err = librarian.SaveLocalManifest(ctx, localManifest)
		assert.NoError(t, err)

		// Test the Exit method which calls copyWorldsToTemp
		err = molfar.Exit()
		assert.NoError(t, err)
	})
}

func TestMolfarService_createLocalBackup(t *testing.T) {
	t.Run("successful local backup creation", func(t *testing.T) {
		molfar, _, localStorage, _, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		timestamp := time.Now().Unix()

		// Create test archive file
		archivePath := filepath.Join(workdir, "test-backup.zip")
		testData := []byte("test archive data")
		err := os.WriteFile(archivePath, testData, 0644)
		assert.NoError(t, err)

		// Create local backup
		err = molfar.CreateLocalBackup(ctx, archivePath, timestamp)
		assert.NoError(t, err)

		// Verify backup was created
		backupKeys, err := localStorage.List(ctx, "local-backups/")
		assert.NoError(t, err)
		assert.Len(t, backupKeys, 1)

		// Verify backup content
		backupData, err := localStorage.Get(ctx, backupKeys[0])
		assert.NoError(t, err)
		assert.Equal(t, testData, backupData)
	})

	t.Run("nil context error", func(t *testing.T) {
		molfar, _, _, _, workdir, cleanup := setupIntegrationTest(t)
		defer cleanup()

		archivePath := filepath.Join(workdir, "test-backup.zip")
		err := os.WriteFile(archivePath, []byte("test"), 0644)
		assert.NoError(t, err)

		var ctx context.Context
		err = molfar.CreateLocalBackup(ctx, archivePath, time.Now().Unix())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})

	t.Run("empty archive path error", func(t *testing.T) {
		molfar, _, _, _, _, cleanup := setupIntegrationTest(t)
		defer cleanup()

		ctx := context.Background()
		err := molfar.CreateLocalBackup(ctx, "", time.Now().Unix())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "archive path cannot be empty")
	})
}
