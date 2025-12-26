package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSystemInfoProvider is a mock implementation of SystemInfoProvider for testing
type mockSystemInfoProvider struct {
	freeRAMMB int
	err       error
}

func (m *mockSystemInfoProvider) GetFreeRAMMB() (int, error) {
	return m.freeRAMMB, m.err
}

func TestNewRAMCondition(t *testing.T) {
	t.Run("nil system info provider returns error", func(t *testing.T) {
		_, err := NewRAMCondition(4096, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "system info provider cannot be nil")
	})

	t.Run("zero min RAM returns error", func(t *testing.T) {
		provider := &mockSystemInfoProvider{}
		_, err := NewRAMCondition(0, provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min RAM must be positive")
	})

	t.Run("negative min RAM returns error", func(t *testing.T) {
		provider := &mockSystemInfoProvider{}
		_, err := NewRAMCondition(-1, provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min RAM must be positive")
	})

	t.Run("valid parameters returns condition", func(t *testing.T) {
		provider := &mockSystemInfoProvider{}
		condition, err := NewRAMCondition(4096, provider)
		assert.NoError(t, err)
		assert.NotNil(t, condition)
	})
}

func TestRAMCondition_Check(t *testing.T) {
	tests := []struct {
		name        string
		minRAMMB    int
		freeRAMMB   int
		providerErr error
		wantErr     bool
		errContains string
	}{
		{
			name:      "sufficient RAM passes",
			minRAMMB:  4096,
			freeRAMMB: 8192,
			wantErr:   false,
		},
		{
			name:      "exact match passes",
			minRAMMB:  4096,
			freeRAMMB: 4096,
			wantErr:   false,
		},
		{
			name:        "insufficient RAM fails",
			minRAMMB:    4096,
			freeRAMMB:   2048,
			wantErr:     true,
			errContains: "insufficient RAM",
		},
		{
			name:        "provider error",
			minRAMMB:    4096,
			providerErr: errors.New("failed to get memory info"),
			wantErr:     true,
			errContains: "failed to get free RAM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockSystemInfoProvider{
				freeRAMMB: tt.freeRAMMB,
				err:       tt.providerErr,
			}
			condition, err := NewRAMCondition(tt.minRAMMB, provider)
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

func TestRAMCondition_Check_DefensiveValidation(t *testing.T) {
	t.Run("nil receiver returns error", func(t *testing.T) {
		var condition *RAMCondition
		err := condition.Check(context.Background())
		assert.Error(t, err)
		assert.Equal(t, ErrRAMConditionNil, err)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		provider := &mockSystemInfoProvider{freeRAMMB: 8192}
		condition, err := NewRAMCondition(4096, provider)
		require.NoError(t, err)

		err = condition.Check(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrRAMConditionCtxNil, err)
	})
}
