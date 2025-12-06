package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"ritual/internal/core/ports"
)

// RitualUpdater error constants
var (
	ErrRitualUpdaterLibrarianNil = errors.New("librarian service cannot be nil")
	ErrRitualUpdaterStorageNil   = errors.New("storage repository cannot be nil")
	ErrRitualUpdaterNil          = errors.New("ritual updater cannot be nil")
	ErrRitualCtxNil              = errors.New("context cannot be nil")
	ErrRitualRemoteManifestNil   = errors.New("remote manifest cannot be nil")
	ErrRitualLocalManifestNil    = errors.New("local manifest cannot be nil")
)

// RitualUpdater constants
const (
	ritualBinaryKey = "ritual.exe"
)

// RitualUpdater implements UpdaterService for ritual self-updates
// Compares local and remote ritual versions and performs self-update if local is outdated
type RitualUpdater struct {
	librarian ports.LibrarianService
	storage   ports.StorageRepository
}

// Compile-time check to ensure RitualUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*RitualUpdater)(nil)

// NewRitualUpdater creates a new ritual updater
func NewRitualUpdater(
	librarian ports.LibrarianService,
	storage ports.StorageRepository,
) (*RitualUpdater, error) {
	if librarian == nil {
		return nil, ErrRitualUpdaterLibrarianNil
	}
	if storage == nil {
		return nil, ErrRitualUpdaterStorageNil
	}

	return &RitualUpdater{
		librarian: librarian,
		storage:   storage,
	}, nil
}

// Run executes the ritual self-update process
// Downloads new binary if local version is outdated, replaces current exe, and restarts
func (u *RitualUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrRitualUpdaterNil
	}
	if ctx == nil {
		return ErrRitualCtxNil
	}

	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest == nil {
		return ErrRitualRemoteManifestNil
	}

	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local manifest: %w", err)
	}
	if localManifest == nil {
		return ErrRitualLocalManifestNil
	}

	// No update needed if versions match
	if localManifest.RitualVersion == remoteManifest.RitualVersion {
		return nil
	}

	// Download new binary
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}

	data, err := u.storage.Get(ctx, ritualBinaryKey)
	if err != nil {
		return fmt.Errorf("failed to download ritual binary: %w", err)
	}

	// Overwrite current binary
	if err := os.WriteFile(currentExe, data, 0755); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	// Update local manifest with new version
	localManifest.RitualVersion = remoteManifest.RitualVersion
	if err := u.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	// Restart with same arguments
	cmd := exec.Command(currentExe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to restart: %w", err)
	}

	os.Exit(0)
	return nil
}
