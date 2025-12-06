package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/adapters/streamer"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
)

// InstanceUpdater error constants
var (
	ErrInstanceUpdaterLibrarianNil     = errors.New("librarian service cannot be nil")
	ErrInstanceUpdaterValidatorNil     = errors.New("validator service cannot be nil")
	ErrInstanceUpdaterDownloaderNil    = errors.New("downloader cannot be nil")
	ErrInstanceUpdaterWorkRootNil      = errors.New("workRoot cannot be nil")
	ErrInstanceUpdaterNil              = errors.New("instance updater cannot be nil")
)

// InstanceUpdater implements UpdaterService for instance updates
// InstanceUpdater handles downloading and extracting instance.tar.gz from remote storage
type InstanceUpdater struct {
	librarian  ports.LibrarianService
	validator  ports.ValidatorService
	downloader streamer.S3StreamDownloader
	bucket     string
	workRoot   *os.Root
}

// Compile-time check to ensure InstanceUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*InstanceUpdater)(nil)

// NewInstanceUpdater creates a new instance updater
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewInstanceUpdater(
	librarian ports.LibrarianService,
	validator ports.ValidatorService,
	downloader streamer.S3StreamDownloader,
	bucket string,
	workRoot *os.Root,
) (*InstanceUpdater, error) {
	if librarian == nil {
		return nil, ErrInstanceUpdaterLibrarianNil
	}
	if validator == nil {
		return nil, ErrInstanceUpdaterValidatorNil
	}
	if downloader == nil {
		return nil, ErrInstanceUpdaterDownloaderNil
	}
	if workRoot == nil {
		return nil, ErrInstanceUpdaterWorkRootNil
	}

	updater := &InstanceUpdater{
		librarian:  librarian,
		validator:  validator,
		downloader: downloader,
		bucket:     bucket,
		workRoot:   workRoot,
	}

	// Postcondition assertion
	if updater == nil {
		return nil, errors.New("instance updater initialization failed")
	}

	return updater, nil
}

// Run executes the instance update process
// Returns nil if no update needed or update succeeded, error if update failed
func (u *InstanceUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrInstanceUpdaterNil
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

	// Get or initialize local manifest
	localManifest, err := u.getOrInitializeLocalManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

	// Check if instance update is needed
	if err := u.validator.CheckInstance(localManifest, remoteManifest); err != nil {
		if err.Error() == "outdated instance" {
			// Instance needs update
			if updateErr := u.updateInstance(ctx, remoteManifest); updateErr != nil {
				return fmt.Errorf("failed to update instance: %w", updateErr)
			}
		} else {
			return fmt.Errorf("instance validation failed: %w", err)
		}
	}

	return nil
}

// getOrInitializeLocalManifest retrieves local manifest or initializes a new instance
func (u *InstanceUpdater) getOrInitializeLocalManifest(ctx context.Context, remoteManifest *domain.Manifest) (*domain.Manifest, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return nil, errors.New("remote manifest cannot be nil")
	}

	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "key not found") {
			// No local manifest - initialize new instance
			if initErr := u.initializeInstance(ctx, remoteManifest); initErr != nil {
				return nil, fmt.Errorf("failed to initialize instance: %w", initErr)
			}
			// Get the newly created manifest
			localManifest, err = u.librarian.GetLocalManifest(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get local manifest after initialization: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get local manifest: %w", err)
		}
	}

	if localManifest == nil {
		return nil, errors.New("local manifest cannot be nil")
	}

	return localManifest, nil
}

// initializeInstance sets up a new local instance
func (u *InstanceUpdater) initializeInstance(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	// Download and extract instance
	if err := u.downloadAndExtractInstance(ctx); err != nil {
		return err
	}

	// Create local manifest with only instance info, not worlds
	// Worlds will be added by WorldsUpdater
	localManifest := &domain.Manifest{
		RitualVersion:   remoteManifest.RitualVersion,
		InstanceVersion: remoteManifest.InstanceVersion,
		StoredWorlds:    []domain.World{}, // Empty - WorldsUpdater handles this
		UpdatedAt:       remoteManifest.UpdatedAt,
	}

	// Save local manifest
	if err := u.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	return nil
}

// updateInstance replaces existing instance with updated version
func (u *InstanceUpdater) updateInstance(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	// Download and extract instance
	if err := u.downloadAndExtractInstance(ctx); err != nil {
		return err
	}

	// Save updated local manifest
	if err := u.librarian.SaveLocalManifest(ctx, remoteManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	return nil
}

// downloadAndExtractInstance downloads instance.tar.gz from remote and extracts it
func (u *InstanceUpdater) downloadAndExtractInstance(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Destination directory
	destPath := filepath.Join(u.workRoot.Name(), config.InstanceDir)

	// Use streamer.Pull to download and extract
	err := streamer.Pull(ctx, streamer.PullConfig{
		Bucket:   u.bucket,
		Key:      config.InstanceArchiveKey,
		Dest:     destPath,
		Conflict: streamer.Replace,
	}, u.downloader)
	if err != nil {
		return fmt.Errorf("failed to download and extract instance: %w", err)
	}

	return nil
}
