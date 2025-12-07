package services

import (
	"context"
	"errors"
	"fmt"
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
	events       chan<- ports.Event
}

// Compile-time check to ensure LocalRetention implements ports.RetentionService
var _ ports.RetentionService = (*LocalRetention)(nil)

// NewLocalRetention creates a new local retention service
func NewLocalRetention(localStorage ports.StorageRepository, events chan<- ports.Event) (*LocalRetention, error) {
	if localStorage == nil {
		return nil, ErrLocalRetentionStorageNil
	}

	return &LocalRetention{
		localStorage: localStorage,
		events:       events,
	}, nil
}

// send safely sends an event to the channel
func (r *LocalRetention) send(evt ports.Event) {
	ports.SendEvent(r.events, evt)
}

// Apply removes old local backups exceeding the retention limit
// Keeps only the latest LocalMaxBackups files regardless of manifest
func (r *LocalRetention) Apply(ctx context.Context, manifest *domain.Manifest) error {
	if r == nil {
		return ErrLocalRetentionNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	// manifest parameter kept for interface compatibility but not used

	// List all local backups
	keys, err := r.localStorage.List(ctx, config.LocalBackups)
	if err != nil {
		return fmt.Errorf("failed to list local backups: %w", err)
	}

	// Static bounds check
	if len(keys) > config.MaxFiles {
		return fmt.Errorf("too many backup files: %d exceeds limit %d", len(keys), config.MaxFiles)
	}

	// Filter backup files (skip temp files)
	var backups []string
	for _, key := range keys {
		if strings.HasSuffix(key, config.BackupExtension) {
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

	// Delete excess backups beyond retention limit
	if len(backups) > config.LocalMaxBackups {
		r.send(ports.UpdateEvent{Operation: "retention", Message: "Applying local retention policy", Data: map[string]any{
			"total":       len(backups),
			"max_allowed": config.LocalMaxBackups,
			"to_delete":   len(backups) - config.LocalMaxBackups,
		}})

		for _, key := range backups[config.LocalMaxBackups:] {
			r.send(ports.UpdateEvent{Operation: "retention", Message: "Deleting local backup", Data: map[string]any{"key": key}})
			if err := r.localStorage.Delete(ctx, key); err != nil {
				return fmt.Errorf("failed to delete local backup %s: %w", key, err)
			}
		}
	}

	return nil
}
