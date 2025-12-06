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

// LocalBackupper constants
const (
	localMaxBackups          = 10
	localMaxFiles            = 1000
	localTimestampLength     = 14
	localMinFilenameLength   = len(localBackupFileExtension) + localTimestampLength
	localBackupDirectory     = config.LocalBackups + "/"
	localBackupFileExtension = ".zip"
	localTimestampFormat     = "20060102150405"
)

// LocalBackupper error constants
var (
	ErrLocalBackupperStorageNil  = errors.New("local storage repository cannot be nil")
	ErrLocalBackupperArchiveNil  = errors.New("archive service cannot be nil")
	ErrLocalBackupperWorkRootNil = errors.New("workRoot cannot be nil")
	ErrLocalBackupperNil         = errors.New("local backupper cannot be nil")
)

// LocalBackupper implements BackupperService for local backup storage
// LocalBackupper creates archives from world directories and stores them locally
type LocalBackupper struct {
	localStorage ports.StorageRepository
	archive      ports.ArchiveService
	workRoot     *os.Root
}

// Compile-time check to ensure LocalBackupper implements ports.BackupperService
var _ ports.BackupperService = (*LocalBackupper)(nil)

// NewLocalBackupper creates a new local backupper
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewLocalBackupper(
	localStorage ports.StorageRepository,
	archive ports.ArchiveService,
	workRoot *os.Root,
) (*LocalBackupper, error) {
	if localStorage == nil {
		return nil, ErrLocalBackupperStorageNil
	}
	if archive == nil {
		return nil, ErrLocalBackupperArchiveNil
	}
	if workRoot == nil {
		return nil, ErrLocalBackupperWorkRootNil
	}

	backupper := &LocalBackupper{
		localStorage: localStorage,
		archive:      archive,
		workRoot:     workRoot,
	}

	// Postcondition assertion
	if backupper == nil {
		return nil, errors.New("local backupper initialization failed")
	}

	return backupper, nil
}

// Run executes the local backup process
// Returns the archive name for manifest updates
func (b *LocalBackupper) Run(ctx context.Context) (string, error) {
	if b == nil {
		return "", ErrLocalBackupperNil
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

	// Store archive in local backup directory
	if err := b.storeArchive(ctx, archivePath, backupName); err != nil {
		return "", fmt.Errorf("failed to store archive: %w", err)
	}

	// Apply retention policy
	if err := b.applyRetention(ctx); err != nil {
		return "", fmt.Errorf("failed to apply retention: %w", err)
	}

	return backupName, nil
}

// createWorldArchive creates a zip archive from world directories
func (b *LocalBackupper) createWorldArchive(ctx context.Context, name string) (string, func(), error) {
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

// storeArchive stores the archive in the backup directory with proper naming
func (b *LocalBackupper) storeArchive(ctx context.Context, archivePath string, name string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Read archive data
	data, err := b.workRoot.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	// Generate timestamp-based filename
	timestamp := time.Now().Format(localTimestampFormat)
	filename := filepath.Join(config.LocalBackups, fmt.Sprintf("%s.zip", timestamp))

	// Store backup
	if err := b.localStorage.Put(ctx, filename, data); err != nil {
		return fmt.Errorf("failed to store backup: %w", err)
	}

	return nil
}

// applyRetention removes old backups exceeding the limit
func (b *LocalBackupper) applyRetention(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// Get all backup files
	files, err := b.getBackupFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to get backup files: %w", err)
	}

	// If under limit, nothing to do
	if len(files) <= localMaxBackups {
		return nil
	}

	// Remove oldest backups
	for i := localMaxBackups; i < len(files); i++ {
		if err := b.localStorage.Delete(ctx, files[i]); err != nil {
			return fmt.Errorf("failed to remove old backup %s: %w", files[i], err)
		}
	}

	return nil
}

// getBackupFiles returns all valid backup files sorted by timestamp (newest first)
func (b *LocalBackupper) getBackupFiles(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	keys, err := b.localStorage.List(ctx, config.LocalBackups)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	// Static bounds check
	if len(keys) > localMaxFiles {
		return nil, fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), localMaxFiles)
	}

	var validFiles []string
	for _, key := range keys {
		if filepath.Ext(key) == localBackupFileExtension {
			filename := filepath.Base(key)
			// Skip temp directories
			if strings.HasPrefix(filename, "temp_") {
				continue
			}
			// Validate filename format
			if len(filename) >= localMinFilenameLength {
				timestampStr := filename[:len(filename)-len(localBackupFileExtension)]
				if _, err := time.Parse(localTimestampFormat, timestampStr); err == nil {
					validFiles = append(validFiles, key)
				}
			}
		}
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(validFiles, func(i, j int) bool {
		filenameI := filepath.Base(validFiles[i])
		filenameJ := filepath.Base(validFiles[j])
		timestampStrI := filenameI[:len(filenameI)-len(localBackupFileExtension)]
		timestampStrJ := filenameJ[:len(filenameJ)-len(localBackupFileExtension)]
		timestampI, _ := time.Parse(localTimestampFormat, timestampStrI)
		timestampJ, _ := time.Parse(localTimestampFormat, timestampStrJ)
		return timestampI.After(timestampJ)
	})

	return validFiles, nil
}
