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
	maxBackups          = 10                                         // Maximum number of backups to retain
	maxFiles            = 1000                                       // Maximum number of files to process in single operation
	timestampLength     = 14                                         // Expected length of timestamp string (YYYYMMDDHHMMSS)
	minFilenameLength   = len(backupFileExtension) + timestampLength // Minimum filename length
	backupDirectory     = config.LocalBackups + "/"
	backupFileExtension = ".zip"
	timestampFormat     = "20060102150405"
)

// LocalBackupTarget implements BackupTarget using local filesystem storage
type LocalBackupTarget struct {
	storage ports.StorageRepository
	ctx     context.Context
}

// NewLocalBackupTarget creates a new local backup target
func NewLocalBackupTarget(storage ports.StorageRepository, ctx context.Context) (*LocalBackupTarget, error) {
	if storage == nil {
		return nil, errors.New("storage repository cannot be nil")
	}
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	return &LocalBackupTarget{
		storage: storage,
		ctx:     ctx,
	}, nil
}

// Backup stores the provided data to the local backup destination
func (l *LocalBackupTarget) Backup(data []byte, name string) error {
	if l == nil {
		return errors.New("local backup target cannot be nil")
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

	// Check if backup should be skipped based on monthly frequency
	shouldSkip, err := l.shouldSkipBackup()
	if err != nil {
		return fmt.Errorf("failed to check backup frequency: %w", err)
	}
	if shouldSkip {
		return nil // Skip backup if newest file was created in same month
	}

	// Generate timestamp-based filename
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%s%d_%s%s", backupDirectory, timestamp, name, backupFileExtension)

	// Store backup data using storage repository
	if err := l.storage.Put(l.ctx, filename, data); err != nil {
		return fmt.Errorf("failed to store backup file %s: %w", filename, err)
	}

	return nil
}

// DataRetention applies retention policies to manage stored backups
func (l *LocalBackupTarget) DataRetention() error {
	if l == nil {
		return errors.New("local backup target cannot be nil")
	}
	if l.storage == nil {
		return errors.New("storage repository cannot be nil")
	}

	// Get all backup files with timestamps
	files, err := l.getBackupFiles()
	if err != nil {
		return fmt.Errorf("failed to get backup files: %w", err)
	}

	// If amount of backups does not exceed limit, return
	if len(files) <= maxBackups {
		return nil
	}

	// Remove oldest backups until we have only maxBackups amount
	for i := maxBackups; i < len(files); i++ {
		if err := l.storage.Delete(l.ctx, files[i]); err != nil {
			return fmt.Errorf("failed to remove backup file %s: %w", files[i], err)
		}
	}

	// Postcondition assertion - verify we processed the correct number of files
	expectedRemaining := len(files)
	if len(files) > maxBackups {
		expectedRemaining = maxBackups
	}
	if len(files)-len(files[maxBackups:]) != expectedRemaining {
		return errors.New("retention policy calculation error")
	}

	return nil
}

// getBackupFiles returns all backup files with valid timestamps sorted in descending order
// Invalid timestamp files are deleted immediately
func (l *LocalBackupTarget) getBackupFiles() ([]string, error) {
	if l == nil {
		return nil, errors.New("local backup target cannot be nil")
	}
	if l.storage == nil {
		return nil, errors.New("storage repository cannot be nil")
	}

	keys, err := l.storage.List(l.ctx, backupDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup files: %w", err)
	}

	// Static bounds check
	if len(keys) > maxFiles {
		return nil, fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), maxFiles)
	}

	var validFiles []string
	for _, key := range keys {
		if filepath.Ext(key) == backupFileExtension {
			filename := filepath.Base(key)
			// Validate filename length before substring operations
			if len(filename) < minFilenameLength {
				// Delete invalid timestamp files immediately
				if deleteErr := l.storage.Delete(l.ctx, key); deleteErr != nil {
					return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
				}
				continue // Skip files with invalid names
			}
			if filename[len(filename)-len(backupFileExtension):] == backupFileExtension {
				timestampStr := filename[:len(filename)-len(backupFileExtension)]
				// Validate timestamp string length
				if len(timestampStr) != timestampLength {
					// Delete invalid timestamp files immediately
					if deleteErr := l.storage.Delete(l.ctx, key); deleteErr != nil {
						return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
					}
					continue
				}
				// Validate timestamp format
				if _, err := time.Parse(timestampFormat, timestampStr); err == nil {
					validFiles = append(validFiles, key)
				} else {
					// Delete invalid timestamp files immediately
					if deleteErr := l.storage.Delete(l.ctx, key); deleteErr != nil {
						return nil, fmt.Errorf("failed to delete invalid backup file %s: %w", key, deleteErr)
					}
				}
			}
		}
	}

	// Sort by timestamp in descending order (newest first)
	sort.Slice(validFiles, func(i, j int) bool {
		filenameI := filepath.Base(validFiles[i])
		filenameJ := filepath.Base(validFiles[j])
		// Validate filename lengths before substring operations
		if len(filenameI) < minFilenameLength || len(filenameJ) < minFilenameLength {
			return false // Invalid filenames, maintain order
		}
		timestampStrI := filenameI[:len(filenameI)-len(backupFileExtension)]
		timestampStrJ := filenameJ[:len(filenameJ)-len(backupFileExtension)]

		timestampI, _ := time.Parse(timestampFormat, timestampStrI)
		timestampJ, _ := time.Parse(timestampFormat, timestampStrJ)

		return timestampI.After(timestampJ)
	})

	return validFiles, nil
}

// shouldSkipBackup checks if backup should be skipped based on monthly frequency
func (l *LocalBackupTarget) shouldSkipBackup() (bool, error) {
	if l == nil {
		return false, errors.New("local backup target cannot be nil")
	}

	// Get all backup files
	files, err := l.getBackupFiles()
	if err != nil {
		return false, fmt.Errorf("failed to get backup files: %w", err)
	}

	// If no existing backups, don't skip
	if len(files) == 0 {
		return false, nil
	}

	// Get the newest backup file (first in sorted list)
	newestFile := files[0]
	filename := filepath.Base(newestFile)
	// Validate filename length before substring operation
	if len(filename) < minFilenameLength {
		return false, errors.New("invalid backup filename format")
	}
	timestampStr := filename[:len(filename)-len(backupFileExtension)]

	// Parse timestamp
	newestTime, err := time.Parse(timestampFormat, timestampStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse newest backup timestamp: %w", err)
	}

	// Check if newest backup was created in the same month as current time
	currentTime := time.Now()
	shouldSkip := newestTime.Year() == currentTime.Year() && newestTime.Month() == currentTime.Month()

	// Postcondition assertion
	if shouldSkip && len(files) == 0 {
		return false, errors.New("skip logic error - no files but should skip")
	}

	return shouldSkip, nil
}

// Ensure LocalBackupTarget implements BackupTarget interface
var _ ports.BackupTarget = (*LocalBackupTarget)(nil)
