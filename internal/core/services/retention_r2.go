package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// R2Retention error constants
var (
	ErrR2RetentionStorageNil = errors.New("remote storage repository cannot be nil")
	ErrR2RetentionNil        = errors.New("R2 retention cannot be nil")
)

// R2Retention implements RetentionService for R2 backup storage
type R2Retention struct {
	remoteStorage ports.StorageRepository
}

// Compile-time check to ensure R2Retention implements ports.RetentionService
var _ ports.RetentionService = (*R2Retention)(nil)

// NewR2Retention creates a new R2 retention service
func NewR2Retention(remoteStorage ports.StorageRepository) (*R2Retention, error) {
	if remoteStorage == nil {
		return nil, ErrR2RetentionStorageNil
	}

	return &R2Retention{
		remoteStorage: remoteStorage,
	}, nil
}

// Apply removes old R2 backups exceeding the retention limit
// Keeps only backups that are in manifest's StoredWorlds, up to R2MaxBackups
func (r *R2Retention) Apply(ctx context.Context, manifest *domain.Manifest) error {
	if r == nil {
		return ErrR2RetentionNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if manifest == nil {
		return errors.New("manifest cannot be nil")
	}

	// List all R2 backups
	keys, err := r.remoteStorage.List(ctx, config.RemoteBackups)
	if err != nil {
		return fmt.Errorf("failed to list R2 backups: %w", err)
	}

	// Static bounds check
	if len(keys) > config.MaxFiles {
		return fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), config.MaxFiles)
	}

	// Build set of valid URIs from manifest
	validURIs := make(map[string]bool)
	for _, world := range manifest.StoredWorlds {
		validURIs[world.URI] = true
	}

	// Filter valid backup files (exclude manual.tar.gz and temp files)
	var backups []string
	for _, key := range keys {
		if strings.HasSuffix(key, config.BackupExtension) {
			// Skip manual world file and temp files
			if strings.Contains(key, config.ManualWorldFilename) || strings.Contains(key, "temp_") {
				continue
			}
			backups = append(backups, key)
		}
	}

	// Sort by key (timestamp in name, newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i] > backups[j]
	})

	// Identify backups to delete:
	// 1. Dangling backups (not in manifest)
	// 2. Excess backups beyond retention limit
	var toDelete []string

	// First pass: identify dangling backups
	var validBackups []string
	for _, key := range backups {
		if !validURIs[key] {
			// Dangling backup - not in manifest
			slog.Info("Found dangling R2 backup", "key", key)
			toDelete = append(toDelete, key)
		} else {
			validBackups = append(validBackups, key)
		}
	}

	// Second pass: apply retention limit to valid backups
	if len(validBackups) > config.R2MaxBackups {
		slog.Info("Applying R2 retention policy",
			"total_valid", len(validBackups),
			"max_allowed", config.R2MaxBackups,
			"to_delete", len(validBackups)-config.R2MaxBackups)
		toDelete = append(toDelete, validBackups[config.R2MaxBackups:]...)
	}

	// Delete identified backups
	for _, key := range toDelete {
		slog.Info("Deleting R2 backup", "key", key)
		if err := r.remoteStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to delete R2 backup %s: %w", key, err)
		}
	}

	return nil
}
