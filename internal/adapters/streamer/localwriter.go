package streamer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalFileWriter implements S3StreamUploader for local filesystem writes
// It writes streamed data directly to a local file
type LocalFileWriter struct {
	basePath string
}

// LocalFileWriter error constants
var (
	ErrLocalWriterBasePathEmpty = errors.New("base path cannot be empty")
	ErrLocalWriterNil           = errors.New("local file writer cannot be nil")
)

// NewLocalFileWriter creates a new local file writer
func NewLocalFileWriter(basePath string) (*LocalFileWriter, error) {
	if basePath == "" {
		return nil, ErrLocalWriterBasePathEmpty
	}

	return &LocalFileWriter{
		basePath: basePath,
	}, nil
}

// Upload writes the streamed content to a local file
// The key parameter is used as the relative path from basePath
// bucket parameter is ignored for local writes
// estimatedSize is ignored for local writes
func (w *LocalFileWriter) Upload(ctx context.Context, bucket, key string, body io.Reader, estimatedSize int64) (int64, error) {
	if ctx == nil {
		return 0, errors.New("context cannot be nil")
	}
	if w == nil {
		return 0, ErrLocalWriterNil
	}
	if body == nil {
		return 0, errors.New("body cannot be nil")
	}

	// Construct full path
	fullPath := filepath.Join(w.basePath, key)

	// Ensure parent directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create file
	file, err := os.Create(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file %s: %w", fullPath, err)
	}
	defer file.Close()

	// Copy with context cancellation check
	written, err := copyWithContext(ctx, file, body)
	if err != nil {
		os.Remove(fullPath) // Cleanup partial file
		return written, fmt.Errorf("failed to write file: %w", err)
	}

	return written, nil
}

// copyWithContext copies from src to dst, checking for context cancellation
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	if ctx == nil {
		return 0, errors.New("context cannot be nil")
	}

	buf := make([]byte, 32*1024) // 32 KB buffer
	var written int64

	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, readErr := src.Read(buf)
		if nr > 0 {
			nw, writeErr := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if writeErr != nil {
				return written, writeErr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

var _ S3StreamUploader = (*LocalFileWriter)(nil)
