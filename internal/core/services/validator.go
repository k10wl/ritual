package services

import (
	"errors"
	"ritual/internal/core/domain"
	"strings"
)

// Validation error constants
var (
	ErrOutdatedManifest           = errors.New("outdated manifest")
	ErrLocalManifestNil           = errors.New("local manifest cannot be nil")
	ErrRemoteManifestNil          = errors.New("remote manifest cannot be nil")
	ErrRemoteManifestVersionEmpty = errors.New("remote manifest version cannot be empty")
	ErrLockConflict               = errors.New("lock conflict")
)

// ValidatorService implements validation logic for manifest integrity
// Validator ensures instance integrity and validates data consistency
type ValidatorService struct{}

// NewValidatorService creates a new ValidatorService instance
func NewValidatorService() (*ValidatorService, error) {
	return &ValidatorService{}, nil
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
