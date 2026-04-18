package adapters

import (
	"context"
	"errors"
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
	name string
}

// NewFSRepository creates a new filesystem storage repository.
// Optional name is the human-readable initial path used in observability
// events (e.g. "./worlds"). When omitted, label falls back to "fs::".
func NewFSRepository(root *os.Root, name ...string) (*FSRepository, error) {
	if root == nil {
		return nil, errors.New("root cannot be nil")
	}

	n := ""
	if len(name) > 0 {
		n = name[0]
	}

	return &FSRepository{
		root: root,
		name: n,
	}, nil
}

// String returns adapter label for observability events: "fs::<name>".
func (f *FSRepository) String() string {
	return "fs::" + f.name
}

// Rename moves a key to a new location atomically when supported by the
// underlying filesystem. Falls back to copy + delete on cross-device errors.
func (f *FSRepository) Rename(ctx context.Context, sourceKey string, destKey string) error {
	sourceKey = filepath.FromSlash(sourceKey)
	destKey = filepath.FromSlash(destKey)
	if _, err := f.root.Stat(sourceKey); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source key not found: %s", sourceKey)
		}
		return fmt.Errorf("failed to stat source %s: %w", sourceKey, err)
	}
	destDir := filepath.Dir(destKey)
	if destDir != "." {
		if err := f.root.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
		}
	}
	if err := f.root.Rename(sourceKey, destKey); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", sourceKey, destKey, err)
	}
	return nil
}

// Get retrieves data by key from filesystem
func (f *FSRepository) Get(ctx context.Context, key string) ([]byte, error) {
	key = filepath.FromSlash(key)
	data, err := f.root.ReadFile(key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key not found: %s", key)
		}
		return nil, fmt.Errorf("failed to read file %s: %w", key, err)
	}
	return data, nil
}

// Put stores data with the given key to filesystem
func (f *FSRepository) Put(ctx context.Context, key string, data []byte) error {
	key = filepath.FromSlash(key)
	dir := filepath.Dir(key)
	if dir != "." {
		if err := f.root.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	if err := f.root.WriteFile(key, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", key, err)
	}
	return nil
}

// Delete removes data by key from filesystem
func (f *FSRepository) Delete(ctx context.Context, key string) error {
	key = filepath.FromSlash(key)
	if _, err := f.root.Stat(key); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to stat %s: %w", key, err)
	}
	if err := f.root.RemoveAll(key); err != nil {
		return fmt.Errorf("failed to delete %s: %w", key, err)
	}
	return nil
}

// List returns all keys with the given prefix from filesystem
func (f *FSRepository) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	if prefix == "" {
		prefix = "."
	} else {
		prefix = filepath.FromSlash(prefix)
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

	sourceKey = filepath.FromSlash(sourceKey)
	destKey = filepath.FromSlash(destKey)

	info, err := f.root.Stat(sourceKey)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source key not found: %s", sourceKey)
		}
		return fmt.Errorf("failed to stat source %s: %w", sourceKey, err)
	}

	if info.IsDir() {
		return f.copyDir(ctx, sourceKey, destKey)
	}

	data, err := f.root.ReadFile(sourceKey)
	if err != nil {
		return fmt.Errorf("failed to read source %s: %w", sourceKey, err)
	}

	destDir := filepath.Dir(destKey)
	if destDir != "." {
		if err := f.root.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
		}
	}

	return f.root.WriteFile(destKey, data, 0644)
}

// copyDir recursively copies a directory
func (f *FSRepository) copyDir(ctx context.Context, sourceDir string, destDir string) error {
	if err := f.root.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
	}

	dir, err := f.root.Open(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to open source directory %s: %w", sourceDir, err)
	}
	defer dir.Close()

	entries, err := dir.Readdir(0)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s: %w", sourceDir, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(sourceDir, entry.Name())
		dstPath := filepath.Join(destDir, entry.Name())
		if err := f.Copy(ctx, srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// DeleteBatch removes multiple keys in a single operation
func (f *FSRepository) DeleteBatch(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := f.Delete(ctx, key); err != nil {
			return err
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
