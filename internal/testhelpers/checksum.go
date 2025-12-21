package testhelpers

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
)

// HashDir calculates SHA-256 hash for entire directory structure
func HashDir(path string) ([]byte, error) {
	h := sha256.New()
	var paths []string
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(path, p)
		rel = filepath.ToSlash(rel)
		h.Write([]byte(rel))
		h.Write(data)
	}
	return h.Sum(nil), nil
}

// HashDirs calculates a single combined SHA-256 hash for multiple directories
func HashDirs(paths ...string) ([]byte, error) {
	h := sha256.New()
	for _, path := range paths {
		dirHash, err := HashDir(path)
		if err != nil {
			return nil, err
		}
		h.Write(dirHash)
	}
	return h.Sum(nil), nil
}

// DirPair represents two sets of directory paths to compare
type DirPair struct {
	P1 []string
	P2 []string
}

// CheckDirs compares two sets of directories and returns true if their hashes match
func CheckDirs(data DirPair) (bool, error) {
	hash1, err := HashDirs(data.P1...)
	if err != nil {
		return false, err
	}
	hash2, err := HashDirs(data.P2...)
	if err != nil {
		return false, err
	}
	return string(hash1) == string(hash2), nil
}
