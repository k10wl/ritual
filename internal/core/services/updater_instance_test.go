package services_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// InstanceUpdater Tests
//
// TDD approach: Write tests first, then implement InstanceUpdater to make them pass.
// Uses same patterns as molfar_test.go - real FS storage, testhelpers for setup.

func setupInstanceUpdaterServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.LibrarianService,
	*services.ValidatorService,
	*services.ArchiveService,
	string,
	string,
	*os.Root,
	func(),
) {
	tempDir := t.TempDir()
	remoteTempDir := t.TempDir()

	// Create roots for safe operations
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)

	remoteRoot, err := os.OpenRoot(remoteTempDir)
	require.NoError(t, err)

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempRoot)
	require.NoError(t, err)

	// Create remote storage (FS for testing)
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err)

	// Create archive service
	archiveService, err := services.NewArchiveService(tempRoot)
	require.NoError(t, err)

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	require.NoError(t, err)

	// Create validator service
	validatorService, err := services.NewValidatorService()
	require.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return localStorage, remoteStorage, librarianService, validatorService, archiveService, tempDir, remoteTempDir, tempRoot, cleanup
}

func createInstanceTestManifest(ritualVersion string, instanceVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   ritualVersion,
		InstanceVersion: instanceVersion,
		StoredWorlds:    worlds,
		UpdatedAt:       time.Now(),
	}
}

func setupInstanceRemoteManifest(t *testing.T, remoteStorage *adapters.FSRepository, manifestVersion string, instanceVersion string) {
	ctx := context.Background()
	remoteManifest := createInstanceTestManifest(manifestVersion, instanceVersion, []domain.World{})
	manifestData, err := json.Marshal(remoteManifest)
	require.NoError(t, err)
	err = remoteStorage.Put(ctx, "manifest.json", manifestData)
	require.NoError(t, err)
}

func setupInstanceRemoteInstanceZip(t *testing.T, remoteStorage *adapters.FSRepository, remoteTempDir string) {
	ctx := context.Background()

	// Create instance directory structure in remote temp dir
	instanceDir := filepath.Join(remoteTempDir, config.InstanceDir)
	err := os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	instanceRoot, err := os.OpenRoot(instanceDir)
	require.NoError(t, err)
	defer instanceRoot.Close()

	// Use test helper to create Paper instance
	_, _, _, err = testhelpers.PaperInstanceSetup(instanceRoot, "1.20.1")
	require.NoError(t, err)

	// Create zip file using standard zip package
	zipPath := filepath.Join(remoteTempDir, "instance.zip")
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through instance directory and add files to zip
	err = filepath.Walk(instanceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(instanceDir, path)
		require.NoError(t, err)

		header, err := zip.FileInfoHeader(info)
		require.NoError(t, err)
		header.Name = relPath

		writer, err := zipWriter.CreateHeader(header)
		require.NoError(t, err)

		file, err := os.Open(path)
		require.NoError(t, err)
		defer file.Close()

		_, err = io.Copy(writer, file)
		require.NoError(t, err)

		return nil
	})
	require.NoError(t, err)

	// Close zip writer to flush
	zipWriter.Close()

	// Read the zip file and store in remote storage
	zipData, err := os.ReadFile(zipPath)
	require.NoError(t, err)

	err = remoteStorage.Put(ctx, "instance.zip", zipData)
	require.NoError(t, err)
}

func TestInstanceUpdater_Run(t *testing.T) {
	t.Run("no local manifest - downloads and extracts instance", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, tempDir, remoteTempDir, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		// Setup remote data
		setupInstanceRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1")
		setupInstanceRemoteInstanceZip(t, remoteStorage, remoteTempDir)

		// Create InstanceUpdater
		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute update
		ctx := context.Background()
		err = updater.Run(ctx)
		require.NoError(t, err)

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)

		// Verify local manifest was created
		localManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)
		assert.NotEmpty(t, localManifest)

		// Verify manifest has correct instance version
		var manifestObj domain.Manifest
		err = json.Unmarshal(localManifest, &manifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "1.20.1", manifestObj.InstanceVersion)
	})

	t.Run("outdated instance version - updates instance", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, tempDir, remoteTempDir, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		// Setup remote data with newer version
		setupInstanceRemoteManifest(t, remoteStorage, "1.0.0", "1.20.2")
		setupInstanceRemoteInstanceZip(t, remoteStorage, remoteTempDir)

		// Create local manifest with older version
		ctx := context.Background()
		oldManifest := createInstanceTestManifest("1.0.0", "1.20.1", []domain.World{})
		manifestData, err := json.Marshal(oldManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		require.NoError(t, err)

		// Create InstanceUpdater
		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute update
		err = updater.Run(ctx)
		require.NoError(t, err)

		// Verify instance directory was created
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		_, err = os.Stat(instancePath)
		assert.NoError(t, err)

		// Verify local manifest was updated
		localManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)

		var manifestObj domain.Manifest
		err = json.Unmarshal(localManifest, &manifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "1.20.2", manifestObj.InstanceVersion)
	})

	t.Run("up-to-date instance - no download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		// Setup remote data
		setupInstanceRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1")

		// Create local manifest with same version
		ctx := context.Background()
		localManifest := createInstanceTestManifest("1.0.0", "1.20.1", []domain.World{})
		manifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		require.NoError(t, err)

		// Create InstanceUpdater
		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute update - should succeed without downloading
		err = updater.Run(ctx)
		assert.NoError(t, err)

		// Manifest should remain unchanged
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)

		var manifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &manifestObj)
		assert.NoError(t, err)
		assert.Equal(t, "1.20.1", manifestObj.InstanceVersion)
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		_, _, librarian, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			nil, // localStorage
			nil, // remoteStorage
			archive,
			workRoot,
		)
		// Constructor should fail with nil dependencies
		assert.Error(t, err)
		assert.Nil(t, updater)
	})
}

func TestNewInstanceUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, remoteStorage, _, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			nil, // librarian
			validator,
			nil,
			remoteStorage,
			archive,
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian")
	})

	t.Run("nil validator returns error", func(t *testing.T) {
		localStorage, remoteStorage, librarian, _, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			nil, // validator
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validator")
	})

	t.Run("nil localStorage returns error", func(t *testing.T) {
		_, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			nil, // localStorage
			remoteStorage,
			archive,
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage")
	})

	t.Run("nil remoteStorage returns error", func(t *testing.T) {
		localStorage, _, librarian, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			nil, // remoteStorage
			archive,
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage")
	})

	t.Run("nil archive returns error", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, _, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			nil, // archive
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "archive")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, _, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			nil, // workRoot
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, updater)
	})
}
