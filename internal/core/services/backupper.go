package services

import (
	"errors"
	"fmt"
	"os"

	"ritual/internal/core/ports"
)

// BackupperService implements the backup orchestration interface
type BackupperService struct {
	buildArchive func() (string, string, func() error, error) // Returns generated path, name, and cleanup
	targets      []ports.BackupTarget                         // List of backup destinations
	workRoot     *os.Root                                      // Working root for safe operations
}

// Compile-time check to ensure BackupperService implements ports.BackupperService
var _ ports.BackupperService = (*BackupperService)(nil)

// NewBackupperService creates a new backupper service instance
func NewBackupperService(buildArchive func() (string, string, func() error, error), targets []ports.BackupTarget, workRoot *os.Root) (*BackupperService, error) {
	if buildArchive == nil {
		return nil, errors.New("buildArchive cannot be nil")
	}
	if len(targets) == 0 {
		return nil, errors.New("at least one backup target is required")
	}
	if workRoot == nil {
		return nil, errors.New("workRoot cannot be nil")
	}

	return &BackupperService{
		buildArchive: buildArchive,
		targets:      targets,
		workRoot:     workRoot,
	}, nil
}

// Run executes the backup orchestration process
// Returns the archive name that was created for manifest updates
func (b *BackupperService) Run() (string, error) {
	// Call buildArchive() to generate archive path, name, and get cleanup function
	archivePath, backupName, cleanup, err := b.buildArchive()
	if err != nil {
		return "", err
	}
	defer cleanup()

	// Validate archive using root
	if err := b.validateArchiveWithRoot(b.workRoot, archivePath); err != nil {
		return "", err
	}

	// Read archive data using root
	data, err := b.workRoot.ReadFile(archivePath)
	if err != nil {
		return "", err
	}

	// Store to all targets and apply retention
	for _, target := range b.targets {
		if err := target.Backup(data, backupName); err != nil {
			return "", err
		}
		if err := target.DataRetention(); err != nil {
			return "", err
		}
	}

	return backupName, nil
}

// validateArchiveWithRoot validates the archive file using os.Root
func (b *BackupperService) validateArchiveWithRoot(root *os.Root, archivePath string) error {
	if archivePath == "" {
		return errors.New("archive path cannot be empty")
	}

	// Check if file exists
	info, err := root.Stat(archivePath)
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
	file, err := root.Open(archivePath)
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
