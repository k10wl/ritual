package services

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/testhelpers"
)

// BackupperService Integration Tests
//
// These tests use real integration ports (FSRepository) with low-level verification
// to test the metal behavior of backup orchestration. No dependency injection or
// mocking - direct filesystem operations with raw file inspection.
//
// Testing methodology:
// - Use adapters.NewFSRepository() with t.TempDir() for real filesystem operations
// - Verify backup orchestration by inspecting raw filesystem state changes
// - Test actual file creation, modification, deletion operations at OS level
// - Validate archive timestamps, checksums, and retention policies through file metadata
// - Test both success and failure scenarios with real filesystem constraints
// - NO DI, NO mocks, NO abstraction - test the actual metal behavior

func TestNewBackupperService(t *testing.T) {
	// Use real filesystem for integration testing
	tempDir := t.TempDir()
	storage, err := adapters.NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FSRepository: %v", err)
	}
	defer storage.Close()

	buildArchive := func() (string, func() error, error) {
		return filepath.Join(tempDir, "test-archive.zip"), func() error { return nil }, nil
	}
	markForCleanup := func(storage ports.StorageRepository) ([]domain.World, error) {
		return []domain.World{}, nil
	}

	service, err := NewBackupperService(storage, buildArchive, markForCleanup)
	require.NoError(t, err)
	require.NotNil(t, service)
	require.Equal(t, storage, service.storage)
	require.NotNil(t, service.buildArchive)
	require.NotNil(t, service.markForCleanup)
}

func TestNewBackupperService_NilStorage(t *testing.T) {
	_, err := NewBackupperService(nil, func() (string, func() error, error) { return "", func() error { return nil }, nil }, func(ports.StorageRepository) ([]domain.World, error) { return []domain.World{}, nil })
	if err == nil {
		t.Error("Expected error for nil storage")
	}
}

func TestNewBackupperService_NilBuildArchive(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := adapters.NewFSRepository(tempDir)
	require.NoError(t, err)
	defer storage.Close()

	_, err = NewBackupperService(storage, nil, func(ports.StorageRepository) ([]domain.World, error) { return []domain.World{}, nil })
	require.Error(t, err)
}

func TestNewBackupperService_NilMarkForCleanup(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := adapters.NewFSRepository(tempDir)
	require.NoError(t, err)
	defer storage.Close()

	_, err = NewBackupperService(storage, func() (string, func() error, error) { return "", func() error { return nil }, nil }, nil)
	require.Error(t, err)
}

func setupRitualDirectory(t *testing.T) (string, func(string) error) {
	ritualDir := t.TempDir()
	instanceDir := filepath.Join(ritualDir, "instance")
	err := os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	// Use PaperMinecraftWorldSetup for comprehensive world structure
	_, _, compareFunc, err := testhelpers.PaperMinecraftWorldSetup(instanceDir)
	require.NoError(t, err)

	return ritualDir, compareFunc
}

func logDirectoryTree(t *testing.T, dirPath string, prefix string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		t.Logf("%s[ERROR: %v]", prefix, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			t.Logf("%s%s/", prefix, entry.Name())
			logDirectoryTree(t, filepath.Join(dirPath, entry.Name()), prefix+"  ")
		} else {
			t.Logf("%s%s", prefix, entry.Name())
		}
	}
}

func TestBackupperService_Run_HappyScenario(t *testing.T) {
	tempDir := t.TempDir()
	fs, err := adapters.NewFSRepository(tempDir)
	require.NoError(t, err)
	defer fs.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	// Setup Paper Minecraft world structure using testhelper
	_, _, _, err = testhelpers.PaperMinecraftWorldSetup(instanceDir)
	require.NoError(t, err)

	archiveService, err := NewArchiveService(tempDir)
	require.NoError(t, err)

	// Create archive using ArchivePaperWorld function
	archivePath, cleanup, err := ArchivePaperWorld(
		context.Background(),
		fs,
		archiveService,
		"instance",
		"tmp",
		"test_backup",
	)
	require.NoError(t, err)
	require.NotNil(t, archivePath)
	require.NotNil(t, cleanup)

	backupper, err := NewBackupperService(
		fs,
		func() (string, func() error, error) {
			return filepath.Join(tempDir, archivePath), cleanup, nil
		},
		func(ports.StorageRepository) ([]domain.World, error) { return []domain.World{}, nil },
	)
	require.NoError(t, err)
	require.NotNil(t, backupper)

	// Execute backup orchestration
	cleanupFunc, err := backupper.Run()
	require.NoError(t, err)
	require.NotNil(t, cleanupFunc)

	// Verify archive file exists and extract for verification
	files, err := fs.List(context.Background(), "")
	require.NoError(t, err)

	var backupFile string
	for _, file := range files {
		if len(file) > 4 && file[len(file)-4:] == ".zip" {
			timestamp := file[:len(file)-4]
			if len(timestamp) == 10 { // Unix timestamp length
				backupFile = file
				break
			}
		}
	}
	require.NotEmpty(t, backupFile, "Backup file should exist")

	// Extract archive using standard zip package for verification
	extractedDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractedDir, 0755)
	require.NoError(t, err)

	// Open zip file
	zipReader, err := zip.OpenReader(filepath.Join(tempDir, backupFile))
	require.NoError(t, err)
	defer zipReader.Close()

	// Extract all files
	for _, file := range zipReader.File {
		filePath := filepath.Join(extractedDir, file.Name)

		if file.FileInfo().IsDir() {
			err = os.MkdirAll(filePath, file.FileInfo().Mode())
			require.NoError(t, err)
			continue
		}

		err = os.MkdirAll(filepath.Dir(filePath), 0755)
		require.NoError(t, err)

		rc, err := file.Open()
		require.NoError(t, err)

		outFile, err := os.Create(filePath)
		require.NoError(t, err)

		_, err = outFile.ReadFrom(rc)
		require.NoError(t, err)

		outFile.Close()
		rc.Close()
	}

	// Log directory structures for debugging
	t.Log("=== ORIGINAL INSTANCE STRUCTURE ===")
	logDirectoryTree(t, filepath.Join(tempDir, "instance"), "")

	t.Log("=== EXTRACTED ARCHIVE STRUCTURE ===")
	logDirectoryTree(t, extractedDir, "")

	// Calculate checksums for original and extracted directories
	originalChecksum, err := testhelpers.DirectoryChecksum(filepath.Join(tempDir, "instance"))
	require.NoError(t, err)

	extractedChecksum, err := testhelpers.DirectoryChecksum(extractedDir)
	require.NoError(t, err)

	t.Logf("Original checksum: %s", originalChecksum)
	t.Logf("Extracted checksum: %s", extractedChecksum)

	// Verify checksums match
	require.Equal(t, originalChecksum, extractedChecksum, "Extracted archive content should match original world structure")
}
