package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// ArchivePaperWorld creates a backup archive from multiple data sources
// Returns the archive path, backup name, and a cleanup function
func ArchivePaperWorld(
	ctx context.Context,
	storage ports.StorageRepository,
	archiveService ports.ArchiveService,
	instanceRoot *os.Root,
	destinationPath string,
	name string,
) (string, string, func() error, error) {
	if storage == nil {
		return "", "", func() error { return nil }, errors.New("storage cannot be nil")
	}
	if archiveService == nil {
		return "", "", func() error { return nil }, errors.New("archiveService cannot be nil")
	}
	if instanceRoot == nil {
		return "", "", func() error { return nil }, errors.New("instanceRoot cannot be nil")
	}
	if destinationPath == "" {
		return "", "", func() error { return nil }, errors.New("destinationPath cannot be empty")
	}
	if name == "" {
		return "", "", func() error { return nil }, errors.New("name cannot be empty")
	}

	archivePath := filepath.Join(destinationPath, fmt.Sprintf("%s.zip", name))
	log.Println("Archiving worlds directly to", archivePath)

	// Get relative paths for storage operations
	// These paths are relative to wherever the storage is rooted
	targetKeys := []string{
		filepath.Join(config.InstanceDir, "world"),
		filepath.Join(config.InstanceDir, "world_nether"),
		filepath.Join(config.InstanceDir, "world_the_end"),
	}

	archiveName := fmt.Sprintf("%s.zip", name)

	tempDir := filepath.Join(destinationPath, fmt.Sprintf("%s_%s", config.TmpDir, archiveName))
	log.Println("Temp dir:", tempDir)

	for _, targetKey := range targetKeys {
		leaf := filepath.Base(targetKey)
		destKey := filepath.Join(tempDir, leaf)
		log.Println("Copying", targetKey, "to", destKey)
		err := storage.Copy(ctx, targetKey, destKey)
		if err != nil {
			return "", "", func() error { return nil }, err
		}
	}

	// Archive each world directory directly
	log.Println("Archiving", tempDir, "to", archivePath)
	err := archiveService.Archive(ctx, tempDir, archivePath)
	if err != nil {
		return "", "", func() error { return nil }, err
	}

	return archivePath, name, func() error {
		if err := storage.Delete(ctx, tempDir); err != nil {
			// Ignore "key not found" errors during cleanup as temp dir may already be deleted
			if !strings.Contains(err.Error(), "key not found") {
				return err
			}
		}
		if err := storage.Delete(ctx, archivePath); err != nil {
			// Ignore "key not found" errors during cleanup as archive may already be deleted
			if !strings.Contains(err.Error(), "key not found") {
				return err
			}
		}
		return nil
	}, nil
}
