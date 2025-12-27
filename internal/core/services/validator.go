package services

import (
	"errors"
	"ritual/internal/core/domain"
	"strings"
)

// Validation error constants
var (
	ErrOutdatedManifest              = errors.New("outdated manifest")
	ErrOutdatedInstance              = errors.New("outdated instance")
	ErrOutdatedWorld                 = errors.New("outdated world")
	ErrLocalManifestNil              = errors.New("local manifest cannot be nil")
	ErrRemoteManifestNil             = errors.New("remote manifest cannot be nil")
	ErrRemoteManifestVersionEmpty    = errors.New("remote manifest version cannot be empty")
	ErrLocalInstanceVersionEmpty     = errors.New("local manifest instance version cannot be empty")
	ErrRemoteInstanceVersionEmpty    = errors.New("remote manifest instance version cannot be empty")
	ErrNoLocalWorlds                 = errors.New("local manifest has no stored worlds")
	ErrNoRemoteWorlds                = errors.New("remote manifest has no stored worlds")
	ErrLocalWorldURIEmpty            = errors.New("local world URI cannot be empty")
	ErrLocalWorldTimestampZero       = errors.New("local world created timestamp cannot be zero")
	ErrRemoteWorldURIEmpty           = errors.New("remote world URI cannot be empty")
	ErrRemoteWorldTimestampZero      = errors.New("remote world created timestamp cannot be zero")
	ErrLockConflict                  = errors.New("lock conflict")
	ErrValidatorInitializationFailed = errors.New("validator initialization failed")
)

// ValidatorService implements validation logic for manifest integrity
// Validator ensures instance integrity and validates data consistency
type ValidatorService struct{}

// NewValidatorService creates a new ValidatorService instance
func NewValidatorService() (*ValidatorService, error) {
	validator := &ValidatorService{}

	// Postcondition assertion (NASA JPL Rule 2)
	if validator == nil {
		return nil, ErrValidatorInitializationFailed
	}

	return validator, nil
}

// CheckManifestVersion validates manifest version using semantic comparison
// Returns ErrOutdatedManifest if local version is older than remote
// Empty local version is considered outdated (legacy instance)
// Empty remote version is an error (broken world view)
func (v *ValidatorService) CheckManifestVersion(local *domain.Manifest, remote *domain.Manifest) error {
	if v == nil {
		return errors.New("validator service cannot be nil")
	}
	if local == nil {
		return ErrLocalManifestNil
	}
	if remote == nil {
		return ErrRemoteManifestNil
	}

	// Remote must always have a manifest version
	if strings.TrimSpace(remote.ManifestVersion) == "" {
		return ErrRemoteManifestVersionEmpty
	}

	// Empty local version means legacy instance needs update
	if strings.TrimSpace(local.ManifestVersion) == "" {
		return ErrOutdatedManifest
	}

	// Semantic version comparison: local older than remote triggers update
	if IsVersionOlder(local.ManifestVersion, remote.ManifestVersion) {
		return ErrOutdatedManifest
	}

	return nil
}

// CheckInstance validates manifest structure and content
func (v *ValidatorService) CheckInstance(local *domain.Manifest, remote *domain.Manifest) error {
	if v == nil {
		return errors.New("validator service cannot be nil")
	}
	if local == nil {
		return ErrLocalManifestNil
	}
	if remote == nil {
		return ErrRemoteManifestNil
	}

	if strings.TrimSpace(local.InstanceVersion) == "" {
		return ErrLocalInstanceVersionEmpty
	}
	if strings.TrimSpace(remote.InstanceVersion) == "" {
		return ErrRemoteInstanceVersionEmpty
	}

	if local.InstanceVersion != remote.InstanceVersion {
		return ErrOutdatedInstance
	}

	return nil
}

// CheckWorld validates world data integrity
func (v *ValidatorService) CheckWorld(local *domain.Manifest, remote *domain.Manifest) error {
	if v == nil {
		return errors.New("validator service cannot be nil")
	}
	if local == nil {
		return ErrLocalManifestNil
	}
	if remote == nil {
		return ErrRemoteManifestNil
	}

	// Skip world validation if remote has no worlds - allow launching without worlds
	if len(remote.Backups) == 0 {
		return nil
	}

	if len(local.Backups) == 0 {
		return ErrNoLocalWorlds
	}

	for _, world := range local.Backups {
		if strings.TrimSpace(world.URI) == "" {
			return ErrLocalWorldURIEmpty
		}
		if world.CreatedAt.IsZero() {
			return ErrLocalWorldTimestampZero
		}
	}

	for _, world := range remote.Backups {
		if strings.TrimSpace(world.URI) == "" {
			return ErrRemoteWorldURIEmpty
		}
		if world.CreatedAt.IsZero() {
			return ErrRemoteWorldTimestampZero
		}
	}

	// Safe comparison of last world only
	if remote.Backups[len(remote.Backups)-1] !=
		local.Backups[len(local.Backups)-1] {
		return ErrOutdatedWorld
	}

	return nil
}

// CheckLock validates lock mechanism compliance
func (v *ValidatorService) CheckLock(local *domain.Manifest, remote *domain.Manifest) error {
	if v == nil {
		return errors.New("validator service cannot be nil")
	}
	if local == nil {
		return ErrLocalManifestNil
	}
	if remote == nil {
		return ErrRemoteManifestNil
	}

	if local.IsLocked() || remote.IsLocked() {
		return ErrLockConflict
	}

	return nil
}
