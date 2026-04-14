package adapters

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/cespare/xxhash/v2"
	"ritual/internal/core/ports"
)

type FullScanner struct {
	fsys fs.FS
}

var _ ports.DirectoryScanner = (*FullScanner)(nil)

func NewFullScanner(fsys fs.FS) *FullScanner {
	return &FullScanner{fsys: fsys}
}

func (s *FullScanner) Scan(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)
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
		hash, hashErr := hashFSFile(s.fsys, path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", path, hashErr)
		}
		result[path] = hash
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// hashFile computes xxhash of a file at an OS path. Used by MtimeScanner.
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

func hashFSFile(fsys fs.FS, path string) (string, error) {
	f, err := fsys.Open(path)
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
