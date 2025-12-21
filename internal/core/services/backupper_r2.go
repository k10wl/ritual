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
	uploader        streamer.S3StreamUploader
	bucket          string
	workRoot        *os.Root
	worldDirs       []string                   // Directories to archive (relative to instance dir)
	saveLocalBackup bool                       // Whether to also save local backup
	shouldBackup    func() bool                // Condition for local backup
	events          chan<- ports.Event         // Optional: channel for progress events
}

// Compile-time check to ensure R2Backupper implements ports.BackupperService
var _ ports.BackupperService = (*R2Backupper)(nil)

// NewR2Backupper creates a new R2 backupper with streaming support
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewR2Backupper(
	uploader streamer.S3StreamUploader,
	bucket string,
	workRoot *os.Root,
	worldDirs []string,
	saveLocalBackup bool,
	shouldBackup func() bool,
	events chan<- ports.Event,
) (*R2Backupper, error) {
	if uploader == nil {
		return nil, ErrR2BackupperUploaderNil
	}
	if workRoot == nil {
		return nil, ErrR2BackupperWorkRootNil
	}
	if len(worldDirs) == 0 {
		return nil, errors.New("worldDirs cannot be empty")
	}

	backupper := &R2Backupper{
		uploader:        uploader,
		bucket:          bucket,
		workRoot:        workRoot,
		worldDirs:       worldDirs,
		saveLocalBackup: saveLocalBackup,
		shouldBackup:    shouldBackup,
		events:          events,
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
	backupFilename := timestamp + config.BackupExtension

	// World directories to backup (via workRoot for safety)
	var existingDirs []string
	for _, dir := range b.worldDirs {
		relPath := config.InstanceDir + "/" + dir
		if _, err := b.workRoot.Stat(relPath); err == nil {
			// Convert to absolute path for tar archiver
			existingDirs = append(existingDirs, filepath.Join(b.workRoot.Name(), relPath))
		}
	}

	if len(existingDirs) == 0 {
		return "", errors.New("no world directories found")
	}

	// Prepare local path if configured (via workRoot for safety)
	// Evaluate condition early to avoid creating directory unnecessarily
	var localBackupPath string
	doLocalBackup := b.saveLocalBackup && (b.shouldBackup == nil || b.shouldBackup())
	if doLocalBackup {
		// Ensure local backup directory exists
		if err := b.workRoot.Mkdir(config.LocalBackups, 0755); err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("failed to create local backup directory: %w", err)
		}
		localBackupPath = filepath.Join(b.workRoot.Name(), config.LocalBackups, backupFilename)
	}

	// Execute streaming push
	// Note: ShouldBackup already evaluated above, so we pass nil here
	cfg := streamer.PushConfig{
		Dirs:      existingDirs,
		Bucket:    b.bucket,
		Key:       key,
		LocalPath: localBackupPath,
		Events:    b.events,
	}

	_, err := streamer.Push(ctx, cfg, b.uploader)
	if err != nil {
		return "", fmt.Errorf("streaming backup failed: %w", err)
	}

	return key, nil
}
