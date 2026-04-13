package adapters

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"ritual/internal/core/ports"
)

// MtimeWorldScanner walks the worlds directory and only computes xxhash for files
// modified after a threshold time. Unchanged files carry forward their hash from
// the previous map.
type MtimeWorldScanner struct {
	root     string
	since    time.Time
	previous map[string]string
}

var _ ports.WorldScanner = (*MtimeWorldScanner)(nil)

// NewMtimeWorldScanner creates a scanner that hashes only recently modified files.
// root: worlds directory path
// since: files modified after this time get re-hashed
// previous: previous xxhash map — carried forward for unchanged files
func NewMtimeWorldScanner(root string, since time.Time, previous map[string]string) (*MtimeWorldScanner, error) {
	if root == "" {
		return nil, errors.New("root directory cannot be empty")
	}
	if previous == nil {
		previous = map[string]string{}
	}
	return &MtimeWorldScanner{
		root:     root,
		since:    since,
		previous: previous,
	}, nil
}

// Scan walks the directory and returns a map of relative paths to xxhash hex strings.
// Files modified after since are re-hashed. Others carry forward from previous map.
func (s *MtimeWorldScanner) Scan(ctx context.Context) (map[string]string, error) {
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

		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, relErr)
		}
		key := filepath.ToSlash(rel)

		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", path, infoErr)
		}

		// File modified after threshold or not in previous map → compute fresh hash
		if info.ModTime().After(s.since) {
			hash, hashErr := hashFile(path)
			if hashErr != nil {
				return fmt.Errorf("hashing %s: %w", path, hashErr)
			}
			result[key] = hash
			return nil
		}

		// File unchanged and exists in previous map → carry forward
		if prevHash, exists := s.previous[key]; exists {
			result[key] = prevHash
			return nil
		}

		// File not in previous map (new file with old mtime) → compute hash
		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", path, hashErr)
		}
		result[key] = hash
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
