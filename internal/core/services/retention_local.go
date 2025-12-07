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

// LocalRetention error constants
var (
	ErrLocalRetentionStorageNil = errors.New("local storage repository cannot be nil")
	ErrLocalRetentionNil        = errors.New("local retention cannot be nil")
)

// LocalRetention implements RetentionService for local backup storage
type LocalRetention struct {
	localStorage ports.StorageRepository
}

// Compile-time check to ensure LocalRetention implements ports.RetentionService
var _ ports.RetentionService = (*LocalRetention)(nil)

// NewLocalRetention creates a new local retention service
func NewLocalRetention(localStorage ports.StorageRepository) (*LocalRetention, error) {
	if localStorage == nil {
		return nil, ErrLocalRetentionStorageNil
	}

	return &LocalRetention{
		localStorage: localStorage,
	}, nil
}

// Apply removes old local backups exceeding the retention limit
// Keeps only backups that are in manifest's StoredWorlds, up to LocalMaxBackups
func (r *LocalRetention) Apply(ctx context.Context, manifest *domain.Manifest) error {
	if r == nil {
		return ErrLocalRetentionNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if manifest == nil {
		return errors.New("manifest cannot be nil")
	}

	// List all local backups
	keys, err := r.localStorage.List(ctx, config.LocalBackups)
	if err != nil {
		return fmt.Errorf("failed to list local backups: %w", err)
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

	// Filter valid backup files
	var backups []string
	for _, key := range keys {
		if strings.HasSuffix(key, config.BackupExtension) {
			// Skip temp files
			if strings.Contains(key, "temp_") {
				continue
			}
			backups = append(backups, key)
		}
	}

	// Sort by filename (timestamp in name, newest first)
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
			slog.Info("Found dangling local backup", "key", key)
			toDelete = append(toDelete, key)
		} else {
			validBackups = append(validBackups, key)
		}
	}

	// Second pass: apply retention limit to valid backups
	if len(validBackups) > config.LocalMaxBackups {
		slog.Info("Applying local retention policy",
			"total_valid", len(validBackups),
			"max_allowed", config.LocalMaxBackups,
			"to_delete", len(validBackups)-config.LocalMaxBackups)
		toDelete = append(toDelete, validBackups[config.LocalMaxBackups:]...)
	}

	// Delete identified backups
	for _, key := range toDelete {
		slog.Info("Deleting local backup", "key", key)
		if err := r.localStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to delete local backup %s: %w", key, err)
		}
	}

	return nil
}
