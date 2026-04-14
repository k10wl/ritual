package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cespare/xxhash/v2"

	"ritual/internal/core/ports"
)

// FullWorldScanner walks the entire worlds directory and computes xxhash for every file.
// Used when manifest xxhash map is empty (first run, migration).
type FullWorldScanner struct {
	root string
}

var _ ports.DirectoryScanner = (*FullWorldScanner)(nil)

// NewFullWorldScanner creates a scanner that hashes every file in root.
func NewFullWorldScanner(root string) (*FullWorldScanner, error) {
	if root == "" {
		return nil, errors.New("root directory cannot be empty")
	}
	return &FullWorldScanner{root: root}, nil
}

// Scan walks the directory and returns a map of relative paths to xxhash hex strings.
func (s *FullWorldScanner) Scan(ctx context.Context) (map[string]string, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	result := make(map[string]string)

	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			return nil
		}

		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", path, hashErr)
		}

		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, relErr)
		}

		result[filepath.ToSlash(rel)] = hash
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// hashFile computes the xxhash of a file and returns hex string.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := xxhash.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%016x", h.Sum64()), nil
}
