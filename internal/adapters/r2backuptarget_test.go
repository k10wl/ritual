package adapters

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewR2BackupTarget(t *testing.T) {
	// Create mock storage for testing
	mockStorage := &mocks.MockStorageRepository{}

	tests := []struct {
		name          string
		storage       ports.StorageRepository
		wantError     bool
		errorContains string
	}{
		{
			name:      "valid configuration",
			storage:   mockStorage,
			wantError: false,
		},
		{
			name:          "nil storage",
			storage:       nil,
			wantError:     true,
			errorContains: "storage repository cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := NewR2BackupTarget(tt.storage, context.Background())

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, target)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, target)
				assert.Equal(t, tt.storage, target.storage)
			}
		})
	}
}

func TestR2BackupTarget_Backup(t *testing.T) {
	// Setup
	mockStorage := &mocks.MockStorageRepository{}
	target, err := NewR2BackupTarget(mockStorage, context.Background())
	require.NoError(t, err)

	tests := []struct {
		name          string
		data          []byte
		wantError     bool
		errorContains string
		setupMocks    func()
	}{
		{
			name:      "valid backup data",
			data:      []byte("test backup data"),
			wantError: false,
			setupMocks: func() {
				mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
					assert.Equal(t, []byte("test backup data"), data)
					assert.Contains(t, key, "world_backups/")
					return nil
				}
			},
		},
		{
			name:          "nil data",
			data:          nil,
			wantError:     true,
			errorContains: "backup data cannot be nil",
			setupMocks:    func() {},
		},
		{
			name:          "empty data",
			data:          []byte{},
			wantError:     true,
			errorContains: "backup data cannot be empty",
			setupMocks:    func() {},
		},
		{
			name:          "storage put error",
			data:          []byte("test backup data"),
			wantError:     true,
			errorContains: "failed to store backup file",
			setupMocks: func() {
				mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
					return errors.New("storage error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMocks()
			err := target.Backup(tt.data)

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestR2BackupTarget_DataRetention(t *testing.T) {
	// Setup
	mockStorage := &mocks.MockStorageRepository{}
	target, err := NewR2BackupTarget(mockStorage, context.Background())
	require.NoError(t, err)

	t.Run("no retention needed when under limit", func(t *testing.T) {
		// Create 3 backup files (under limit of 5)
		mockFiles := []string{}
		for i := 0; i < 3; i++ {
			timestamp := time.Now().Add(-time.Duration(i) * time.Hour).Format("20060102150405")
			mockFiles = append(mockFiles, fmt.Sprintf("world_backups/%s.zip", timestamp))
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		deleteCalled := false
		mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
			deleteCalled = true
			return nil
		}

		err := target.DataRetention()
		assert.NoError(t, err)
		assert.False(t, deleteCalled, "No files should be deleted when under limit")
	})

	t.Run("removes excess backups", func(t *testing.T) {
		// Create 8 backup files (exceeds limit of 5)
		mockFiles := []string{}
		for i := 0; i < 8; i++ {
			timestamp := time.Now().Add(-time.Duration(i) * time.Hour).Format("20060102150405")
			mockFiles = append(mockFiles, fmt.Sprintf("world_backups/%s.zip", timestamp))
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		deletedFiles := []string{}
		mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
			deletedFiles = append(deletedFiles, key)
			return nil
		}

		err := target.DataRetention()
		assert.NoError(t, err)
		assert.Equal(t, 3, len(deletedFiles), "Expected 3 files to be deleted (8 - 5 limit)")
	})

	t.Run("deletes invalid timestamp files immediately", func(t *testing.T) {
		// Mix of valid and invalid timestamp files
		mockFiles := []string{
			"world_backups/20240101120000.zip", // valid (18 chars)
			"world_backups/invalid.zip",        // invalid (too short)
			"world_backups/20240102120000.zip", // valid (18 chars)
			"world_backups/notimestamp.zip",    // invalid (too short)
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		deletedFiles := []string{}
		mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
			deletedFiles = append(deletedFiles, key)
			return nil
		}

		err := target.DataRetention()
		assert.NoError(t, err)
		assert.Equal(t, 2, len(deletedFiles), "Expected 2 invalid files to be deleted immediately")
		assert.Contains(t, deletedFiles, "world_backups/invalid.zip")
		assert.Contains(t, deletedFiles, "world_backups/notimestamp.zip")
	})

	t.Run("storage list error", func(t *testing.T) {
		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return nil, errors.New("storage list error")
		}

		err := target.DataRetention()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get backup files")
	})

	t.Run("storage delete error during retention", func(t *testing.T) {
		// Create 8 backup files (exceeds limit of 5)
		mockFiles := []string{}
		for i := 0; i < 8; i++ {
			timestamp := time.Now().Add(-time.Duration(i) * time.Hour).Format("20060102150405")
			mockFiles = append(mockFiles, fmt.Sprintf("world_backups/%s.zip", timestamp))
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
			return errors.New("delete error")
		}

		err := target.DataRetention()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove backup file")
	})

	t.Run("storage delete error during invalid file cleanup", func(t *testing.T) {
		mockFiles := []string{
			"world_backups/invalid.zip", // invalid timestamp (too short)
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
			return errors.New("delete error")
		}

		err := target.DataRetention()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete invalid backup file")
	})
}

func TestR2BackupTarget_AlwaysBackup(t *testing.T) {
	mockStorage := &mocks.MockStorageRepository{}
	target, err := NewR2BackupTarget(mockStorage, context.Background())
	require.NoError(t, err)

	t.Run("always creates backup regardless of existing files", func(t *testing.T) {
		// Create existing backup files
		currentTime := time.Now()
		currentMonthTimestamp := currentTime.Format("20060102150405")
		mockFiles := []string{
			fmt.Sprintf("world_backups/%s.zip", currentMonthTimestamp),
		}

		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return mockFiles, nil
		}

		putCalled := false
		mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
			putCalled = true
			assert.Contains(t, key, "world_backups/")
			return nil
		}

		err := target.Backup([]byte("test data"))
		assert.NoError(t, err)
		assert.True(t, putCalled, "R2 backup should always create backup regardless of existing files")
	})

	t.Run("creates backup when no existing backups", func(t *testing.T) {
		mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
			return []string{}, nil
		}

		putCalled := false
		mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
			putCalled = true
			assert.Contains(t, key, "world_backups/")
			return nil
		}

		err := target.Backup([]byte("test data"))
		assert.NoError(t, err)
		assert.True(t, putCalled, "Backup should be created when no existing backups")
	})
}

func TestR2BackupTarget_InterfaceCompliance(t *testing.T) {
	mockStorage := &mocks.MockStorageRepository{}
	target, err := NewR2BackupTarget(mockStorage, context.Background())
	require.NoError(t, err)

	// Verify interface compliance
	var _ ports.BackupTarget = target
}

func TestR2BackupTarget_NilReceiver(t *testing.T) {
	var target *R2BackupTarget

	t.Run("Backup with nil receiver", func(t *testing.T) {
		err := target.Backup([]byte("test"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "R2 backup target cannot be nil")
	})

	t.Run("DataRetention with nil receiver", func(t *testing.T) {
		err := target.DataRetention()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "R2 backup target cannot be nil")
	})
}
