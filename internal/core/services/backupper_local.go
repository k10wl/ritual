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
	workRoot  *os.Root
	worldDirs []string           // Directories to archive (relative to instance dir)
	events    chan<- ports.Event // Optional: channel for progress events
}

// Compile-time check to ensure LocalBackupper implements ports.BackupperService
var _ ports.BackupperService = (*LocalBackupper)(nil)

// NewLocalBackupper creates a new local backupper with streaming support
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewLocalBackupper(workRoot *os.Root, worldDirs []string, events chan<- ports.Event) (*LocalBackupper, error) {
	if workRoot == nil {
		return nil, ErrLocalBackupperWorkRootNil
	}
	if len(worldDirs) == 0 {
		return nil, errors.New("worldDirs cannot be empty")
	}

	backupper := &LocalBackupper{
		workRoot:  workRoot,
		worldDirs: worldDirs,
		events:    events,
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
	var existingDirs []string
	for _, dir := range b.worldDirs {
		fullPath := filepath.Join(rootPath, config.InstanceDir, dir)
		if _, err := os.Stat(fullPath); err == nil {
			existingDirs = append(existingDirs, fullPath)
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
		Events: b.events,
	}

	_, err = streamer.Push(ctx, cfg, localWriter)
	if err != nil {
		return "", fmt.Errorf("streaming backup failed: %w", err)
	}

	// Return full path relative to workRoot for manifest tracking
	return config.LocalBackups + "/" + backupName, nil
}
