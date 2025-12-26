package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDiskInfoProvider is a mock implementation of DiskInfoProvider for testing
type mockDiskInfoProvider struct {
	freeDiskMB int
	err        error
}

func (m *mockDiskInfoProvider) GetFreeDiskMB(path string) (int, error) {
	return m.freeDiskMB, m.err
}

func TestNewDiskSpaceCondition(t *testing.T) {
	t.Run("nil disk info provider returns error", func(t *testing.T) {
		_, err := NewDiskSpaceCondition(5120, "C:\\", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disk info provider cannot be nil")
	})

	t.Run("zero min disk returns error", func(t *testing.T) {
		provider := &mockDiskInfoProvider{}
		_, err := NewDiskSpaceCondition(0, "C:\\", provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min disk space must be positive")
	})

	t.Run("negative min disk returns error", func(t *testing.T) {
		provider := &mockDiskInfoProvider{}
		_, err := NewDiskSpaceCondition(-1, "C:\\", provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min disk space must be positive")
	})

	t.Run("empty path returns error", func(t *testing.T) {
		provider := &mockDiskInfoProvider{}
		_, err := NewDiskSpaceCondition(5120, "", provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})

	t.Run("valid parameters returns condition", func(t *testing.T) {
		provider := &mockDiskInfoProvider{}
		condition, err := NewDiskSpaceCondition(5120, "C:\\", provider)
		assert.NoError(t, err)
		assert.NotNil(t, condition)
	})
}

func TestDiskSpaceCondition_Check(t *testing.T) {
	tests := []struct {
		name        string
		minDiskMB   int
		freeDiskMB  int
		providerErr error
		wantErr     bool
		errContains string
	}{
		{
			name:       "sufficient disk space passes",
			minDiskMB:  5120,
			freeDiskMB: 10240,
			wantErr:    false,
		},
		{
			name:       "exact match passes",
			minDiskMB:  5120,
			freeDiskMB: 5120,
			wantErr:    false,
		},
		{
			name:        "insufficient disk space fails",
			minDiskMB:   5120,
			freeDiskMB:  1024,
			wantErr:     true,
			errContains: "insufficient disk space",
		},
		{
			name:        "provider error",
			minDiskMB:   5120,
			providerErr: errors.New("failed to get disk info"),
			wantErr:     true,
			errContains: "failed to get free disk space",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockDiskInfoProvider{
				freeDiskMB: tt.freeDiskMB,
				err:        tt.providerErr,
			}
			condition, err := NewDiskSpaceCondition(tt.minDiskMB, "C:\\ritual", provider)
			require.NoError(t, err)

			err = condition.Check(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDiskSpaceCondition_Check_DefensiveValidation(t *testing.T) {
	t.Run("nil receiver returns error", func(t *testing.T) {
		var condition *DiskSpaceCondition
		err := condition.Check(context.Background())
		assert.Error(t, err)
		assert.Equal(t, ErrDiskConditionNil, err)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		provider := &mockDiskInfoProvider{freeDiskMB: 10240}
		condition, err := NewDiskSpaceCondition(5120, "C:\\", provider)
		require.NoError(t, err)

		err = condition.Check(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrDiskConditionCtxNil, err)
	})
}
