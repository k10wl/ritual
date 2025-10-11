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
	if err := f.root.Remove(key); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to delete file %s: %w", key, err)
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
		if !entry.IsDir() {
			keys = append(keys, strings.ReplaceAll(filepath.Join(prefix, entry.Name()), "\\", "/"))
		}
	}

	return keys, nil
}

// Close closes the root filesystem
func (f *FSRepository) Close() error {
	return f.root.Close()
}

// Ensure FSRepository implements StorageRepository interface
var _ ports.StorageRepository = (*FSRepository)(nil)
