package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// BackupperService implements the backup orchestration interface
type BackupperService struct {
	storage        ports.StorageRepository
	buildArchive   func() (string, func() error, error) // Returns generated path and cleanup
	markForCleanup func(ports.StorageRepository) ([]domain.World, error)
}

// Compile-time check to ensure BackupperService implements ports.BackupperService
var _ ports.BackupperService = (*BackupperService)(nil)

// NewBackupperService creates a new backupper service instance
func NewBackupperService(storage ports.StorageRepository, buildArchive func() (string, func() error, error), markForCleanup func(ports.StorageRepository) ([]domain.World, error)) (*BackupperService, error) {
	if storage == nil {
		return nil, errors.New("storage cannot be nil")
	}
	if buildArchive == nil {
		return nil, errors.New("buildArchive cannot be nil")
	}
	if markForCleanup == nil {
		return nil, errors.New("markForCleanup cannot be nil")
	}

	return &BackupperService{
		storage:        storage,
		buildArchive:   buildArchive,
		markForCleanup: markForCleanup,
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

	// validateArchive(archivePath)
	if err := b.validateArchive(archivePath); err != nil {
		return nil, err
	}

	// store(archivePath)
	if err := b.store(archivePath); err != nil {
		return nil, err
	}

	// applyRetention()
	if err := b.applyRetention(); err != nil {
		return nil, err
	}

	// Return cleanup function and error if any step fails, else success
	// Cleanup function applies retention and removes created archives
	return cleanup, nil
}

// validateArchive confirms archive exists, readable, and checksum valid
func (b *BackupperService) validateArchive(archivePath string) error {
	// TODO: Implement archive validation logic
	return nil
}

// store persists archive to storage backend with timestamp format
func (b *BackupperService) store(archivePath string) error {
	// Read archive file
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}

	// Generate timestamp filename
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("%d.zip", timestamp)

	// Store with timestamp format
	return b.storage.Put(context.Background(), filename, data)
}

// applyRetention executes retention policy using markForCleanup and deletes expired backups
func (b *BackupperService) applyRetention() error {
	// TODO: Implement retention policy logic
	return nil
}

// Exit gracefully shuts down the backup service
func (b *BackupperService) Exit() error {
	// TODO: Implement cleanup and shutdown logic
	return nil
}
