package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ritual/internal/config"
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
	events        chan<- ports.Event
}

// Compile-time check to ensure R2Retention implements ports.RetentionService
var _ ports.RetentionService = (*R2Retention)(nil)

// NewR2Retention creates a new R2 retention service
func NewR2Retention(remoteStorage ports.StorageRepository, events chan<- ports.Event) (*R2Retention, error) {
	if remoteStorage == nil {
		return nil, ErrR2RetentionStorageNil
	}

	return &R2Retention{
		remoteStorage: remoteStorage,
		events:        events,
	}, nil
}

// send safely sends an event to the channel
func (r *R2Retention) send(evt ports.Event) {
	ports.SendEvent(r.events, evt)
}

// Apply removes old R2 backups exceeding the retention limit
// TODO(Task 17): this implementation will be replaced; manifest logic removed temporarily
func (r *R2Retention) Apply(ctx context.Context) error {
	if r == nil {
		return ErrR2RetentionNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
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

	// Filter backup entries by timestamp (skip manual and temp files, ignore invalid names)
	var backups []string
	for _, key := range keys {
		if strings.Contains(key, config.ManualWorldFilename) || strings.Contains(key, "temp_") {
			continue
		}
		if extractTimestamp(key) != "" {
			backups = append(backups, key)
		}
	}

	// Sort by key (timestamp in name, newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i] > backups[j]
	})

	// Apply retention limit
	var toDelete []string
	if len(backups) > config.R2MaxBackups {
		r.send(ports.UpdateEvent{Operation: "retention", Message: "Applying R2 retention policy", Data: map[string]any{
			"total":       len(backups),
			"max_allowed": config.R2MaxBackups,
			"to_delete":   len(backups) - config.R2MaxBackups,
		}})
		toDelete = backups[config.R2MaxBackups:]
	}

	// Delete identified backups
	for _, key := range toDelete {
		r.send(ports.UpdateEvent{Operation: "retention", Message: "Deleting R2 backup", Data: map[string]any{"key": key}})
		if err := r.remoteStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to delete R2 backup %s: %w", key, err)
		}
	}

	return nil
}
