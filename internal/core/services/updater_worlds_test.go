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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WorldsUpdater Tests
//
// TDD approach: Write tests first, then implement WorldsUpdater to make them pass.
// Uses same patterns as molfar_test.go - real FS storage, testhelpers for setup.

func setupWorldsUpdaterServices(t *testing.T) (
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

func createWorldsTestManifest(ritualVersion string, instanceVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   ritualVersion,
		InstanceVersion: instanceVersion,
		StoredWorlds:    worlds,
		UpdatedAt:       time.Now(),
	}
}

func createWorldsTestWorld(uri string) domain.World {
	return domain.World{
		URI:       uri,
		CreatedAt: time.Now(),
	}
}

func setupWorldsRemoteManifest(t *testing.T, remoteStorage *adapters.FSRepository, manifestVersion string, instanceVersion string, worldURI string) {
	ctx := context.Background()
	world := createWorldsTestWorld(worldURI)
	remoteManifest := createWorldsTestManifest(manifestVersion, instanceVersion, []domain.World{world})
	manifestData, err := json.Marshal(remoteManifest)
	require.NoError(t, err)
	err = remoteStorage.Put(ctx, "manifest.json", manifestData)
	require.NoError(t, err)
}

func setupWorldsRemoteWorldZip(t *testing.T, remoteStorage *adapters.FSRepository, remoteTempDir string, worldURI string) {
	ctx := context.Background()

	// Create world directory structure in remote temp dir
	worldsDir := filepath.Join(remoteTempDir, config.RemoteBackups)
	err := os.MkdirAll(worldsDir, 0755)
	require.NoError(t, err)

	worldsRoot, err := os.OpenRoot(worldsDir)
	require.NoError(t, err)
	defer worldsRoot.Close()

	// Use test helper to create Paper world
	_, _, _, err = testhelpers.PaperMinecraftWorldSetup(worldsRoot)
	require.NoError(t, err)

	// Create zip file using standard zip package
	zipPath := filepath.Join(remoteTempDir, worldURI)
	err = os.MkdirAll(filepath.Dir(zipPath), 0755)
	require.NoError(t, err)

	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through worlds directory and add files to zip
	err = filepath.Walk(worldsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == worldsDir {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(worldsDir, path)
		require.NoError(t, err)

		// Only include world directories
		if !strings.HasPrefix(relPath, "world") {
			return nil
		}

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

	err = remoteStorage.Put(ctx, worldURI, zipData)
	require.NoError(t, err)
}

func TestWorldsUpdater_Run(t *testing.T) {
	t.Run("no worlds - downloads and extracts worlds", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, tempDir, remoteTempDir, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Setup remote data with world
		worldURI := config.RemoteBackups + "/1234567890.zip"
		setupWorldsRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1", worldURI)
		setupWorldsRemoteWorldZip(t, remoteStorage, remoteTempDir, worldURI)

		// Create local manifest without worlds
		ctx := context.Background()
		localManifest := createWorldsTestManifest("1.0.0", "1.20.1", []domain.World{})
		manifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		require.NoError(t, err)

		// Create instance directory (worlds are extracted into instance)
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		err = os.MkdirAll(instancePath, 0755)
		require.NoError(t, err)

		// Create WorldsUpdater
		updater, err := services.NewWorldsUpdater(
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

		// Verify world directories were created
		worldPath := filepath.Join(instancePath, "world")
		_, err = os.Stat(worldPath)
		assert.NoError(t, err)

		// Verify local manifest was updated with world
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)

		var manifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &manifestObj)
		assert.NoError(t, err)
		assert.NotEmpty(t, manifestObj.StoredWorlds)
	})

	t.Run("outdated world - updates worlds", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, tempDir, remoteTempDir, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Setup remote data with newer world
		worldURI := config.RemoteBackups + "/9999999999.zip"
		setupWorldsRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1", worldURI)
		setupWorldsRemoteWorldZip(t, remoteStorage, remoteTempDir, worldURI)

		// Create local manifest with older world
		ctx := context.Background()
		oldWorld := createWorldsTestWorld(config.RemoteBackups + "/old.zip")
		localManifest := createWorldsTestManifest("1.0.0", "1.20.1", []domain.World{oldWorld})
		manifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", manifestData)
		require.NoError(t, err)

		// Create instance directory
		instancePath := filepath.Join(tempDir, config.InstanceDir)
		err = os.MkdirAll(instancePath, 0755)
		require.NoError(t, err)

		// Create WorldsUpdater
		updater, err := services.NewWorldsUpdater(
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

		// Verify world directories were created
		worldPath := filepath.Join(instancePath, "world")
		_, err = os.Stat(worldPath)
		assert.NoError(t, err)

		// Verify local manifest was updated with new world
		updatedManifest, err := localStorage.Get(ctx, "manifest.json")
		assert.NoError(t, err)

		var manifestObj domain.Manifest
		err = json.Unmarshal(updatedManifest, &manifestObj)
		assert.NoError(t, err)
		assert.Contains(t, manifestObj.GetLatestWorld().URI, "9999999999.zip")
	})

	t.Run("up-to-date worlds - no download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Create a fixed timestamp for both local and remote
		fixedTime := time.Now()
		worldURI := config.RemoteBackups + "/1234567890.zip"
		world := domain.World{
			URI:       worldURI,
			CreatedAt: fixedTime,
		}

		// Setup remote manifest with exact same world
		ctx := context.Background()
		remoteManifest := &domain.Manifest{
			RitualVersion:   "1.0.0",
			InstanceVersion: "1.20.1",
			StoredWorlds:    []domain.World{world},
			UpdatedAt:       fixedTime,
		}
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Create local manifest with exact same world
		localManifest := &domain.Manifest{
			RitualVersion:   "1.0.0",
			InstanceVersion: "1.20.1",
			StoredWorlds:    []domain.World{world},
			UpdatedAt:       fixedTime,
		}
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		// Create WorldsUpdater
		updater, err := services.NewWorldsUpdater(
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
		assert.Equal(t, worldURI, manifestObj.GetLatestWorld().URI)
	})

	t.Run("empty remote worlds - skip download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Setup remote manifest with NO worlds
		ctx := context.Background()
		remoteManifest := createWorldsTestManifest("1.0.0", "1.20.1", []domain.World{})
		manifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", manifestData)
		require.NoError(t, err)

		// Create local manifest
		localManifest := createWorldsTestManifest("1.0.0", "1.20.1", []domain.World{})
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		// Create WorldsUpdater
		updater, err := services.NewWorldsUpdater(
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
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewWorldsUpdater(
			librarian,
			validator,
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewWorldsUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, remoteStorage, _, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		localStorage, remoteStorage, librarian, _, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		_, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		localStorage, _, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		localStorage, remoteStorage, librarian, validator, _, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		localStorage, remoteStorage, librarian, validator, archive, _, _, _, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
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
		localStorage, remoteStorage, librarian, validator, archive, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewWorldsUpdater(
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
