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
)

func createTestZipData() []byte {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add a simple file to make it a valid ZIP
	file, err := zipWriter.Create("test.txt")
	if err != nil {
		panic(err)
	}
	file.Write([]byte("test content"))

	zipWriter.Close()
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

	archive := services.NewArchiveService()

	mockServerRunner := &mocks.MockServerRunner{}
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

		// Execute Prepare - should initialize local instance
		err = molfar.Prepare()
		if err != nil {
			t.Logf("Prepare failed: %v", err)
		}
		assert.NoError(t, err)

		// Verify local manifest was created
		localManifest, err := librarian.GetLocalManifest(ctx)
		assert.NoError(t, err)
		assert.Equal(t, remoteManifest.InstanceVersion, localManifest.InstanceVersion)
		assert.Equal(t, remoteManifest.RitualVersion, localManifest.RitualVersion)
		assert.NotEmpty(t, localManifest.LockedBy)

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
		assert.NotEmpty(t, updatedLocalManifest.LockedBy)
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
		assert.NotEmpty(t, updatedLocalManifest.LockedBy)
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
	molfar, _, _, _, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	err := molfar.Run()
	assert.NoError(t, err)
}

func TestMolfarService_Exit(t *testing.T) {
	molfar, _, _, _, _, cleanup := setupIntegrationTest(t)
	defer cleanup()

	err := molfar.Exit()
	assert.NoError(t, err)
}
