package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockJavaVersionProvider is a mock implementation of JavaVersionProvider for testing
type mockJavaVersionProvider struct {
	version int
	err     error
}

func (m *mockJavaVersionProvider) GetJavaVersion() (int, error) {
	return m.version, m.err
}

func TestNewJavaVersionCondition(t *testing.T) {
	t.Run("nil java info provider returns error", func(t *testing.T) {
		_, err := NewJavaVersionCondition(21, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Java info provider cannot be nil")
	})

	t.Run("zero min version returns error", func(t *testing.T) {
		provider := &mockJavaVersionProvider{}
		_, err := NewJavaVersionCondition(0, provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min Java version must be positive")
	})

	t.Run("negative min version returns error", func(t *testing.T) {
		provider := &mockJavaVersionProvider{}
		_, err := NewJavaVersionCondition(-1, provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "min Java version must be positive")
	})

	t.Run("valid parameters returns condition", func(t *testing.T) {
		provider := &mockJavaVersionProvider{}
		condition, err := NewJavaVersionCondition(21, provider)
		assert.NoError(t, err)
		assert.NotNil(t, condition)
	})
}

func TestJavaVersionCondition_Check(t *testing.T) {
	tests := []struct {
		name        string
		minVersion  int
		version     int
		providerErr error
		wantErr     bool
		errContains string
	}{
		{
			name:       "newer version passes",
			minVersion: 21,
			version:    22,
			wantErr:    false,
		},
		{
			name:       "exact match passes",
			minVersion: 21,
			version:    21,
			wantErr:    false,
		},
		{
			name:        "older version fails",
			minVersion:  21,
			version:     17,
			wantErr:     true,
			errContains: "Java version too old",
		},
		{
			name:        "Java not found",
			minVersion:  21,
			providerErr: errors.New("java not in PATH"),
			wantErr:     true,
			errContains: "Java not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockJavaVersionProvider{
				version: tt.version,
				err:     tt.providerErr,
			}
			condition, err := NewJavaVersionCondition(tt.minVersion, provider)
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

func TestJavaVersionCondition_Check_DefensiveValidation(t *testing.T) {
	t.Run("nil receiver returns error", func(t *testing.T) {
		var condition *JavaVersionCondition
		err := condition.Check(context.Background())
		assert.Error(t, err)
		assert.Equal(t, ErrJavaConditionNil, err)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		provider := &mockJavaVersionProvider{version: 21}
		condition, err := NewJavaVersionCondition(21, provider)
		require.NoError(t, err)

		err = condition.Check(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrJavaConditionCtxNil, err)
	})
}
