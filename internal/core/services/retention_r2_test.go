package services_test

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR2Retention_ManifestShouldNotAccumulateWorlds(t *testing.T) {
	// This test verifies that after retention deletes old backups,
	// the manifest's Backups should also be cleaned up
	// BUG: Currently manifest accumulates worlds beyond R2MaxBackups

	tempDir := t.TempDir()
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer tempRoot.Close()

	// Create remote storage
	remoteStorage, err := adapters.NewFSRepository(tempRoot)
	require.NoError(t, err)
	defer remoteStorage.Close()

	ctx := context.Background()

	// Create backup directory
	backupDir := filepath.Join(tempDir, config.RemoteBackups)
	err = os.MkdirAll(backupDir, 0755)
	require.NoError(t, err)

	// Create more backups than R2MaxBackups allows (R2MaxBackups = 2)
	numBackups := config.R2MaxBackups + 2 // 4 backups
	var worlds []domain.World

	for i := 0; i < numBackups; i++ {
		timestamp := time.Now().Add(time.Duration(-i) * time.Hour).Format(config.TimestampFormat)
		filename := timestamp + config.BackupExtension
		// Use forward slashes for URI (as stored in manifest)
		key := config.RemoteBackups + "/" + filename

		// Create the backup file (use OS path for file creation)
		filePath := filepath.Join(tempDir, config.RemoteBackups, filename)
		err = os.WriteFile(filePath, []byte("backup data"), 0644)
		require.NoError(t, err)

		// Add to worlds list
		worlds = append(worlds, domain.World{
			URI:       key,
			CreatedAt: time.Now().Add(time.Duration(-i) * time.Hour),
		})
	}

	// Create manifest with all worlds
	manifest := &domain.Manifest{
		Backups: worlds,
	}

	// Verify we start with more worlds than allowed
	assert.Equal(t, numBackups, len(manifest.Backups), "Should start with %d worlds", numBackups)

	// Create and run retention
	retention, err := services.NewR2Retention(remoteStorage, nil)
	require.NoError(t, err)

	err = retention.Apply(ctx, manifest)
	require.NoError(t, err)

	// Verify files were deleted
	remainingFiles, err := remoteStorage.List(ctx, config.RemoteBackups)
	require.NoError(t, err)

	var backupFiles []string
	for _, f := range remainingFiles {
		if strings.HasSuffix(f, config.BackupExtension) {
			backupFiles = append(backupFiles, f)
		}
	}
	assert.Equal(t, config.R2MaxBackups, len(backupFiles), "Should have only %d backup files after retention", config.R2MaxBackups)

	// BUG: This assertion will FAIL because manifest.Backups is not updated
	// After retention, manifest should only have R2MaxBackups worlds
	assert.Equal(t, config.R2MaxBackups, len(manifest.Backups),
		"BUG: Manifest should have only %d worlds after retention, but has %d",
		config.R2MaxBackups, len(manifest.Backups))
}
