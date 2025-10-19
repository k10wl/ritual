package services

import (
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

func TestBackupperService_Run_HappyScenario(t *testing.T) {
	// Use real filesystem operations to verify backup orchestration completion
	tempDir, _ := setupRitualDirectory(t)
	fs, err := adapters.NewFSRepository(tempDir)
	require.NoError(t, err)
	defer fs.Close()

	archivePath := filepath.Join(tempDir, "test-archive.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Failed to create archive file: %v", err)
	}
	file.WriteString("test archive content")
	file.Close()

	archiveService, err := NewArchiveService(tempDir)
	require.NoError(t, err)

	archivePath, cleanup, err := ArchivePaperWorld(
		context.Background(),
		fs,
		archiveService,
		"",
		tempDir,
		"test_backup",
	)
	require.NoError(t, err)
	require.NotNil(t, archivePath)
	require.NotNil(t, cleanup)

	backupper, err := NewBackupperService(
		fs,
		func() (string, func() error, error) { return archivePath, cleanup, nil },
		func(ports.StorageRepository) ([]domain.World, error) { return []domain.World{}, nil },
	)
	require.NoError(t, err)
	require.NotNil(t, backupper)

	// Execute backup orchestration
	cleanupFunc, err := backupper.Run()
	if err != nil {
		t.Errorf("Run() returned error: %v", err)
	}
	if cleanupFunc == nil {
		t.Error("Run() returned nil cleanup function")
	}

	// Verify filesystem state after successful run
	// Archive should be processed and stored in backup location with timestamp format
	// Look for files matching pattern {unixtimestamp}.zip
	files, err := fs.List(context.Background(), "")
	if err != nil {
		t.Errorf("Failed to list files after successful run: %v", err)
	}

	var foundBackup bool
	for _, file := range files {
		if len(file) > 4 && file[len(file)-4:] == ".zip" {
			// Check if filename is numeric timestamp + .zip
			timestamp := file[:len(file)-4]
			if len(timestamp) == 10 { // Unix timestamp length
				foundBackup = true
				break
			}
		}
	}

	if !foundBackup {
		t.Error("No backup file found with timestamp format")
	}

	// Find the backup file for verification
	var backupFile string
	for _, file := range files {
		if len(file) > 4 && file[len(file)-4:] == ".zip" {
			timestamp := file[:len(file)-4]
			if len(timestamp) == 10 {
				backupFile = file
				break
			}
		}
	}
	require.NotEmpty(t, backupFile, "Backup file should exist")
}
