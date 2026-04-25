package adapters

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"time"
)

// MtimeScanner walks the directory and only computes xxhash for files
// modified after a threshold time. Unchanged files carry forward their entry
// from the previous map.
type MtimeScanner struct {
	root     string
	since    time.Time
	previous map[string]domain.FileEntry
}

var _ ports.DirectoryScanner = (*MtimeScanner)(nil)

// NewMtimeScanner creates a scanner that hashes only recently modified files.
// root: directory path
// since: files modified after this time get re-hashed
// previous: previous file map — carried forward for unchanged files
func NewMtimeScanner(root string, since time.Time, previous map[string]domain.FileEntry) (*MtimeScanner, error) {
	if root == "" {
		return nil, errors.New("root directory cannot be empty")
	}
	if previous == nil {
		previous = map[string]domain.FileEntry{}
	}
	return &MtimeScanner{
		root:     root,
		since:    since,
		previous: previous,
	}, nil
}

// Scan walks the directory and returns a map of relative paths to FileEntry.
// Files modified after since are re-hashed. Others carry forward from previous map.
func (s *MtimeScanner) Scan(ctx context.Context, targets []string) (map[string]domain.FileEntry, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	result := make(map[string]domain.FileEntry)

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

		if !matchesAnyGlob(targets, key) {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", path, infoErr)
		}

		// File modified after threshold → compute fresh hash
		if info.ModTime().After(s.since) {
			hash, hashErr := hashFile(path)
			if hashErr != nil {
				return fmt.Errorf("hashing %s: %w", path, hashErr)
			}
			result[key] = domain.FileEntry{Hash: hash, Size: info.Size()}
			return nil
		}

		// File unchanged and exists in previous map → carry forward
		if prev, exists := s.previous[key]; exists {
			// Refresh size from current stat in case it changed.
			result[key] = domain.FileEntry{Hash: prev.Hash, Size: info.Size()}
			return nil
		}

		// File not in previous map (new file with old mtime) → compute hash
		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", path, hashErr)
		}
		result[key] = domain.FileEntry{Hash: hash, Size: info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
