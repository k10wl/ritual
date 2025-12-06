package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ritual/internal/adapters/streamer"
	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// R2Backupper error constants
var (
	ErrR2BackupperUploaderNil      = errors.New("uploader cannot be nil")
	ErrR2BackupperRemoteStorageNil = errors.New("remote storage repository cannot be nil")
	ErrR2BackupperWorkRootNil      = errors.New("workRoot cannot be nil")
	ErrR2BackupperNil              = errors.New("R2 backupper cannot be nil")
)

// R2Backupper implements BackupperService for R2 backup storage with streaming
type R2Backupper struct {
	uploader      streamer.S3StreamUploader
	remoteStorage ports.StorageRepository
	bucket        string
	workRoot      *os.Root
	localPath     string      // Optional local backup path
	shouldBackup  func() bool // Condition for local backup
}

// Compile-time check to ensure R2Backupper implements ports.BackupperService
var _ ports.BackupperService = (*R2Backupper)(nil)

// NewR2Backupper creates a new R2 backupper with streaming support
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewR2Backupper(
	uploader streamer.S3StreamUploader,
	remoteStorage ports.StorageRepository,
	bucket string,
	workRoot *os.Root,
	localPath string,
	shouldBackup func() bool,
) (*R2Backupper, error) {
	if uploader == nil {
		return nil, ErrR2BackupperUploaderNil
	}
	if remoteStorage == nil {
		return nil, ErrR2BackupperRemoteStorageNil
	}
	if workRoot == nil {
		return nil, ErrR2BackupperWorkRootNil
	}

	backupper := &R2Backupper{
		uploader:      uploader,
		remoteStorage: remoteStorage,
		bucket:        bucket,
		workRoot:      workRoot,
		localPath:     localPath,
		shouldBackup:  shouldBackup,
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
	}

	_, err := streamer.Push(ctx, cfg, b.uploader)
	if err != nil {
		return "", fmt.Errorf("streaming backup failed: %w", err)
	}

	// Apply retention policy
	if err := b.applyRetention(ctx); err != nil {
		return "", fmt.Errorf("retention policy failed: %w", err)
	}

	return key, nil
}

// applyRetention removes old backups exceeding the limit
func (b *R2Backupper) applyRetention(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// List all backups
	keys, err := b.remoteStorage.List(ctx, config.RemoteBackups)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// Static bounds check
	if len(keys) > config.MaxFiles {
		return fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), config.MaxFiles)
	}

	// Filter valid backup files
	var backups []string
	for _, key := range keys {
		if strings.HasSuffix(key, config.BackupExtension) {
			backups = append(backups, key)
		}
	}

	// Sort by key (timestamp in name, newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i] > backups[j]
	})

	// Delete excess backups
	if len(backups) > config.R2MaxBackups {
		for _, key := range backups[config.R2MaxBackups:] {
			if err := b.remoteStorage.Delete(ctx, key); err != nil {
				return fmt.Errorf("failed to delete old backup %s: %w", key, err)
			}
		}
	}

	return nil
}
