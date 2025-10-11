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
type ArchiveService struct{}

// NewArchiveService creates a new ArchiveService instance
func NewArchiveService() *ArchiveService {
	return &ArchiveService{}
}

// Archive compresses source to destination
func (a *ArchiveService) Archive(ctx context.Context, source string, destination string) error {
	if a == nil {
		return fmt.Errorf("archive service cannot be nil")
	}
	if source == "" {
		return fmt.Errorf("source path cannot be empty")
	}
	if destination == "" {
		return fmt.Errorf("destination path cannot be empty")
	}

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
func (a *ArchiveService) Unarchive(ctx context.Context, archive string, destination string) error {
	if a == nil {
		return fmt.Errorf("archive service cannot be nil")
	}
	if archive == "" {
		return fmt.Errorf("archive path cannot be empty")
	}
	if destination == "" {
		return fmt.Errorf("destination path cannot be empty")
	}

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
