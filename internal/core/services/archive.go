// Package services provides archive functionality for compressing and extracting files.
//
// ArchiveService handles compression and extraction of data archives using relative paths
// based on a configured base path. All operations work within the base path directory.
//
// All paths are relative to the basePath. The service automatically:
// - Joins relative paths with basePath for actual file operations
// - Creates relative paths in archives based on basePath
// - Validates paths to prevent directory traversal attacks
package services

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ArchiveService handles compression and extraction of data archives
type ArchiveService struct {
	baseRoot *os.Root
}

// NewArchiveService creates a new ArchiveService instance
func NewArchiveService(root *os.Root) (*ArchiveService, error) {
	if root == nil {
		return nil, fmt.Errorf("root cannot be nil")
	}
	return &ArchiveService{baseRoot: root}, nil
}

// Archive compresses source to destination
func (a *ArchiveService) Archive(ctx context.Context, relSrc string, relDest string) error {
	if a == nil {
		return fmt.Errorf("archive service cannot be nil")
	}
	if relSrc == "" {
		return fmt.Errorf("source path cannot be empty")
	}
	if relDest == "" {
		return fmt.Errorf("destination path cannot be empty")
	}

	// Get full paths for zip operations
	source := filepath.Join(a.baseRoot.Name(), relSrc)
	destination := filepath.Join(a.baseRoot.Name(), relDest)

	// Check if source exists
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return fmt.Errorf("source path does not exist: %s", source)
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destination)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create zip file
	zipFile, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through source directory
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip the zip file we're creating to prevent archiving the archive itself
		if path == destination {
			return nil
		}
		return a.archivePath(path, info, zipWriter, source)
	})

	if err != nil {
		return fmt.Errorf("failed to archive files: %w", err)
	}

	return nil
}

// archivePath processes a single file or directory during archiving
func (a *ArchiveService) archivePath(path string, info os.FileInfo, zipWriter *zip.Writer, source string) error {

	// Create relative path for archive
	relPath, err := filepath.Rel(source, path)
	if err != nil {
		return err
	}

	// Skip root directory
	if relPath == "." {
		return nil
	}

	// Create zip file header
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	// Set archive path
	header.Name = strings.ReplaceAll(relPath, "\\", "/")

	// Handle directories
	if info.IsDir() {
		header.Name += "/"
		_, err = zipWriter.CreateHeader(header)
		return err
	}

	// Handle files
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	file.Close()
	return err
}

// Unarchive extracts archive to destination
func (a *ArchiveService) Unarchive(ctx context.Context, relArchive string, relDestination string) error {
	if a == nil {
		return fmt.Errorf("archive service cannot be nil")
	}
	if relArchive == "" {
		return fmt.Errorf("archive path cannot be empty")
	}
	if relDestination == "" {
		return fmt.Errorf("destination path cannot be empty")
	}

	// Get full paths for operations
	destination := filepath.Join(a.baseRoot.Name(), relDestination)
	archive := filepath.Join(a.baseRoot.Name(), relArchive)

	// Check if archive exists
	if _, err := os.Stat(archive); os.IsNotExist(err) {
		return fmt.Errorf("archive file does not exist: %s", archive)
	}

	// Create destination directory
	if err := os.MkdirAll(destination, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open zip file
	zipReader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer zipReader.Close()

	// Extract files
	for _, file := range zipReader.File {
		// Validate file path
		if strings.Contains(file.Name, "..") {
			return fmt.Errorf("invalid file path in archive: %s", file.Name)
		}

		path := filepath.Join(destination, file.Name)

		// Create directory if needed
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.FileInfo().Mode()); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Extract file
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.FileInfo().Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create output file: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}
