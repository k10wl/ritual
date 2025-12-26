package services

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLockLibrarian is a mock implementation of LibrarianService for testing
type mockLockLibrarian struct {
	remoteManifest    *domain.Manifest
	remoteManifestErr error
}

func (m *mockLockLibrarian) GetLocalManifest(ctx context.Context) (*domain.Manifest, error) {
	return nil, nil
}

func (m *mockLockLibrarian) GetRemoteManifest(ctx context.Context) (*domain.Manifest, error) {
	return m.remoteManifest, m.remoteManifestErr
}

func (m *mockLockLibrarian) SaveLocalManifest(ctx context.Context, manifest *domain.Manifest) error {
	return nil
}

func (m *mockLockLibrarian) SaveRemoteManifest(ctx context.Context, manifest *domain.Manifest) error {
	return nil
}

func TestNewManifestLockCondition(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, err := NewManifestLockCondition(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrLockConditionLibrarianNil, err)
	})

	t.Run("valid librarian returns condition", func(t *testing.T) {
		librarian := &mockLockLibrarian{}
		condition, err := NewManifestLockCondition(librarian)
		assert.NoError(t, err)
		assert.NotNil(t, condition)
	})
}

func TestManifestLockCondition_Check(t *testing.T) {
	tests := []struct {
		name           string
		remoteManifest *domain.Manifest
		remoteErr      error
		wantErr        bool
		errContains    string
	}{
		{
			name:           "unlocked manifest passes",
			remoteManifest: &domain.Manifest{LockedBy: ""},
			wantErr:        false,
		},
		{
			name:           "locked manifest fails",
			remoteManifest: &domain.Manifest{LockedBy: "testhost::123456"},
			wantErr:        true,
			errContains:    "manifest is locked",
		},
		{
			name:        "remote manifest fetch error",
			remoteErr:   errors.New("network error"),
			wantErr:     true,
			errContains: "failed to get remote manifest",
		},
		{
			name:        "nil remote manifest",
			wantErr:     true,
			errContains: "remote manifest cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			librarian := &mockLockLibrarian{
				remoteManifest:    tt.remoteManifest,
				remoteManifestErr: tt.remoteErr,
			}
			condition, err := NewManifestLockCondition(librarian)
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

func TestManifestLockCondition_Check_DefensiveValidation(t *testing.T) {
	t.Run("nil receiver returns error", func(t *testing.T) {
		var condition *ManifestLockCondition
		err := condition.Check(context.Background())
		assert.Error(t, err)
		assert.Equal(t, ErrLockConditionNil, err)
	})

	t.Run("nil context returns error", func(t *testing.T) {
		librarian := &mockLockLibrarian{
			remoteManifest: &domain.Manifest{},
		}
		condition, err := NewManifestLockCondition(librarian)
		require.NoError(t, err)

		err = condition.Check(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrLockConditionCtxNil, err)
	})
}
