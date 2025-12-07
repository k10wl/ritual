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

// LocalBackupper error constants
var (
	ErrLocalBackupperWorkRootNil = errors.New("workRoot cannot be nil")
	ErrLocalBackupperNil         = errors.New("local backupper cannot be nil")
)

// LocalBackupper implements BackupperService for local backup storage with streaming
type LocalBackupper struct {
	workRoot *os.Root
}

// Compile-time check to ensure LocalBackupper implements ports.BackupperService
var _ ports.BackupperService = (*LocalBackupper)(nil)

// NewLocalBackupper creates a new local backupper with streaming support
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewLocalBackupper(workRoot *os.Root) (*LocalBackupper, error) {
	if workRoot == nil {
		return nil, ErrLocalBackupperWorkRootNil
	}

	backupper := &LocalBackupper{
		workRoot: workRoot,
	}

	// Postcondition assertion
	if backupper == nil {
		return nil, errors.New("local backupper initialization failed")
	}

	return backupper, nil
}

// Run executes the streaming backup process
// Returns the archive path (relative to workRoot) for manifest updates
func (b *LocalBackupper) Run(ctx context.Context) (string, error) {
	if b == nil {
		return "", ErrLocalBackupperNil
	}
	if ctx == nil {
		return "", errors.New("context cannot be nil")
	}

	// Generate backup name based on timestamp
	timestamp := time.Now().Format(config.TimestampFormat)
	backupName := timestamp + config.BackupExtension

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

	// Create local file writer for the backup directory
	backupDir := filepath.Join(rootPath, config.LocalBackups)
	localWriter, err := streamer.NewLocalFileWriter(backupDir)
	if err != nil {
		return "", fmt.Errorf("failed to create local writer: %w", err)
	}

	// Execute streaming push (key is just the filename since basePath is backupDir)
	cfg := streamer.PushConfig{
		Dirs:   existingDirs,
		Bucket: "local", // Not used by LocalFileWriter but required by Push
		Key:    backupName,
	}

	_, err = streamer.Push(ctx, cfg, localWriter)
	if err != nil {
		return "", fmt.Errorf("streaming backup failed: %w", err)
	}

	// Return full path relative to workRoot for manifest tracking
	return config.LocalBackups + "/" + backupName, nil
}
