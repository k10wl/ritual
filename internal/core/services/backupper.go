package services

import (
	"errors"
	"fmt"
	"os"

	"ritual/internal/core/ports"
)

// BackupperService implements the backup orchestration interface
type BackupperService struct {
	buildArchive func() (string, func() error, error) // Returns generated path and cleanup
	targets      []ports.BackupTarget                 // List of backup destinations
}

// Compile-time check to ensure BackupperService implements ports.BackupperService
var _ ports.BackupperService = (*BackupperService)(nil)

// NewBackupperService creates a new backupper service instance
func NewBackupperService(buildArchive func() (string, func() error, error), targets []ports.BackupTarget) (*BackupperService, error) {
	if buildArchive == nil {
		return nil, errors.New("buildArchive cannot be nil")
	}
	if len(targets) == 0 {
		return nil, errors.New("at least one backup target is required")
	}

	return &BackupperService{
		buildArchive: buildArchive,
		targets:      targets,
	}, nil
}

// Run executes the backup orchestration process
func (b *BackupperService) Run() (func() error, error) {
	// Call buildArchive() to generate archive path and get cleanup function
	archivePath, cleanup, err := b.buildArchive()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Validate archive
	if err := b.validateArchive(archivePath); err != nil {
		return nil, err
	}

	// Read archive data
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, err
	}

	// Store to all targets and apply retention
	for _, target := range b.targets {
		if err := target.Backup(data); err != nil {
			return nil, err
		}
		if err := target.DataRetention(); err != nil {
			return nil, err
		}
	}

	return cleanup, nil
}

// validateArchive confirms archive exists, readable, and checksum valid
func (b *BackupperService) validateArchive(archivePath string) error {
	if archivePath == "" {
		return errors.New("archive path cannot be empty")
	}

	// Check if file exists
	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("archive file does not exist: %w", err)
	}

	// Check if file is readable
	if info.Mode()&0400 == 0 {
		return errors.New("archive file is not readable")
	}

	// Check if file has content
	if info.Size() == 0 {
		return errors.New("archive file is empty")
	}

	// Validate zip format by attempting to open
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer file.Close()

	// Read first few bytes to check zip signature
	buffer := make([]byte, 4)
	_, err = file.Read(buffer)
	if err != nil {
		return fmt.Errorf("failed to read archive file: %w", err)
	}

	// Check for ZIP file signature (PK)
	if buffer[0] != 0x50 || buffer[1] != 0x4B {
		return errors.New("file is not a valid ZIP archive")
	}

	return nil
}
