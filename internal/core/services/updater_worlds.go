package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
)

// WorldsUpdater error constants
var (
	ErrWorldsUpdaterLibrarianNil     = errors.New("librarian service cannot be nil")
	ErrWorldsUpdaterValidatorNil     = errors.New("validator service cannot be nil")
	ErrWorldsUpdaterLocalStorageNil  = errors.New("local storage repository cannot be nil")
	ErrWorldsUpdaterRemoteStorageNil = errors.New("remote storage repository cannot be nil")
	ErrWorldsUpdaterArchiveNil       = errors.New("archive service cannot be nil")
	ErrWorldsUpdaterWorkRootNil      = errors.New("workRoot cannot be nil")
	ErrWorldsUpdaterNil              = errors.New("worlds updater cannot be nil")
)

// WorldsUpdater implements UpdaterService for world updates
// WorldsUpdater handles downloading and extracting world archives from remote storage
type WorldsUpdater struct {
	librarian     ports.LibrarianService
	validator     ports.ValidatorService
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
	archive       ports.ArchiveService
	workRoot      *os.Root
}

// Compile-time check to ensure WorldsUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*WorldsUpdater)(nil)

// NewWorldsUpdater creates a new worlds updater
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewWorldsUpdater(
	librarian ports.LibrarianService,
	validator ports.ValidatorService,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	archive ports.ArchiveService,
	workRoot *os.Root,
) (*WorldsUpdater, error) {
	if librarian == nil {
		return nil, ErrWorldsUpdaterLibrarianNil
	}
	if validator == nil {
		return nil, ErrWorldsUpdaterValidatorNil
	}
	if localStorage == nil {
		return nil, ErrWorldsUpdaterLocalStorageNil
	}
	if remoteStorage == nil {
		return nil, ErrWorldsUpdaterRemoteStorageNil
	}
	if archive == nil {
		return nil, ErrWorldsUpdaterArchiveNil
	}
	if workRoot == nil {
		return nil, ErrWorldsUpdaterWorkRootNil
	}

	updater := &WorldsUpdater{
		librarian:     librarian,
		validator:     validator,
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
		archive:       archive,
		workRoot:      workRoot,
	}

	// Postcondition assertion
	if updater == nil {
		return nil, errors.New("worlds updater initialization failed")
	}

	return updater, nil
}

// Run executes the worlds update process
// Returns nil if no update needed or update succeeded, error if update failed
func (u *WorldsUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrWorldsUpdaterNil
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

	// Check if world update is needed
	if err := u.validator.CheckWorld(localManifest, remoteManifest); err != nil {
		// Both "outdated world" and "local manifest has no stored worlds" require update
		if err.Error() == "outdated world" || err.Error() == "local manifest has no stored worlds" {
			// Worlds need update
			if updateErr := u.updateWorlds(ctx, remoteManifest); updateErr != nil {
				return fmt.Errorf("failed to update worlds: %w", updateErr)
			}
		} else {
			return fmt.Errorf("world validation failed: %w", err)
		}
	}

	return nil
}

// updateWorlds downloads and extracts world archive
func (u *WorldsUpdater) updateWorlds(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	latestWorld := remoteManifest.GetLatestWorld()
	if latestWorld == nil {
		// No worlds available - skip download
		return nil
	}

	// Sanitize world URI
	sanitizedURI, err := u.sanitizeWorldURI(latestWorld.URI)
	if err != nil {
		return err
	}

	// Download world archive
	if err := u.downloadWorldArchive(ctx, sanitizedURI); err != nil {
		return err
	}

	// Extract world archive
	if err := u.extractWorldArchive(ctx, sanitizedURI); err != nil {
		return err
	}

	// Save updated local manifest
	if err := u.librarian.SaveLocalManifest(ctx, remoteManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	return nil
}

// sanitizeWorldURI validates and sanitizes the world URI
func (u *WorldsUpdater) sanitizeWorldURI(uri string) (string, error) {
	sanitizedURI := filepath.ToSlash(filepath.Clean(uri))
	if !strings.HasPrefix(sanitizedURI, config.RemoteBackups+"/") {
		return "", fmt.Errorf("invalid world URI: %s", sanitizedURI)
	}
	return sanitizedURI, nil
}

// downloadWorldArchive downloads the world archive from remote storage
func (u *WorldsUpdater) downloadWorldArchive(ctx context.Context, sanitizedURI string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if sanitizedURI == "" {
		return errors.New("sanitized URI cannot be empty")
	}

	// Download world archive from remote storage
	worldZipData, err := u.remoteStorage.Get(ctx, sanitizedURI)
	if err != nil {
		return fmt.Errorf("failed to get %s from remote storage: %w", sanitizedURI, err)
	}

	// Store in temporary location
	tempKey := filepath.Join(tempPrefix, sanitizedURI)
	err = u.localStorage.Put(ctx, tempKey, worldZipData)
	if err != nil {
		return fmt.Errorf("failed to store %s in temp storage: %w", sanitizedURI, err)
	}

	return nil
}

// extractWorldArchive extracts the world archive to the instance directory
func (u *WorldsUpdater) extractWorldArchive(ctx context.Context, sanitizedURI string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if sanitizedURI == "" {
		return errors.New("sanitized URI cannot be empty")
	}

	tempKey := filepath.Join(tempPrefix, sanitizedURI)

	// Extract archive to instance directory
	err := u.archive.Unarchive(ctx, tempKey, instanceDir)
	if err != nil {
		return fmt.Errorf("failed to extract worlds: %w", err)
	}

	// Cleanup temp file
	err = u.localStorage.Delete(ctx, tempKey)
	if err != nil {
		return fmt.Errorf("failed to cleanup %s: %w", sanitizedURI, err)
	}

	return nil
}
