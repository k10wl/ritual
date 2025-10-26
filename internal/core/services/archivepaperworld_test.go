package services_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchivePaperWorld(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)

	fs, err := adapters.NewFSRepository(root)
	require.NoError(t, err)
	defer fs.Close()

	instanceDir := filepath.Join(tempDir, config.InstanceDir)
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	instanceRoot, err := os.OpenRoot(instanceDir)
	require.NoError(t, err)
	defer instanceRoot.Close()

	// Create temporary directory structure mimicking Minecraft world
	_, _, worldsCompareFunc, err := testhelpers.PaperMinecraftWorldSetup(instanceRoot)
	require.NoError(t, err)

	log.Println(fs.List(context.Background(), config.InstanceDir))

	archiveService, err := services.NewArchiveService(root)
	require.NoError(t, err)

	// Execute ArchivePaperWorld
	ctx := context.Background()
	// instanceRoot already opened above

	archivePath, backupName, cleanup, err := services.ArchivePaperWorld(
		ctx,
		fs,
		archiveService,
		instanceRoot,
		"temp",
		"test_backup",
	)

	// Verify results
	require.NoError(t, err)
	require.NotNil(t, archivePath)
	require.Equal(t, "test_backup", backupName)
	require.NotNil(t, cleanup)

	// Verify archive path format
	assert.Equal(t, archivePath, filepath.Join("temp", "test_backup.zip"))

	// Verify archive file exists
	_, err = os.Stat(filepath.Join(tempDir, archivePath))
	require.NoError(t, err, "Archive file should exist")

	// Verify archive file is not empty
	archiveInfo, err := os.Stat(filepath.Join(tempDir, archivePath))
	require.NoError(t, err)
	assert.Greater(t, archiveInfo.Size(), int64(0), "Archive should not be empty")

	extractedDir := "extracted"
	err = archiveService.Unarchive(context.Background(), archivePath, extractedDir)
	require.NoError(t, err)

	// Test cleanup function
	err = cleanup()
	require.NoError(t, err)

	// Verify temp directory is deleted
	_, err = os.Stat(filepath.Join(tempDir, "temp", fmt.Sprintf("%s_%s", config.TmpDir, "test_backup.zip")))
	assert.Error(t, err, "Temp directory should be deleted")

	// Verify archive file is deleted
	_, err = os.Stat(filepath.Join(tempDir, archivePath))
	assert.Error(t, err, "Archive file should be deleted")

	files, err := fs.List(context.Background(), extractedDir)
	require.NoError(t, err)
	log.Println("List of files in extracted:", files)

	err = worldsCompareFunc(filepath.Join(tempDir, extractedDir))
	if err != nil {
		t.Fatalf("Failed to compare archive: %v", err)
	}
}
