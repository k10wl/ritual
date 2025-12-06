package services_test

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LocalBackupper Tests
//
// LocalBackupper creates streaming tar.gz archives from world directories and stores them locally.

func setupLocalBackupperServices(t *testing.T) (
	*adapters.FSRepository,
	string,
	*os.Root,
	func(),
) {
	tempDir := t.TempDir()

	// Create root for safe operations
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempRoot)
	require.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
	}

	return localStorage, tempDir, tempRoot, cleanup
}

func setupLocalBackupperWorldData(t *testing.T, tempDir string) {
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

func TestLocalBackupper_Run(t *testing.T) {
	t.Run("creates archive from world directories", func(t *testing.T) {
		localStorage, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(
			localStorage,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, archiveName)
		assert.True(t, strings.HasSuffix(archiveName, ".tar.gz"))
	})

	t.Run("stores archive in local backup directory", func(t *testing.T) {
		localStorage, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(
			localStorage,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify backup was created in backup directory
		backupFiles, err := localStorage.List(ctx, config.LocalBackups)
		assert.NoError(t, err)
		assert.NotEmpty(t, backupFiles, "Backup file should be created")
	})

	t.Run("returns archive name for manifest", func(t *testing.T) {
		localStorage, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(
			localStorage,
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

	t.Run("applies retention policy - max 10 backups", func(t *testing.T) {
		localStorage, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		ctx := context.Background()

		// Create 12 fake backup files (exceeds max of 10)
		backupDir := filepath.Join(tempDir, config.LocalBackups)
		err := os.MkdirAll(backupDir, 0755)
		require.NoError(t, err)

		for i := 0; i < 12; i++ {
			// Create fake backup files with different timestamps
			timestamp := time.Now().Add(time.Duration(-i*24) * time.Hour).Format("20060102150405")
			filename := filepath.Join(config.LocalBackups, timestamp+".tar.gz")
			err := localStorage.Put(ctx, filename, []byte("fake tar.gz data"))
			require.NoError(t, err)
		}

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(
			localStorage,
			workRoot,
		)
		require.NoError(t, err)

		// Execute backup - this should apply retention
		_, err = backupper.Run(ctx)
		require.NoError(t, err)

		// Verify retention was applied - should have at most 10 backups
		backupFiles, err := localStorage.List(ctx, config.LocalBackups)
		assert.NoError(t, err)
		// Filter only .tar.gz files
		tarGzCount := 0
		for _, f := range backupFiles {
			if strings.HasSuffix(f, ".tar.gz") {
				tarGzCount++
			}
		}
		assert.LessOrEqual(t, tarGzCount, 10, "Should have at most 10 backup files after retention")
	})

	t.Run("nil context returns error", func(t *testing.T) {
		localStorage, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		backupper, err := services.NewLocalBackupper(
			localStorage,
			workRoot,
		)
		require.NoError(t, err)

		_, err = backupper.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewLocalBackupper(t *testing.T) {
	t.Run("nil localStorage returns error", func(t *testing.T) {
		_, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		_, err := services.NewLocalBackupper(nil, workRoot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage")
	})

	t.Run("nil workRoot returns error", func(t *testing.T) {
		localStorage, _, _, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		_, err := services.NewLocalBackupper(localStorage, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("valid dependencies returns backupper", func(t *testing.T) {
		localStorage, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		backupper, err := services.NewLocalBackupper(localStorage, workRoot)
		assert.NoError(t, err)
		assert.NotNil(t, backupper)
	})
}
