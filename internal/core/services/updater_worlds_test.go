package services_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/adapters/streamer"
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
// Uses streamer.Pull for downloading and extracting world archives.
// Uses mockWorldsDownloader to simulate R2 storage in tests.

// mockWorldsDownloader implements streamer.S3StreamDownloader for testing
type mockWorldsDownloader struct {
	data map[string][]byte
}

func (m *mockWorldsDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if data, ok := m.data[key]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, io.EOF
}

func setupWorldsUpdaterServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.LibrarianService,
	*services.ValidatorService,
	*mockWorldsDownloader,
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

	// Create mock downloader
	downloader := &mockWorldsDownloader{
		data: make(map[string][]byte),
	}

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	require.NoError(t, err)

	// Create validator service
	validatorService, err := services.NewValidatorService()
	require.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
		tempRoot.Close()
	}

	return localStorage, remoteStorage, librarianService, validatorService, downloader, tempDir, remoteTempDir, tempRoot, cleanup
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

func setupWorldsRemoteTar(t *testing.T, downloader *mockWorldsDownloader, remoteTempDir string, worldURI string) {
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

	// Create tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Walk through worlds directory and add files to tar
	err = filepath.Walk(worldsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == worldsDir {
			return nil
		}

		relPath, err := filepath.Rel(worldsDir, path)
		if err != nil {
			return err
		}

		// Only include world directories
		if !strings.HasPrefix(relPath, "world") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}
		}

		return nil
	})
	require.NoError(t, err)

	// Close tar writer
	require.NoError(t, tw.Close())

	// Store in mock downloader with the world URI as key
	downloader.data[worldURI] = buf.Bytes()
}

func TestWorldsUpdater_Run(t *testing.T) {
	t.Run("no worlds - downloads and extracts worlds", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, downloader, tempDir, remoteTempDir, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Setup remote data with world
		worldURI := config.RemoteBackups + "/1234567890.tar"
		setupWorldsRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1", worldURI)
		setupWorldsRemoteTar(t, downloader, remoteTempDir, worldURI)

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
			downloader,
			"test-bucket",
			workRoot,
			nil,
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
		localStorage, remoteStorage, librarian, validator, downloader, tempDir, remoteTempDir, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Setup remote data with newer world
		worldURI := config.RemoteBackups + "/9999999999.tar"
		setupWorldsRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1", worldURI)
		setupWorldsRemoteTar(t, downloader, remoteTempDir, worldURI)

		// Create local manifest with older world
		ctx := context.Background()
		oldWorld := createWorldsTestWorld(config.RemoteBackups + "/old.tar")
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
			downloader,
			"test-bucket",
			workRoot,
			nil,
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
		assert.Contains(t, manifestObj.GetLatestWorld().URI, "9999999999.tar")
	})

	t.Run("up-to-date worlds - no download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		// Create a fixed timestamp for both local and remote
		fixedTime := time.Now()
		worldURI := config.RemoteBackups + "/1234567890.tar"
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
			downloader,
			"test-bucket",
			workRoot,
			nil,
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
		localStorage, remoteStorage, librarian, validator, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
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
			downloader,
			"test-bucket",
			workRoot,
			nil,
		)
		require.NoError(t, err)

		// Execute update - should succeed without downloading
		err = updater.Run(ctx)
		assert.NoError(t, err)
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		_, _, librarian, validator, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewWorldsUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			workRoot,
			nil,
		)
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewWorldsUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, _, _, validator, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
			nil, // librarian
			validator,
			downloader,
			"test-bucket",
			workRoot,
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian")
	})

	t.Run("nil validator returns error", func(t *testing.T) {
		_, _, librarian, _, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
			librarian,
			nil, // validator
			downloader,
			"test-bucket",
			workRoot,
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validator")
	})

	t.Run("nil downloader returns error", func(t *testing.T) {
		_, _, librarian, validator, _, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
			librarian,
			validator,
			nil, // downloader
			"test-bucket",
			workRoot,
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "downloader")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		_, _, librarian, validator, downloader, _, _, _, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		_, err := services.NewWorldsUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			nil, // workRoot
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		_, _, librarian, validator, downloader, _, _, workRoot, cleanup := setupWorldsUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewWorldsUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			workRoot,
			nil,
		)
		assert.NoError(t, err)
		assert.NotNil(t, updater)

		// Verify downloader implements the interface
		var _ streamer.S3StreamDownloader = downloader
	})
}
