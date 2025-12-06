package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"sort"
	"strings"
	"time"
)

// R2Backupper constants
const (
	r2MaxBackups          = 5
	r2MaxFiles            = 1000
	r2TimestampLength     = 14
	r2MinFilenameLength   = len(r2BackupFileExtension) + r2TimestampLength
	r2BackupDirectory     = config.LocalBackups + "/"
	r2BackupFileExtension = ".zip"
	r2TimestampFormat     = "20060102150405"
)

// R2Backupper error constants
var (
	ErrR2BackupperLocalStorageNil  = errors.New("local storage repository cannot be nil")
	ErrR2BackupperRemoteStorageNil = errors.New("remote storage repository cannot be nil")
	ErrR2BackupperArchiveNil       = errors.New("archive service cannot be nil")
	ErrR2BackupperWorkRootNil      = errors.New("workRoot cannot be nil")
	ErrR2BackupperNil              = errors.New("R2 backupper cannot be nil")
)

// R2Backupper implements BackupperService for R2 backup storage
// R2Backupper creates archives from world directories and uploads to R2 storage
type R2Backupper struct {
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
	archive       ports.ArchiveService
	workRoot      *os.Root
}

// Compile-time check to ensure R2Backupper implements ports.BackupperService
var _ ports.BackupperService = (*R2Backupper)(nil)

// NewR2Backupper creates a new R2 backupper
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewR2Backupper(
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	archive ports.ArchiveService,
	workRoot *os.Root,
) (*R2Backupper, error) {
	if localStorage == nil {
		return nil, ErrR2BackupperLocalStorageNil
	}
	if remoteStorage == nil {
		return nil, ErrR2BackupperRemoteStorageNil
	}
	if archive == nil {
		return nil, ErrR2BackupperArchiveNil
	}
	if workRoot == nil {
		return nil, ErrR2BackupperWorkRootNil
	}

	backupper := &R2Backupper{
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
		archive:       archive,
		workRoot:      workRoot,
	}

	// Postcondition assertion
	if backupper == nil {
		return nil, errors.New("R2 backupper initialization failed")
	}

	return backupper, nil
}

// Run executes the R2 backup process
// Returns the archive name for manifest updates
func (b *R2Backupper) Run(ctx context.Context) (string, error) {
	if b == nil {
		return "", ErrR2BackupperNil
	}
	if ctx == nil {
		return "", errors.New("context cannot be nil")
	}

	// Generate backup name based on timestamp
	backupName := fmt.Sprintf("%d", time.Now().Unix())

	// Create archive from world directories
	archivePath, cleanupArchive, err := b.createWorldArchive(ctx, backupName)
	if err != nil {
		return "", fmt.Errorf("failed to create world archive: %w", err)
	}
	defer cleanupArchive()

	// Upload archive to R2 storage
	if err := b.uploadArchive(ctx, archivePath); err != nil {
		return "", fmt.Errorf("failed to upload archive: %w", err)
	}

	// Apply retention policy
	if err := b.applyRetention(ctx); err != nil {
		return "", fmt.Errorf("failed to apply retention: %w", err)
	}

	return backupName, nil
}

// createWorldArchive creates a zip archive from world directories
func (b *R2Backupper) createWorldArchive(ctx context.Context, name string) (string, func(), error) {
	if ctx == nil {
		return "", func() {}, errors.New("context cannot be nil")
	}
	if name == "" {
		return "", func() {}, errors.New("name cannot be empty")
	}

	// Paths for world directories
	worldDirs := []string{
		filepath.Join(config.InstanceDir, "world"),
		filepath.Join(config.InstanceDir, "world_nether"),
		filepath.Join(config.InstanceDir, "world_the_end"),
	}

	// Create temp directory for staging
	tempDir := filepath.Join(config.LocalBackups, fmt.Sprintf("temp_%s", name))
	archivePath := filepath.Join(config.LocalBackups, fmt.Sprintf("%s.zip", name))

	// Copy world directories to temp location
	for _, worldDir := range worldDirs {
		leaf := filepath.Base(worldDir)
		destKey := filepath.Join(tempDir, leaf)
		if err := b.localStorage.Copy(ctx, worldDir, destKey); err != nil {
			// Cleanup on error
			_ = b.localStorage.Delete(ctx, tempDir)
			return "", func() {}, fmt.Errorf("failed to copy %s: %w", worldDir, err)
		}
	}

	// Create archive
	if err := b.archive.Archive(ctx, tempDir, archivePath); err != nil {
		_ = b.localStorage.Delete(ctx, tempDir)
		return "", func() {}, fmt.Errorf("failed to create archive: %w", err)
	}

	// Cleanup function
	cleanup := func() {
		_ = b.localStorage.Delete(ctx, tempDir)
		_ = b.localStorage.Delete(ctx, archivePath)
	}

	return archivePath, cleanup, nil
}

// uploadArchive uploads the archive to R2 storage
func (b *R2Backupper) uploadArchive(ctx context.Context, archivePath string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Read archive data
	data, err := b.workRoot.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	// Generate timestamp-based filename
	timestamp := time.Now().Format(r2TimestampFormat)
	// Use forward slashes for R2 storage keys
	filename := config.LocalBackups + "/" + fmt.Sprintf("%s.zip", timestamp)

	// Upload to R2
	if err := b.remoteStorage.Put(ctx, filename, data); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	return nil
}

// applyRetention removes old backups exceeding the limit
func (b *R2Backupper) applyRetention(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Get all backup files
	files, err := b.getBackupFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to get backup files: %w", err)
	}

	// If under limit, nothing to do
	if len(files) <= r2MaxBackups {
		return nil
	}

	// Remove oldest backups
	for i := r2MaxBackups; i < len(files); i++ {
		if err := b.remoteStorage.Delete(ctx, files[i]); err != nil {
			return fmt.Errorf("failed to remove old backup %s: %w", files[i], err)
		}
	}

	return nil
}

// getBackupFiles returns all valid backup files sorted by timestamp (newest first)
func (b *R2Backupper) getBackupFiles(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	keys, err := b.remoteStorage.List(ctx, config.LocalBackups)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	// Static bounds check
	if len(keys) > r2MaxFiles {
		return nil, fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), r2MaxFiles)
	}

	var validFiles []string
	for _, key := range keys {
		if filepath.Ext(key) == r2BackupFileExtension {
			filename := filepath.Base(key)
			// Skip temp directories
			if strings.HasPrefix(filename, "temp_") {
				continue
			}
			// Validate filename format
			if len(filename) >= r2MinFilenameLength {
				timestampStr := filename[:len(filename)-len(r2BackupFileExtension)]
				if _, err := time.Parse(r2TimestampFormat, timestampStr); err == nil {
					validFiles = append(validFiles, key)
				}
			}
		}
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(validFiles, func(i, j int) bool {
		filenameI := filepath.Base(validFiles[i])
		filenameJ := filepath.Base(validFiles[j])
		timestampStrI := filenameI[:len(filenameI)-len(r2BackupFileExtension)]
		timestampStrJ := filenameJ[:len(filenameJ)-len(r2BackupFileExtension)]
		timestampI, _ := time.Parse(r2TimestampFormat, timestampStrI)
		timestampJ, _ := time.Parse(r2TimestampFormat, timestampStrJ)
		return timestampI.After(timestampJ)
	})

	return validFiles, nil
}
