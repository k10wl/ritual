package services

import (
	"ritual/internal/core/domain"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewValidatorService(t *testing.T) {
	t.Run("valid_constructor", func(t *testing.T) {
		validator, err := NewValidatorService()

		assert.NoError(t, err)
		assert.NotNil(t, validator)
	})
}

func TestValidatorService_CheckLock(t *testing.T) {
	validator, err := NewValidatorService()
	assert.NoError(t, err)

	tests := []struct {
		name        string
		local       *domain.Manifest
		remote      *domain.Manifest
		expectedErr error
	}{
		{
			name: "both_unlocked",
			local: &domain.Manifest{
				LockedBy: "",
			},
			remote: &domain.Manifest{
				LockedBy: "",
			},
			expectedErr: nil,
		},
		{
			name: "both_locked_by_same_entity",
			local: &domain.Manifest{
				LockedBy: "user1::1234567890",
			},
			remote: &domain.Manifest{
				LockedBy: "user1::1234567890",
			},
			expectedErr: ErrLockConflict,
		},
		{
			name:        "nil_local_manifest",
			local:       nil,
			remote:      &domain.Manifest{},
			expectedErr: ErrLocalManifestNil,
		},
		{
			name:        "nil_remote_manifest",
			local:       &domain.Manifest{},
			remote:      nil,
			expectedErr: ErrRemoteManifestNil,
		},
		{
			name: "lock_conflict",
			local: &domain.Manifest{
				LockedBy: "user1::1234567890",
			},
			remote: &domain.Manifest{
				LockedBy: "user2::1234567890",
			},
			expectedErr: ErrLockConflict,
		},
		{
			name: "remote_locked_local_unlocked",
			local: &domain.Manifest{
				LockedBy: "",
			},
			remote: &domain.Manifest{
				LockedBy: "user1::1234567890",
			},
			expectedErr: ErrLockConflict,
		},
		{
			name: "local_locked_remote_unlocked",
			local: &domain.Manifest{
				LockedBy: "user1::1234567890",
			},
			remote: &domain.Manifest{
				LockedBy: "",
			},
			expectedErr: ErrLockConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.CheckLock(tt.local, tt.remote)

			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
}

func TestValidatorService_CheckManifestVersion(t *testing.T) {
	validator, err := NewValidatorService()
	assert.NoError(t, err)

	tests := []struct {
		name        string
		local       *domain.Manifest
		remote      *domain.Manifest
		expectedErr error
	}{
		{
			name:        "matching_versions",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "1.0.0"},
			expectedErr: nil,
		},
		{
			name:        "local_newer_than_remote",
			local:       &domain.Manifest{ManifestVersion: "2.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "1.0.0"},
			expectedErr: nil,
		},
		{
			name:        "local_older_than_remote_major",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "2.0.0"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "local_older_than_remote_minor",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "1.1.0"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "local_older_than_remote_patch",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "1.0.1"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "empty_local_with_remote_version",
			local:       &domain.Manifest{ManifestVersion: ""},
			remote:      &domain.Manifest{ManifestVersion: "1.0.0"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "whitespace_local_with_remote_version",
			local:       &domain.Manifest{ManifestVersion: "   "},
			remote:      &domain.Manifest{ManifestVersion: "1.0.0"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "empty_remote_version_is_error",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: ""},
			expectedErr: ErrRemoteManifestVersionEmpty,
		},
		{
			name:        "whitespace_remote_version_is_error",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "   "},
			expectedErr: ErrRemoteManifestVersionEmpty,
		},
		{
			name:        "nil_local_manifest",
			local:       nil,
			remote:      &domain.Manifest{ManifestVersion: "1.0.0"},
			expectedErr: ErrLocalManifestNil,
		},
		{
			name:        "nil_remote_manifest",
			local:       &domain.Manifest{ManifestVersion: "1.0.0"},
			remote:      nil,
			expectedErr: ErrRemoteManifestNil,
		},
		{
			name:        "complex_version_local_older",
			local:       &domain.Manifest{ManifestVersion: "1.9.9"},
			remote:      &domain.Manifest{ManifestVersion: "2.0.0"},
			expectedErr: ErrOutdatedManifest,
		},
		{
			name:        "complex_version_local_newer",
			local:       &domain.Manifest{ManifestVersion: "2.0.0"},
			remote:      &domain.Manifest{ManifestVersion: "1.9.9"},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.CheckManifestVersion(tt.local, tt.remote)

			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
}

func TestValidatorService_DefensiveValidation(t *testing.T) {
	_, err := NewValidatorService()
	assert.NoError(t, err)

	t.Run("nil_validator_check_manifest_version", func(t *testing.T) {
		var nilValidator *ValidatorService

		err := nilValidator.CheckManifestVersion(&domain.Manifest{}, &domain.Manifest{})
		assert.Error(t, err)
	})

	t.Run("nil_validator_check_lock", func(t *testing.T) {
		var nilValidator *ValidatorService

		err := nilValidator.CheckLock(&domain.Manifest{}, &domain.Manifest{})
		assert.Error(t, err)
	})
}

func TestValidatorService_ConstructorError(t *testing.T) {
	t.Run("constructor_success", func(t *testing.T) {
		validator, err := NewValidatorService()
		assert.NoError(t, err)
		assert.NotNil(t, validator)
	})
}
