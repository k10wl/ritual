package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"ritual/internal/core/ports"
)

// ArchivePaperWorld creates a backup archive from multiple data sources
// Returns the archive path and a cleanup function
func ArchivePaperWorld(
	ctx context.Context,
	storage ports.StorageRepository,
	archiveService ports.ArchiveService,
	instancePath string,
	destinationPath string,
	name string,
) (string, func() error, error) {
	if storage == nil {
		return "", func() error { return nil }, errors.New("storage cannot be nil")
	}
	if archiveService == nil {
		return "", func() error { return nil }, errors.New("archiveService cannot be nil")
	}
	if instancePath == "" {
		return "", func() error { return nil }, errors.New("instancePath cannot be empty")
	}
	if destinationPath == "" {
		return "", func() error { return nil }, errors.New("destinationPath cannot be empty")
	}
	if name == "" {
		return "", func() error { return nil }, errors.New("name cannot be empty")
	}

	archivePath := filepath.Join(destinationPath, fmt.Sprintf("%s.zip", name))
	log.Println("Archiving worlds directly to", archivePath)

	targetPaths := []string{
		filepath.Join(instancePath, "world"),
		filepath.Join(instancePath, "world_nether"),
		filepath.Join(instancePath, "world_the_end"),
	}

	tempDir := filepath.Join(destinationPath, fmt.Sprintf("tmp_%d", time.Now().Unix()))
	log.Println("Temp dir:", tempDir)

	for _, targetPath := range targetPaths {
		leaf := filepath.Base(targetPath)
		fullDestinationPath := filepath.Join(tempDir, leaf)
		log.Println("Copying", targetPath, "to", fullDestinationPath)
		err := storage.Copy(ctx, targetPath, fullDestinationPath)
		if err != nil {
			return "", func() error { return nil }, err
		}
	}

	// Archive each world directory directly
	log.Println("Archiving", tempDir, "to", archivePath)
	err := archiveService.Archive(ctx, tempDir, archivePath)
	if err != nil {
		return "", func() error { return nil }, err
	}
	storage.Delete(ctx, tempDir)

	return archivePath, func() error { return storage.Delete(ctx, tempDir) }, nil
}
