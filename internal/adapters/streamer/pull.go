package streamer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Pull error constants
var (
	ErrPullContextNil    = errors.New("context cannot be nil")
	ErrPullBucketEmpty   = errors.New("bucket cannot be empty")
	ErrPullKeyEmpty      = errors.New("key cannot be empty")
	ErrPullDestEmpty     = errors.New("dest cannot be empty")
	ErrPullDownloaderNil = errors.New("downloader cannot be nil")
	ErrPathTraversal     = errors.New("path traversal detected")
)

// errSkipFile is a sentinel error for skipping files
var errSkipFile = errors.New("skip file")

// Pull downloads and extracts a tar.gz archive from R2
func Pull(ctx context.Context, cfg PullConfig, downloader S3StreamDownloader) error {
	if ctx == nil {
		return ErrPullContextNil
	}
	if cfg.Bucket == "" {
		return ErrPullBucketEmpty
	}
	if cfg.Key == "" {
		return ErrPullKeyEmpty
	}
	if cfg.Dest == "" {
		return ErrPullDestEmpty
	}
	if downloader == nil {
		return ErrPullDownloaderNil
	}

	// Validate destination is safe
	destAbs, err := filepath.Abs(cfg.Dest)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for dest: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destAbs, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Download from R2
	body, err := downloader.Download(ctx, cfg.Bucket, cfg.Key)
	if err != nil {
		return fmt.Errorf("R2 download failed: %w", err)
	}
	defer body.Close()

	// Create gzip reader
	gzReader, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	// Extract files sequentially (tar.Reader is inherently sequential)
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Apply filter if provided
		if cfg.Filter != nil && !cfg.Filter(header.Name) {
			continue
		}

		// Path traversal protection
		targetPath := filepath.Join(destAbs, filepath.FromSlash(header.Name))
		if !isPathSafe(destAbs, targetPath) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, header.Name)
		}

		// Handle conflict strategy for existing files
		err = handleConflict(targetPath, header, cfg.Conflict)
		if err == errSkipFile {
			continue
		}
		if err != nil {
			return err
		}

		// Extract based on type
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := extractFile(targetPath, tarReader, header); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", targetPath, err)
			}
		}
	}

	return nil
}

// isPathSafe validates that targetPath is within baseDir (path traversal protection)
func isPathSafe(baseDir, targetPath string) bool {
	if baseDir == "" || targetPath == "" {
		return false
	}

	// Clean both paths
	baseDir = filepath.Clean(baseDir)
	targetPath = filepath.Clean(targetPath)

	// Check for ".." components in the target
	if strings.Contains(targetPath, "..") {
		return false
	}

	// Verify target is under base
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}

	// Relative path should not start with ".."
	return !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// handleConflict applies the conflict strategy for existing files
func handleConflict(path string, header *tar.Header, strategy ConflictStrategy) error {
	if header == nil {
		return errors.New("header cannot be nil")
	}

	// Skip conflict check for directories
	if header.Typeflag == tar.TypeDir {
		return nil
	}

	// Check if file exists
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil // No conflict
	}
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	// File exists - apply strategy
	switch strategy {
	case Replace:
		return nil // Will overwrite
	case Skip:
		return errSkipFile
	case Backup:
		backupPath := path + ".bak"
		if err := os.Rename(path, backupPath); err != nil {
			return fmt.Errorf("failed to backup %s: %w", path, err)
		}
		return nil
	case Fail:
		return fmt.Errorf("file already exists: %s", path)
	default:
		return nil // Default to Replace
	}
}

// extractFile extracts a single file from tar to disk
func extractFile(path string, reader io.Reader, header *tar.Header) error {
	if reader == nil {
		return errors.New("reader cannot be nil")
	}
	if header == nil {
		return errors.New("header cannot be nil")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create file
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy content
	if _, err := io.Copy(file, reader); err != nil {
		os.Remove(path) // Cleanup partial file
		return err
	}

	return nil
}
