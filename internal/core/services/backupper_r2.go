package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ritual/internal/adapters/streamer"
	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// R2Backupper error constants
var (
	ErrR2BackupperUploaderNil = errors.New("uploader cannot be nil")
	ErrR2BackupperWorkRootNil = errors.New("workRoot cannot be nil")
	ErrR2BackupperNil         = errors.New("R2 backupper cannot be nil")
)

// R2Backupper implements BackupperService for R2 backup storage with streaming
type R2Backupper struct {
	uploader     streamer.S3StreamUploader
	bucket       string
	workRoot     *os.Root
	localPath    string             // Optional local backup path
	shouldBackup func() bool        // Condition for local backup
	events       chan<- ports.Event // Optional: channel for progress events
}

// Compile-time check to ensure R2Backupper implements ports.BackupperService
var _ ports.BackupperService = (*R2Backupper)(nil)

// NewR2Backupper creates a new R2 backupper with streaming support
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewR2Backupper(
	uploader streamer.S3StreamUploader,
	bucket string,
	workRoot *os.Root,
	localPath string,
	shouldBackup func() bool,
	events chan<- ports.Event,
) (*R2Backupper, error) {
	if uploader == nil {
		return nil, ErrR2BackupperUploaderNil
	}
	if workRoot == nil {
		return nil, ErrR2BackupperWorkRootNil
	}

	backupper := &R2Backupper{
		uploader:     uploader,
		bucket:       bucket,
		workRoot:     workRoot,
		localPath:    localPath,
		shouldBackup: shouldBackup,
		events:       events,
	}

	// Postcondition assertion
	if backupper == nil {
		return nil, errors.New("R2 backupper initialization failed")
	}

	return backupper, nil
}

// Run executes the streaming backup process
// Returns the archive key for manifest updates
func (b *R2Backupper) Run(ctx context.Context) (string, error) {
	if b == nil {
		return "", ErrR2BackupperNil
	}
	if ctx == nil {
		return "", errors.New("context cannot be nil")
	}

	// Generate backup key based on timestamp
	timestamp := time.Now().Format(config.TimestampFormat)
	key := config.RemoteBackups + "/" + timestamp + config.BackupExtension

	// World directories to backup (relative to workRoot)
	rootPath := b.workRoot.Name()
	worldDirs := make([]string, len(config.WorldDirs))
	for i, dir := range config.WorldDirs {
		worldDirs[i] = filepath.Join(rootPath, config.InstanceDir, dir)
	}

	// Filter to only existing directories
	var existingDirs []string
	for _, dir := range worldDirs {
		if _, err := os.Stat(dir); err == nil {
			existingDirs = append(existingDirs, dir)
		}
	}

	if len(existingDirs) == 0 {
		return "", errors.New("no world directories found")
	}

	// Prepare local path if configured
	var localBackupPath string
	if b.localPath != "" {
		localBackupPath = filepath.Join(b.localPath, timestamp+config.BackupExtension)
	}

	// Execute streaming push
	cfg := streamer.PushConfig{
		Dirs:         existingDirs,
		Bucket:       b.bucket,
		Key:          key,
		LocalPath:    localBackupPath,
		ShouldBackup: b.shouldBackup,
		Events:       b.events,
	}

	_, err := streamer.Push(ctx, cfg, b.uploader)
	if err != nil {
		return "", fmt.Errorf("streaming backup failed: %w", err)
	}

	return key, nil
}
