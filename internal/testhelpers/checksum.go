package testhelpers

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"sort"
	"strings"
)

// DirectoryChecksum calculates SHA-256 checksum for entire directory structure
func DirectoryChecksum(dirPath string) (string, error) {
	if dirPath == "" {
		return "", fmt.Errorf("directory path cannot be empty")
	}

	// Verify directory exists
	info, err := os.Stat(dirPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat directory %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %s is not a directory", dirPath)
	}

	// Collect all file paths for consistent ordering
	var filePaths []string
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Convert to relative path for consistent hashing
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path for %s: %w", path, err)
			}
			// Normalize path separators for cross-platform consistency
			relPath = strings.ReplaceAll(relPath, "\\", "/")
			filePaths = append(filePaths, relPath)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}

	// Sort paths for consistent ordering
	sort.Strings(filePaths)

	// Calculate combined hash
	hasher := sha256.New()
	for _, relPath := range filePaths {
		// Include path in hash
		hasher.Write([]byte(relPath))
		hasher.Write([]byte("\n"))

		// Include file content directly
		fullPath := filepath.Join(dirPath, relPath)
		file, err := os.Open(fullPath)
		if err != nil {
			return "", fmt.Errorf("failed to open file %s: %w", fullPath, err)
		}
		if _, err := io.Copy(hasher, file); err != nil {
			file.Close()
			return "", fmt.Errorf("failed to read file %s: %w", fullPath, err)
		}
		file.Close()
		hasher.Write([]byte("\n"))
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// CompareDirectories validates that two directories have matching checksums
func CompareDirectories(dir1Path, dir2Path, description string) error {
	checksum1, err := DirectoryChecksum(dir1Path)
	if err != nil {
		return fmt.Errorf("failed to calculate %s checksum for %s: %w", description, dir1Path, err)
	}

	checksum2, err := DirectoryChecksum(dir2Path)
	if err != nil {
		return fmt.Errorf("failed to calculate %s checksum for %s: %w", description, dir2Path, err)
	}

	if checksum1 != checksum2 {
		return fmt.Errorf("%s checksum mismatch: %s=%s, %s=%s", description, dir1Path, checksum1, dir2Path, checksum2)
	}

	return nil
}

// CompareWorldDirectories validates that local and remote world directories have matching checksums
func CompareWorldDirectories(localInstancePath, remoteWorldsPath string, worldDirs []string) error {
	for _, worldDir := range worldDirs {
		localWorldDir := filepath.Join(localInstancePath, worldDir)
		remoteWorldDir := filepath.Join(remoteWorldsPath, worldDir)

		err := CompareDirectories(localWorldDir, remoteWorldDir, worldDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// CompareInstanceDirectories validates that local and remote instance directories have matching checksums
func CompareInstanceDirectories(localInstancePath, remoteInstancePath string) error {
	return CompareDirectories(localInstancePath, remoteInstancePath, config.InstanceDir)
}
