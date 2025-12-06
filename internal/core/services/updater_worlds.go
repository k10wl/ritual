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

// WorldsUpdater error constants
var (
	ErrWorldsUpdaterLibrarianNil  = errors.New("librarian service cannot be nil")
	ErrWorldsUpdaterValidatorNil  = errors.New("validator service cannot be nil")
	ErrWorldsUpdaterDownloaderNil = errors.New("downloader cannot be nil")
	ErrWorldsUpdaterWorkRootNil   = errors.New("workRoot cannot be nil")
	ErrWorldsUpdaterNil           = errors.New("worlds updater cannot be nil")
)

// WorldsUpdater implements UpdaterService for world updates
// WorldsUpdater handles downloading and extracting world archives from remote storage
type WorldsUpdater struct {
	librarian  ports.LibrarianService
	validator  ports.ValidatorService
	downloader streamer.S3StreamDownloader
	bucket     string
	workRoot   *os.Root
}

// Compile-time check to ensure WorldsUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*WorldsUpdater)(nil)

// NewWorldsUpdater creates a new worlds updater
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewWorldsUpdater(
	librarian ports.LibrarianService,
	validator ports.ValidatorService,
	downloader streamer.S3StreamDownloader,
	bucket string,
	workRoot *os.Root,
) (*WorldsUpdater, error) {
	if librarian == nil {
		return nil, ErrWorldsUpdaterLibrarianNil
	}
	if validator == nil {
		return nil, ErrWorldsUpdaterValidatorNil
	}
	if downloader == nil {
		return nil, ErrWorldsUpdaterDownloaderNil
	}
	if workRoot == nil {
		return nil, ErrWorldsUpdaterWorkRootNil
	}

	updater := &WorldsUpdater{
		librarian:  librarian,
		validator:  validator,
		downloader: downloader,
		bucket:     bucket,
		workRoot:   workRoot,
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

	// Download and extract world archive using streamer.Pull
	if err := u.downloadAndExtractWorld(ctx, sanitizedURI); err != nil {
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

// downloadAndExtractWorld downloads and extracts the world archive from remote storage
func (u *WorldsUpdater) downloadAndExtractWorld(ctx context.Context, key string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if key == "" {
		return errors.New("key cannot be empty")
	}

	// Destination is the instance directory
	destPath := filepath.Join(u.workRoot.Name(), config.InstanceDir)

	// Use streamer.Pull to download and extract
	err := streamer.Pull(ctx, streamer.PullConfig{
		Bucket:   u.bucket,
		Key:      key,
		Dest:     destPath,
		Conflict: streamer.Replace,
	}, u.downloader)
	if err != nil {
		return fmt.Errorf("failed to download and extract world: %w", err)
	}

	return nil
}
