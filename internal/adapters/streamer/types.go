package streamer

import (
	"context"
	"io"

	"ritual/internal/core/ports"
)

// ConflictStrategy defines how to handle existing files during Pull
type ConflictStrategy int

const (
	Replace ConflictStrategy = iota // Overwrite existing files (default)
	Skip                            // Skip existing files
	Backup                          // Move existing to .bak
	Fail                            // Return error on conflict
)

// PushConfig configures the Push operation
type PushConfig struct {
	Dirs         []string           // Source directories to archive
	Bucket       string             // R2 bucket name
	Key          string             // R2 object key (path/filename.tar.gz)
	LocalPath    string             // Optional: local backup path. Empty = no local backup
	ShouldBackup func() bool        // Condition for local backup. Evaluated once before streaming.
	Events       chan<- ports.Event // Optional: channel for progress events
}

// PullConfig configures the Pull operation
type PullConfig struct {
	Bucket   string                 // R2 bucket name
	Key      string                 // R2 object key
	Dest     string                 // Destination directory
	Conflict ConflictStrategy       // How to handle existing files
	Filter   func(name string) bool // Optional: filter files to extract. nil = extract all
}

// Result contains Push operation results
type Result struct {
	Size      int64  // Total bytes uploaded
	Checksum  string // SHA-256 checksum of the archive
	Key       string // R2 object key
	LocalPath string // Local backup path (empty if backup skipped)
}

// S3StreamUploader interface for R2 streaming uploads
type S3StreamUploader interface {
	// Upload streams content to storage. estimatedSize is optional hint for progress (0 = unknown)
	Upload(ctx context.Context, bucket, key string, body io.Reader, estimatedSize int64) (int64, error)
}

// S3StreamDownloader interface for R2 streaming downloads
type S3StreamDownloader interface {
	Download(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}
