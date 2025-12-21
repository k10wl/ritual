package services_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/adapters/streamer"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R2Backupper Tests
//
// R2Backupper creates streaming tar.gz archives from world directories and uploads to R2 storage.
// Uses mockStreamUploader and FSRepository to simulate R2 storage in tests.

// mockStreamUploader implements streamer.S3StreamUploader for testing
type mockStreamUploader struct {
	storage   *adapters.FSRepository
	basePath  string
	uploadErr error
}

func (m *mockStreamUploader) Upload(ctx context.Context, bucket, key string, body io.Reader, _ int64) (int64, error) {
	if m.uploadErr != nil {
		return 0, m.uploadErr
	}

	// Read the stream and store using FSRepository
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}

	err = m.storage.Put(ctx, key, data)
	if err != nil {
		return 0, err
	}

	return int64(len(data)), nil
}

func setupR2BackupperServices(t *testing.T) (
	*mockStreamUploader,
	*adapters.FSRepository,
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

	// Create remote storage (FS for testing - simulates R2)
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err)

	// Create mock uploader that writes to remote storage
	uploader := &mockStreamUploader{
		storage:  remoteStorage,
		basePath: remoteTempDir,
	}

	cleanup := func() {
		remoteStorage.Close()
		tempRoot.Close()
	}

	return uploader, remoteStorage, tempDir, tempRoot, cleanup
}

func setupR2BackupperWorldData(t *testing.T, tempDir string) {
	// Create instance directory with world data
	instancePath := filepath.Join(tempDir, config.InstanceDir)
	err := os.MkdirAll(instancePath, 0755)
	require.NoError(t, err)

	instanceRoot, err := os.OpenRoot(instancePath)
	require.NoError(t, err)
	defer instanceRoot.Close()

	// Use test helper to create Paper world
	_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceRoot)
	require.NoError(t, err)
}

func TestR2Backupper_Run(t *testing.T) {
	t.Run("creates archive from world directories", func(t *testing.T) {
		uploader, _, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			false, // no local backup
			nil,   // no backup condition
			nil,   // no events
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, archiveName)
		assert.True(t, strings.HasSuffix(archiveName, ".tar"))
	})

	t.Run("uploads archive to R2 storage", func(t *testing.T) {
		uploader, remoteStorage, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			false,
			nil,
			nil,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify backup was uploaded to remote storage
		backupFiles, err := remoteStorage.List(ctx, config.RemoteBackups)
		assert.NoError(t, err)
		assert.NotEmpty(t, backupFiles, "Backup file should be uploaded to R2")
	})

	t.Run("returns archive key for manifest", func(t *testing.T) {
		uploader, _, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			false,
			nil,
			nil,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveKey, err := backupper.Run(ctx)
		require.NoError(t, err)

		// Archive key should be non-empty and usable for manifest
		assert.NotEmpty(t, archiveKey)
		assert.True(t, strings.HasPrefix(archiveKey, config.RemoteBackups+"/"))
	})

	t.Run("nil context returns error", func(t *testing.T) {
		uploader, _, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			false,
			nil,
			nil,
		)
		require.NoError(t, err)

		_, err = backupper.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewR2Backupper(t *testing.T) {
	t.Run("nil uploader returns error", func(t *testing.T) {
		_, _, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(nil, "bucket", workRoot, false, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "uploader")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		uploader, _, _, _, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(uploader, "bucket", nil, false, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns backupper", func(t *testing.T) {
		uploader, _, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		backupper, err := services.NewR2Backupper(uploader, "bucket", workRoot, false, nil, nil)
		assert.NoError(t, err)
		assert.NotNil(t, backupper)
	})
}

// TestR2Backupper_LocalBackup tests the optional local backup feature
func TestR2Backupper_LocalBackup(t *testing.T) {
	t.Run("creates local backup when enabled", func(t *testing.T) {
		uploader, _, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper with local backup enabled
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			true, // saveLocalBackup
			nil,
			nil,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify local backup was created in config.LocalBackups directory
		localBackupDir := filepath.Join(tempDir, config.LocalBackups)
		files, err := os.ReadDir(localBackupDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, files, "Local backup should be created")
	})

	t.Run("skips local backup when disabled", func(t *testing.T) {
		uploader, _, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper with local backup disabled
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			false, // saveLocalBackup
			nil,
			nil,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify local backup was NOT created
		localBackupDir := filepath.Join(tempDir, config.LocalBackups)
		_, err = os.ReadDir(localBackupDir)
		assert.True(t, os.IsNotExist(err), "Local backup directory should not exist")
	})

	t.Run("skips local backup when condition false", func(t *testing.T) {
		uploader, _, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper with local backup enabled but condition false
		backupper, err := services.NewR2Backupper(
			uploader,
			"test-bucket",
			workRoot,
			true,
			func() bool { return false }, // condition returns false
			nil,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify local backup was NOT created (condition prevented it)
		localBackupDir := filepath.Join(tempDir, config.LocalBackups)
		_, err = os.ReadDir(localBackupDir)
		assert.True(t, os.IsNotExist(err), "Local backup directory should not exist when condition is false")
	})
}

// TestR2Backupper_StreamingVerification verifies the archive is valid tar.gz
func TestR2Backupper_StreamingVerification(t *testing.T) {
	t.Run("produces valid tar.gz archive", func(t *testing.T) {
		// Use a capturing uploader to verify the content
		var capturedData bytes.Buffer
		capturingUploader := &capturingStreamUploader{buf: &capturedData}

		tempDir := t.TempDir()

		tempRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		backupper, err := services.NewR2Backupper(
			capturingUploader,
			"test-bucket",
			tempRoot,
			false,
			nil,
			nil,
		)
		require.NoError(t, err)

		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify the captured data is valid by extracting it
		extractDir := t.TempDir()
		mockDownloader := &mockStreamDownloader{data: capturedData.Bytes()}

		err = streamer.Pull(ctx, streamer.PullConfig{
			Bucket:   "test",
			Key:      "test.tar",
			Dest:     extractDir,
			Conflict: streamer.Replace,
		}, mockDownloader)
		require.NoError(t, err)

		// Verify extracted content has world directory
		_, err = os.Stat(filepath.Join(extractDir, "world"))
		assert.NoError(t, err, "world directory should be extracted")
	})
}

// capturingStreamUploader captures the uploaded data for verification
type capturingStreamUploader struct {
	buf *bytes.Buffer
}

func (c *capturingStreamUploader) Upload(ctx context.Context, bucket, key string, body io.Reader, estimatedSize int64) (int64, error) {
	n, err := io.Copy(c.buf, body)
	return n, err
}

// mockStreamDownloader implements streamer.S3StreamDownloader for testing
type mockStreamDownloader struct {
	data []byte
}

func (m *mockStreamDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}
