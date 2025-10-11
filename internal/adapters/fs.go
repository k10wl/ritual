package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/core/ports"
	"strings"
)

const (
	ErrOpenRootDir = "failed to open root directory %s: %w"
)

// FSRepository implements StorageRepository using local filesystem
type FSRepository struct {
	root *os.Root
}

// NewFSRepository creates a new filesystem storage repository
func NewFSRepository(basePath string) (*FSRepository, error) {
	root, err := os.OpenRoot(basePath)
	if err != nil {
		return nil, fmt.Errorf(ErrOpenRootDir, basePath, err)
	}

	return &FSRepository{
		root: root,
	}, nil
}

// Get retrieves data by key from filesystem
func (f *FSRepository) Get(ctx context.Context, key string) ([]byte, error) {
	file, err := f.root.Open(key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key not found: %s", key)
		}
		return nil, fmt.Errorf("failed to read file %s: %w", key, err)
	}
	defer file.Close()

	var data []byte
	buf := make([]byte, 1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("failed to read file %s: %w", key, err)
		}
	}

	return data, nil
}

// Put stores data with the given key to filesystem
func (f *FSRepository) Put(ctx context.Context, key string, data []byte) error {
	dir := filepath.Dir(key)
	if dir != "." {
		if err := f.root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	file, err := f.root.Create(key)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", key, err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write file %s: %w", key, err)
	}

	return nil
}

// Delete removes data by key from filesystem
func (f *FSRepository) Delete(ctx context.Context, key string) error {
	// Check if key is a directory by trying to open it
	file, err := f.root.Open(key)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to open %s: %w", key, err)
	}
	defer file.Close()

	// Check if it's a directory
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", key, err)
	}

	if stat.IsDir() {
		// For directories, we need to recursively delete contents
		// Since os.Root doesn't have RemoveAll, we'll use standard os.RemoveAll
		fullPath := filepath.Join(f.root.Name(), key)
		if err := os.RemoveAll(fullPath); err != nil {
			return fmt.Errorf("failed to delete directory %s: %w", key, err)
		}
	} else {
		// For files, use the existing Remove method
		if err := f.root.Remove(key); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("key not found: %s", key)
			}
			return fmt.Errorf("failed to delete file %s: %w", key, err)
		}
	}

	return nil
}

// List returns all keys with the given prefix from filesystem
func (f *FSRepository) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	if prefix == "" {
		prefix = "."
	}

	file, err := f.root.Open(prefix)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to open directory %s: %w", prefix, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat directory %s: %w", prefix, err)
	}

	if !info.IsDir() {
		return []string{prefix}, nil
	}

	entries, err := file.Readdir(0)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", prefix, err)
	}

	for _, entry := range entries {
		entryPath := strings.ReplaceAll(filepath.Join(prefix, entry.Name()), "\\", "/")
		keys = append(keys, entryPath)
	}

	return keys, nil
}

// Copy copies data from source key to destination key
func (f *FSRepository) Copy(ctx context.Context, sourceKey string, destKey string) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if f == nil {
		return fmt.Errorf("filesystem repository cannot be nil")
	}
	if sourceKey == "" {
		return fmt.Errorf("source key cannot be empty")
	}
	if destKey == "" {
		return fmt.Errorf("destination key cannot be empty")
	}
	if f.root == nil {
		return fmt.Errorf("root filesystem cannot be nil")
	}

	// Open source file/directory
	sourceFile, err := f.root.Open(sourceKey)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source key not found: %s", sourceKey)
		}
		return fmt.Errorf("failed to open source %s: %w", sourceKey, err)
	}
	defer sourceFile.Close()

	// Check if source is directory
	info, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source %s: %w", sourceKey, err)
	}

	if info.IsDir() {
		// Copy directory recursively
		return f.copyDirectory(ctx, sourceKey, destKey)
	}

	// Create destination directory if needed
	destDir := filepath.Dir(destKey)
	if destDir != "." {
		if err := f.root.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
		}
	}

	// Create destination file
	destFile, err := f.root.Create(destKey)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", destKey, err)
	}
	defer destFile.Close()

	// Copy file content
	buf := make([]byte, 1024)
	for {
		n, err := sourceFile.Read(buf)
		if n > 0 {
			if _, writeErr := destFile.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to destination file %s: %w", destKey, writeErr)
			}
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return fmt.Errorf("failed to read from source file %s: %w", sourceKey, err)
		}
	}

	return nil
}

// copyDirectory recursively copies a directory and its contents
func (f *FSRepository) copyDirectory(ctx context.Context, sourceDir string, destDir string) error {
	// Create destination directory (ignore if already exists)
	if err := f.root.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
	}

	// Open source directory
	sourceFile, err := f.root.Open(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source directory %s: %w", sourceDir, err)
	}
	defer sourceFile.Close()

	// Read directory entries
	entries, err := sourceFile.Readdir(0)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s: %w", sourceDir, err)
	}

	// Copy each entry
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDir, entry.Name())
		destPath := filepath.Join(destDir, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := f.copyDirectory(ctx, sourcePath, destPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := f.Copy(ctx, sourcePath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// Close closes the root filesystem
func (f *FSRepository) Close() error {
	return f.root.Close()
}

// Ensure FSRepository implements StorageRepository interface
var _ ports.StorageRepository = (*FSRepository)(nil)
