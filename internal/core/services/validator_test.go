package services

import (
	"ritual/internal/core/domain"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewValidatorService(t *testing.T) {

	t.Run("valid_constructor", func(t *testing.T) {
		validator, err := NewValidatorService()

		assert.NoError(t, err)
		assert.NotNil(t, validator)
	})
}

func TestValidatorService_CheckInstance(t *testing.T) {

	validator, err := NewValidatorService()
	assert.NoError(t, err)

	tests := []struct {
		name        string
		local       *domain.Manifest
		remote      *domain.Manifest
		expectedErr error
	}{
		{
			name: "valid_manifests",
			local: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			expectedErr: nil,
		},
		{
			name:        "nil_local_manifest",
			local:       nil,
			remote:      &domain.Manifest{InstanceVersion: "1.0.0", RitualVersion: "1.0.0"},
			expectedErr: ErrLocalManifestNil,
		},
		{
			name:        "nil_remote_manifest",
			local:       &domain.Manifest{InstanceVersion: "1.0.0", RitualVersion: "1.0.0"},
			remote:      nil,
			expectedErr: ErrRemoteManifestNil,
		},
		{
			name: "empty_local_instance_version",
			local: &domain.Manifest{
				InstanceVersion: "",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			expectedErr: ErrLocalInstanceVersionEmpty,
		},
		{
			name: "empty_remote_instance_version",
			local: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "",
				RitualVersion:   "1.0.0",
			},
			expectedErr: ErrRemoteInstanceVersionEmpty,
		},
		{
			name: "instance_version_mismatch",
			local: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "2.0.0",
				RitualVersion:   "1.0.0",
			},
			expectedErr: ErrOutdatedInstance,
		},
		{
			name: "whitespace_local_instance_version",
			local: &domain.Manifest{
				InstanceVersion: "   ",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			expectedErr: ErrLocalInstanceVersionEmpty,
		},
		{
			name: "whitespace_remote_instance_version",
			local: &domain.Manifest{
				InstanceVersion: "1.0.0",
				RitualVersion:   "1.0.0",
			},
			remote: &domain.Manifest{
				InstanceVersion: "   ",
				RitualVersion:   "1.0.0",
			},
			expectedErr: ErrRemoteInstanceVersionEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.CheckInstance(tt.local, tt.remote)

			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
}

func TestValidatorService_CheckWorld(t *testing.T) {

	validator, err := NewValidatorService()
	assert.NoError(t, err)

	validTime := time.Now()
	zeroTime := time.Time{}

	tests := []struct {
		name        string
		local       *domain.Manifest
		remote      *domain.Manifest
		expectedErr error
	}{
		{
			name: "valid_worlds",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
					{URI: "world2", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
					{URI: "world2", CreatedAt: validTime},
				},
			},
			expectedErr: nil,
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
			name: "no_local_worlds",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			expectedErr: ErrNoLocalWorlds,
		},
		{
			name: "empty_local_world_uri",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			expectedErr: ErrLocalWorldURIEmpty,
		},
		{
			name: "zero_local_world_timestamp",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: zeroTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			expectedErr: ErrLocalWorldTimestampZero,
		},
		{
			name: "empty_remote_world_uri",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "", CreatedAt: validTime},
				},
			},
			expectedErr: ErrRemoteWorldURIEmpty,
		},
		{
			name: "zero_remote_world_timestamp",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: zeroTime},
				},
			},
			expectedErr: ErrRemoteWorldTimestampZero,
		},
		{
			name: "whitespace_local_world_uri",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "   ", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			expectedErr: ErrLocalWorldURIEmpty,
		},
		{
			name: "whitespace_remote_world_uri",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "   ", CreatedAt: validTime},
				},
			},
			expectedErr: ErrRemoteWorldURIEmpty,
		},
		{
			name: "world_list_mismatch",
			local: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world1", CreatedAt: validTime},
				},
			},
			remote: &domain.Manifest{
				StoredWorlds: []domain.World{
					{URI: "world2", CreatedAt: validTime},
				},
			},
			expectedErr: ErrOutdatedWorld,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.CheckWorld(tt.local, tt.remote)

			if tt.expectedErr == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
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
				LockedBy: "user1__1234567890",
			},
			remote: &domain.Manifest{
				LockedBy: "user1__1234567890",
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
				LockedBy: "user1__1234567890",
			},
			remote: &domain.Manifest{
				LockedBy: "user2__1234567890",
			},
			expectedErr: ErrLockConflict,
		},
		{
			name: "remote_locked_local_unlocked",
			local: &domain.Manifest{
				LockedBy: "",
			},
			remote: &domain.Manifest{
				LockedBy: "user1__1234567890",
			},
			expectedErr: ErrLockConflict,
		},
		{
			name: "local_locked_remote_unlocked",
			local: &domain.Manifest{
				LockedBy: "user1__1234567890",
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

func TestValidatorService_DefensiveValidation(t *testing.T) {

	_, err := NewValidatorService()
	assert.NoError(t, err)

	t.Run("nil_validator_check_instance", func(t *testing.T) {
		var nilValidator *ValidatorService

		err := nilValidator.CheckInstance(&domain.Manifest{}, &domain.Manifest{})
		assert.Error(t, err)
	})

	t.Run("nil_validator_check_world", func(t *testing.T) {
		var nilValidator *ValidatorService

		err := nilValidator.CheckWorld(&domain.Manifest{}, &domain.Manifest{})
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

func TestValidatorService_CheckWorldBounds(t *testing.T) {
	validator, err := NewValidatorService()
	assert.NoError(t, err)

	t.Run("empty_local_worlds_bounds", func(t *testing.T) {
		local := &domain.Manifest{StoredWorlds: []domain.World{}}
		remote := &domain.Manifest{StoredWorlds: []domain.World{{URI: "world1", CreatedAt: time.Now()}}}

		err := validator.CheckWorld(local, remote)
		assert.Error(t, err)
		assert.Equal(t, ErrNoLocalWorlds, err)
	})

	t.Run("empty_remote_worlds_bounds", func(t *testing.T) {
		local := &domain.Manifest{StoredWorlds: []domain.World{{URI: "world1", CreatedAt: time.Now()}}}
		remote := &domain.Manifest{StoredWorlds: []domain.World{}}

		err := validator.CheckWorld(local, remote)
		assert.NoError(t, err, "Empty remote worlds should be allowed")
	})
}
