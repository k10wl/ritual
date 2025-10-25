package adapters

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"sort"
	"time"
)

const (
	r2MaxBackups          = 5                                              // Maximum number of backups to retain for R2
	r2MaxFiles            = 1000                                           // Maximum number of files to process in single operation
	r2TimestampLength     = 14                                             // Expected length of timestamp string (YYYYMMDDHHMMSS)
	r2MinFilenameLength   = len(r2BackupFileExtension) + r2TimestampLength // Minimum filename length
	r2BackupDirectory     = config.LocalBackups + "/"
	r2BackupFileExtension = ".zip"
	r2TimestampFormat     = "20060102150405"
)

// R2BackupTarget implements BackupTarget using R2 cloud storage
type R2BackupTarget struct {
	storage ports.StorageRepository
	ctx     context.Context
}

// NewR2BackupTarget creates a new R2 backup target
func NewR2BackupTarget(storage ports.StorageRepository, ctx context.Context) (*R2BackupTarget, error) {
	if storage == nil {
		return nil, errors.New("storage repository cannot be nil")
	}
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	target := &R2BackupTarget{
		storage: storage,
		ctx:     ctx,
	}

	// Postcondition assertion
	if target.storage == nil {
		return nil, errors.New("target initialization failed")
	}

	return target, nil
}

// Backup stores the provided data to the R2 backup destination
func (r *R2BackupTarget) Backup(data []byte, name string) error {
	if r == nil {
		return errors.New("R2 backup target cannot be nil")
	}
	if r.storage == nil {
		return errors.New("storage repository cannot be nil")
	}
	if r.ctx == nil {
		return errors.New("context cannot be nil")
	}
	if data == nil {
		return errors.New("backup data cannot be nil")
	}
	if len(data) == 0 {
		return errors.New("backup data cannot be empty")
	}
	if name == "" {
		return errors.New("backup name cannot be empty")
	}

	// Generate timestamp-based filename
	timestamp := time.Now().Format(r2TimestampFormat)
	// Validate timestamp format
	if len(timestamp) != r2TimestampLength {
		return errors.New("timestamp format validation failed")
	}
	filename := fmt.Sprintf("%s%s/%s%s", r2BackupDirectory, name, timestamp, r2BackupFileExtension)

	// Store backup data using storage repository
	if err := r.storage.Put(r.ctx, filename, data); err != nil {
		return fmt.Errorf("failed to store backup file %s: %w", filename, err)
	}

	// Postcondition assertion
	if filename == "" {
		return errors.New("backup filename generation failed")
	}

	return nil
}

// DataRetention applies retention policies to manage stored backups
func (r *R2BackupTarget) DataRetention() error {
	if r == nil {
		return errors.New("R2 backup target cannot be nil")
	}
	if r.storage == nil {
		return errors.New("storage repository cannot be nil")
	}
	if r.ctx == nil {
		return errors.New("context cannot be nil")
	}

	// 1. List all files
	files, err := r.getBackupFiles()
	if err != nil {
		return fmt.Errorf("failed to get backup files: %w", err)
	}

	// 1.1. If all files are 5 or less - short return
	if len(files) <= r2MaxBackups {
		return nil
	}

	// 2. Sort names based on timestamps (already done in getBackupFiles)
	// 3. Use slices op to extract part of elements [:] to have 6+ in new slice
	extraFiles := files[r2MaxBackups:]

	// 4. For file of extra { storage.delete
	for _, file := range extraFiles {
		if err := r.storage.Delete(r.ctx, file); err != nil {
			return fmt.Errorf("failed to remove backup file %s: %w", file, err)
		}
	}

	// Postcondition assertion
	if len(extraFiles) != len(files)-r2MaxBackups {
		return errors.New("retention policy calculation error")
	}

	return nil
}

// getBackupFiles returns all backup files with valid timestamps sorted in descending order
// Invalid timestamp files are deleted immediately
func (r *R2BackupTarget) getBackupFiles() ([]string, error) {
	if r == nil {
		return nil, errors.New("R2 backup target cannot be nil")
	}
	if r.storage == nil {
		return nil, errors.New("storage repository cannot be nil")
	}
	if r.ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	keys, err := r.storage.List(r.ctx, r2BackupDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	// Static bounds check
	if len(keys) > r2MaxFiles {
		return nil, fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), r2MaxFiles)
	}

	validFiles, err := r.validateAndFilterFiles(keys)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp in descending order (newest first)
	r.sortFilesByTimestamp(validFiles)

	return validFiles, nil
}

// validateAndFilterFiles validates backup files and removes invalid ones
func (r *R2BackupTarget) validateAndFilterFiles(keys []string) ([]string, error) {
	if r == nil {
		return nil, errors.New("R2 backup target cannot be nil")
	}
	if r.storage == nil {
		return nil, errors.New("storage repository cannot be nil")
	}
	if r.ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if keys == nil {
		return nil, errors.New("keys cannot be nil")
	}

	// Static bounds check
	if len(keys) > r2MaxFiles {
		return nil, fmt.Errorf("too many files to process: %d exceeds limit %d", len(keys), r2MaxFiles)
	}

	var validFiles []string
	for i, key := range keys {
		// Bounds validation for loop iteration
		if i >= len(keys) {
			return nil, errors.New("loop bounds validation failed")
		}

		if filepath.Ext(key) == r2BackupFileExtension {
			filename := filepath.Base(key)
			// Validate filename length before substring operations
			if len(filename) < r2MinFilenameLength {
				// Delete invalid timestamp files immediately
				if deleteErr := r.storage.Delete(r.ctx, key); deleteErr != nil {
					return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
				}
				continue // Skip files with invalid names
			}
			if filename[len(filename)-len(r2BackupFileExtension):] == r2BackupFileExtension {
				timestampStr := filename[:len(filename)-len(r2BackupFileExtension)]
				// Validate timestamp string length
				if len(timestampStr) != r2TimestampLength {
					// Delete invalid timestamp files immediately
					if deleteErr := r.storage.Delete(r.ctx, key); deleteErr != nil {
						return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
					}
					continue
				}
				// Validate timestamp format
				if _, err := time.Parse(r2TimestampFormat, timestampStr); err == nil {
					validFiles = append(validFiles, key)
				} else {
					// Delete invalid timestamp files immediately
					if deleteErr := r.storage.Delete(r.ctx, key); deleteErr != nil {
						return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
					}
				}
			}
		}
	}

	// Postcondition assertion
	if len(validFiles) > len(keys) {
		return nil, errors.New("validation logic error: valid files exceed input")
	}

	return validFiles, nil
}

// sortFilesByTimestamp sorts files by timestamp in descending order (newest first)
func (r *R2BackupTarget) sortFilesByTimestamp(files []string) {
	sort.Slice(files, func(i, j int) bool {
		filenameI := filepath.Base(files[i])
		filenameJ := filepath.Base(files[j])
		// Validate filename lengths before substring operations
		if len(filenameI) < r2MinFilenameLength || len(filenameJ) < r2MinFilenameLength {
			return false // Invalid filenames, maintain order
		}
		timestampStrI := filenameI[:len(filenameI)-len(r2BackupFileExtension)]
		timestampStrJ := filenameJ[:len(filenameJ)-len(r2BackupFileExtension)]

		timestampI, _ := time.Parse(r2TimestampFormat, timestampStrI)
		timestampJ, _ := time.Parse(r2TimestampFormat, timestampStrJ)

		return timestampI.After(timestampJ)
	})
}

// Ensure R2BackupTarget implements BackupTarget interface
var _ ports.BackupTarget = (*R2BackupTarget)(nil)
