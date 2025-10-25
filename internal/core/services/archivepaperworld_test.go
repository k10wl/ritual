package services_test

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchivePaperWorld(t *testing.T) {
	tempDir := t.TempDir()

	fs, err := adapters.NewFSRepository(tempDir)
	require.NoError(t, err)
	defer fs.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	// Create temporary directory structure mimicking Minecraft world
	_, _, worldsCompareFunc, err := testhelpers.PaperMinecraftWorldSetup(instanceDir)
	require.NoError(t, err)

	log.Println(fs.List(context.Background(), "instance"))

	archiveService, err := services.NewArchiveService(tempDir)
	require.NoError(t, err)

	// Execute ArchivePaperWorld
	ctx := context.Background()
	archivePath, cleanup, err := services.ArchivePaperWorld(
		ctx,
		fs,
		archiveService,
		"instance",
		"tmp",
		"test_backup",
	)

	// Verify results
	require.NoError(t, err)
	require.NotNil(t, archivePath)
	require.NotNil(t, cleanup)

	// Verify archive path format
	assert.Equal(t, archivePath, filepath.Join("tmp", "test_backup.zip"))

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

	files, err := fs.List(context.Background(), extractedDir)
	require.NoError(t, err)
	log.Println("List of files in extracted:", files)

	err = worldsCompareFunc(filepath.Join(tempDir, extractedDir))
	if err != nil {
		t.Fatalf("Failed to compare archive: %v", err)
	}
}
