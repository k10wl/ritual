package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
)

// RitualUpdater error constants
var (
	ErrRitualUpdaterLibrarianNil = errors.New("librarian service cannot be nil")
	ErrRitualUpdaterValidatorNil = errors.New("validator service cannot be nil")
	ErrRitualUpdaterNil          = errors.New("ritual updater cannot be nil")
	ErrRitualVersionMismatch     = errors.New("ritual version mismatch: self-update required")
)

// RitualUpdater implements UpdaterService for ritual version checks
// RitualUpdater compares local and remote ritual versions
// Currently returns error if versions don't match (placeholder for future self-update)
type RitualUpdater struct {
	librarian ports.LibrarianService
	validator ports.ValidatorService
}

// Compile-time check to ensure RitualUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*RitualUpdater)(nil)

// NewRitualUpdater creates a new ritual updater
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewRitualUpdater(
	librarian ports.LibrarianService,
	validator ports.ValidatorService,
) (*RitualUpdater, error) {
	if librarian == nil {
		return nil, ErrRitualUpdaterLibrarianNil
	}
	if validator == nil {
		return nil, ErrRitualUpdaterValidatorNil
	}

	updater := &RitualUpdater{
		librarian: librarian,
		validator: validator,
	}

	// Postcondition assertion
	if updater == nil {
		return nil, errors.New("ritual updater initialization failed")
	}

	return updater, nil
}

// Run executes the ritual version check
// Returns nil if versions match, error if they don't (placeholder for self-update)
func (u *RitualUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrRitualUpdaterNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Get remote manifest
	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	// Get local manifest
	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local manifest: %w", err)
	}
	if localManifest == nil {
		return errors.New("local manifest cannot be nil")
	}

	// Compare ritual versions
	if localManifest.RitualVersion != remoteManifest.RitualVersion {
		return ErrRitualVersionMismatch
	}

	return nil
}
