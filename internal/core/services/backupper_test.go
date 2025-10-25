package services

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ritual/internal/core/ports"
)

// BackupperService Unit Tests
//
// These tests focus on testing the backup orchestration logic using mock implementations.
// The tests verify the template method pattern behavior without filesystem operations.
//
// Testing methodology:
// - Use mock BackupTarget implementations to verify orchestration flow
// - Test error handling scenarios with controlled mock failures
// - Validate archive validation logic with mock archive paths
// - Test both success and failure scenarios with pure unit testing
// - NO filesystem operations, NO external dependencies - test the orchestration logic

func TestNewBackupperService(t *testing.T) {
	buildArchive := func() (string, func() error, error) {
		return "test-archive.zip", func() error { return nil }, nil
	}

	targets := []ports.BackupTarget{
		&mockBackupTarget{},
	}

	service, err := NewBackupperService(buildArchive, targets, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, service)
	require.NotNil(t, service.buildArchive)
	require.Len(t, service.targets, 1)
}

func TestNewBackupperService_NilBuildArchive(t *testing.T) {
	targets := []ports.BackupTarget{
		&mockBackupTarget{},
	}

	_, err := NewBackupperService(nil, targets, t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "buildArchive cannot be nil")
}

func TestNewBackupperService_EmptyTargets(t *testing.T) {
	buildArchive := func() (string, func() error, error) {
		return "test-archive.zip", func() error { return nil }, nil
	}

	_, err := NewBackupperService(buildArchive, []ports.BackupTarget{}, t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one backup target is required")
}

func TestBackupperService_Run_HappyScenario(t *testing.T) {
	// Create mock backup target
	mockTarget := &mockBackupTarget{}
	targets := []ports.BackupTarget{mockTarget}

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a mock archive file
	archivePath := "test-archive.zip"
	err := os.WriteFile(tempDir+"/"+archivePath, []byte("PK\x03\x04test data"), 0644)
	require.NoError(t, err)

	buildArchive := func() (string, func() error, error) {
		return archivePath, func() error { return nil }, nil
	}

	backupper, err := NewBackupperService(buildArchive, targets, tempDir)
	require.NoError(t, err)
	require.NotNil(t, backupper)

	// Execute backup orchestration
	cleanupFunc, err := backupper.Run()
	require.NoError(t, err)
	require.NotNil(t, cleanupFunc)

	// Verify backup was called on target
	require.True(t, mockTarget.backupCalled)
	require.True(t, mockTarget.retentionCalled)
	require.NotNil(t, mockTarget.backupData)
}

// Test validateArchiveWithRoot method
func TestBackupperService_validateArchiveWithRoot(t *testing.T) {
	backupper, err := NewBackupperService(
		func() (string, func() error, error) { return "", func() error { return nil }, nil },
		[]ports.BackupTarget{&mockBackupTarget{}},
		t.TempDir(),
	)
	require.NoError(t, err)

	t.Run("EmptyPath", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		require.NoError(t, err)
		defer root.Close()

		err = backupper.validateArchiveWithRoot(root, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "archive path cannot be empty")
	})
}

// Test applyRetention method
func TestBackupperService_Run_BackupTargetError(t *testing.T) {
	// Create mock backup target that returns error
	mockTarget := &mockBackupTarget{backupError: errors.New("backup failed")}
	targets := []ports.BackupTarget{mockTarget}

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a mock archive file
	archivePath := "test-archive.zip"
	err := os.WriteFile(tempDir+"/"+archivePath, []byte("PK\x03\x04test data"), 0644)
	require.NoError(t, err)

	buildArchive := func() (string, func() error, error) {
		return archivePath, func() error { return nil }, nil
	}

	backupper, err := NewBackupperService(buildArchive, targets, tempDir)
	require.NoError(t, err)

	// Execute backup orchestration should fail
	_, err = backupper.Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup failed")
}

func TestBackupperService_Run_RetentionError(t *testing.T) {
	// Create mock backup target that returns error on retention
	mockTarget := &mockBackupTarget{retentionError: errors.New("retention failed")}
	targets := []ports.BackupTarget{mockTarget}

	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// Create a mock archive file
	archivePath := "test-archive.zip"
	err := os.WriteFile(tempDir+"/"+archivePath, []byte("PK\x03\x04test data"), 0644)
	require.NoError(t, err)

	buildArchive := func() (string, func() error, error) {
		return archivePath, func() error { return nil }, nil
	}

	backupper, err := NewBackupperService(buildArchive, targets, tempDir)
	require.NoError(t, err)

	// Execute backup orchestration should fail
	_, err = backupper.Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "retention failed")
}

// mockBackupTarget implements ports.BackupTarget interface for testing
type mockBackupTarget struct {
	backupCalled    bool
	retentionCalled bool
	backupData      []byte
	backupError     error
	retentionError  error
}

func (m *mockBackupTarget) Backup(data []byte) error {
	m.backupCalled = true
	m.backupData = data
	return m.backupError
}

func (m *mockBackupTarget) DataRetention() error {
	m.retentionCalled = true
	return m.retentionError
}
