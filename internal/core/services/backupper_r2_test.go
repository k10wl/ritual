package services_test

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R2Backupper Tests
//
// TDD approach: Write tests first, then implement R2Backupper to make them pass.
// R2Backupper creates archives from world directories and uploads to R2 storage.
// Uses FSRepository as mock for R2 storage in tests.

func setupR2BackupperServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.ArchiveService,
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

	// Create remote storage (FS for testing - simulates R2)
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err)

	// Create archive service
	archiveService, err := services.NewArchiveService(tempRoot)
	require.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return localStorage, remoteStorage, archiveService, tempDir, tempRoot, cleanup
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
		localStorage, remoteStorage, archive, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, archiveName)
	})

	t.Run("uploads archive to R2 storage", func(t *testing.T) {
		localStorage, remoteStorage, archive, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify backup was uploaded to remote storage
		backupFiles, err := remoteStorage.List(ctx, config.LocalBackups)
		assert.NoError(t, err)
		assert.NotEmpty(t, backupFiles, "Backup file should be uploaded to R2")
	})

	t.Run("returns archive URI for manifest", func(t *testing.T) {
		localStorage, remoteStorage, archive, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)

		// Archive name should be non-empty and usable for manifest
		assert.NotEmpty(t, archiveName)
	})

	t.Run("applies retention policy - max 5 backups", func(t *testing.T) {
		localStorage, remoteStorage, archive, tempDir, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		// Setup world data
		setupR2BackupperWorldData(t, tempDir)

		ctx := context.Background()

		// Create 7 fake backup files in remote storage (exceeds max of 5)
		for i := 0; i < 7; i++ {
			// Create fake backup files with different timestamps
			timestamp := time.Now().Add(time.Duration(-i*24) * time.Hour).Format("20060102150405")
			filename := filepath.Join(config.LocalBackups, timestamp+".zip")
			err := remoteStorage.Put(ctx, filename, []byte("PK\x03\x04fake backup data"))
			require.NoError(t, err)
		}

		// Create R2Backupper
		backupper, err := services.NewR2Backupper(
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup - this should apply retention
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify retention was applied - should have at most 5 backups
		backupFiles, err := remoteStorage.List(ctx, config.LocalBackups)
		assert.NoError(t, err)
		// Filter only .zip files
		zipCount := 0
		for _, f := range backupFiles {
			if filepath.Ext(f) == ".zip" {
				zipCount++
			}
		}
		assert.LessOrEqual(t, zipCount, 5, "Should have at most 5 backup files after retention")
	})

	t.Run("nil context returns error", func(t *testing.T) {
		localStorage, remoteStorage, archive, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		backupper, err := services.NewR2Backupper(
			localStorage,
			remoteStorage,
			archive,
			workRoot,
		)
		require.NoError(t, err)

		_, err = backupper.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewR2Backupper(t *testing.T) {
	t.Run("nil localStorage returns error", func(t *testing.T) {
		_, remoteStorage, archive, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(nil, remoteStorage, archive, workRoot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage")
	})

	t.Run("nil remoteStorage returns error", func(t *testing.T) {
		localStorage, _, archive, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(localStorage, nil, archive, workRoot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage")
	})

	t.Run("nil archive returns error", func(t *testing.T) {
		localStorage, remoteStorage, _, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(localStorage, remoteStorage, nil, workRoot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "archive")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		localStorage, remoteStorage, archive, _, _, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		_, err := services.NewR2Backupper(localStorage, remoteStorage, archive, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns backupper", func(t *testing.T) {
		localStorage, remoteStorage, archive, _, workRoot, cleanup := setupR2BackupperServices(t)
		defer cleanup()

		backupper, err := services.NewR2Backupper(localStorage, remoteStorage, archive, workRoot)
		assert.NoError(t, err)
		assert.NotNil(t, backupper)
	})
}
