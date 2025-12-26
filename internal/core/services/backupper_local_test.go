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
	worldDirs := []string{"world", "world_nether", "world_the_end"}

	t.Run("creates archive from world directories", func(t *testing.T) {
		_, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(workRoot, worldDirs, nil, nil)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, archiveName)
		assert.True(t, strings.HasSuffix(archiveName, ".tar"))
	})

	t.Run("stores archive in local backup directory", func(t *testing.T) {
		localStorage, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(workRoot, worldDirs, nil, nil)
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
		_, tempDir, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		// Setup world data
		setupLocalBackupperWorldData(t, tempDir)

		// Create LocalBackupper
		backupper, err := services.NewLocalBackupper(workRoot, worldDirs, nil, nil)
		require.NoError(t, err)

		// Execute backup
		ctx := context.Background()
		archiveName, err := backupper.Run(ctx)
		require.NoError(t, err)

		// Archive name should be non-empty and usable for manifest
		assert.NotEmpty(t, archiveName)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		_, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		backupper, err := services.NewLocalBackupper(workRoot, worldDirs, nil, nil)
		require.NoError(t, err)

		_, err = backupper.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewLocalBackupper(t *testing.T) {
	worldDirs := []string{"world", "world_nether", "world_the_end"}

	t.Run("nil workRoot returns error", func(t *testing.T) {
		_, err := services.NewLocalBackupper(nil, worldDirs, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workRoot")
	})

	t.Run("empty worldDirs returns error", func(t *testing.T) {
		_, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		_, err := services.NewLocalBackupper(workRoot, []string{}, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "worldDirs")
	})

	t.Run("valid dependencies returns backupper", func(t *testing.T) {
		_, _, workRoot, cleanup := setupLocalBackupperServices(t)
		defer cleanup()

		backupper, err := services.NewLocalBackupper(workRoot, worldDirs, nil, nil)
		assert.NoError(t, err)
		assert.NotNil(t, backupper)
	})
}
