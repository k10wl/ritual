package adapters

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cespare/xxhash/v2"
)

// matchesAnyGlob returns true when path matches any glob in targets.
// Empty targets means nothing matches.
func matchesAnyGlob(targets []string, path string) bool {
	for _, t := range targets {
		if ok, _ := doublestar.Match(t, path); ok {
			return true
		}
	}
	return false
}

// FullScanner walks a fs.FS and reports Hash+Size per regular file.
type FullScanner struct {
	fsys fs.FS
}

var _ ports.DirectoryScanner = (*FullScanner)(nil)

// NewFullScanner returns a FullScanner backed by fsys.
func NewFullScanner(fsys fs.FS) *FullScanner {
	return &FullScanner{fsys: fsys}
}

// Scan walks the filesystem and returns files matching any glob in targets,
// keyed by relative path. Non-matching files are not opened or hashed.
func (s *FullScanner) Scan(ctx context.Context, targets []string) (map[string]domain.FileEntry, error) {
	result := make(map[string]domain.FileEntry)
	err := fs.WalkDir(s.fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() || path == "." {
			return nil
		}
		if !matchesAnyGlob(targets, path) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", path, infoErr)
		}
		hash, hashErr := hashFSFile(s.fsys, path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", path, hashErr)
		}
		result[path] = domain.FileEntry{Hash: hash, Size: info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// hashFile computes xxhash of a file at an OS path. Used by MtimeScanner.
func hashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is project-scoped scanner input
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := xxhash.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}

func hashFSFile(fsys fs.FS, path string) (string, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := xxhash.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}
