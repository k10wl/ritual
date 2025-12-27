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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// InstanceUpdater Tests
//
// Uses streamer.Pull for downloading and extracting instance.tar.gz archives.
// Uses mockTarGzDownloader to simulate R2 storage in tests.

// mockInstanceDownloader implements streamer.S3StreamDownloader for testing
type mockInstanceDownloader struct {
	data map[string][]byte
}

func (m *mockInstanceDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if data, ok := m.data[key]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, io.EOF
}

func setupInstanceUpdaterServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.LibrarianService,
	*services.ValidatorService,
	*mockInstanceDownloader,
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
	downloader := &mockInstanceDownloader{
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

func createInstanceTestManifest(ritualVersion string, instanceVersion string, worlds []domain.World) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   ritualVersion,
		InstanceVersion: instanceVersion,
		Backups:    worlds,
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

func setupInstanceRemoteTar(t *testing.T, downloader *mockInstanceDownloader, remoteTempDir string) {
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

	// Create tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Walk through instance directory and add files to tar
	err = filepath.Walk(instanceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(instanceDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
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

	// Store in mock downloader
	downloader.data[config.InstanceArchiveKey] = buf.Bytes()
}

func TestInstanceUpdater_Run(t *testing.T) {
	t.Run("no local manifest - downloads and extracts instance", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, downloader, tempDir, remoteTempDir, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		// Setup remote data
		setupInstanceRemoteManifest(t, remoteStorage, "1.0.0", "1.20.1")
		setupInstanceRemoteTar(t, downloader, remoteTempDir)

		// Create InstanceUpdater
		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
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
		localStorage, remoteStorage, librarian, validator, downloader, tempDir, remoteTempDir, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		// Setup remote data with newer version
		setupInstanceRemoteManifest(t, remoteStorage, "1.0.0", "1.20.2")
		setupInstanceRemoteTar(t, downloader, remoteTempDir)

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
			downloader,
			"test-bucket",
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
		localStorage, remoteStorage, librarian, validator, downloader, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
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
			downloader,
			"test-bucket",
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
		_, _, librarian, validator, downloader, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			workRoot,
		)
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewInstanceUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, _, _, validator, downloader, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			nil, // librarian
			validator,
			downloader,
			"test-bucket",
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian")
	})

	t.Run("nil validator returns error", func(t *testing.T) {
		_, _, librarian, _, downloader, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			nil, // validator
			downloader,
			"test-bucket",
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validator")
	})

	t.Run("nil downloader returns error", func(t *testing.T) {
		_, _, librarian, validator, _, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			nil, // downloader
			"test-bucket",
			workRoot,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "downloader")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		_, _, librarian, validator, downloader, _, _, _, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		_, err := services.NewInstanceUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			nil, // workRoot
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		_, _, librarian, validator, downloader, _, _, workRoot, cleanup := setupInstanceUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewInstanceUpdater(
			librarian,
			validator,
			downloader,
			"test-bucket",
			workRoot,
		)
		assert.NoError(t, err)
		assert.NotNil(t, updater)

		// Verify it implements the interface
		var _ streamer.S3StreamDownloader = downloader
	})
}
