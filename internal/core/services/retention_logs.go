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

// LogRetention error constants
var (
	ErrLogRetentionStorageNil = errors.New("local storage repository cannot be nil")
	ErrLogRetentionNil        = errors.New("log retention cannot be nil")
)

// LogRetention implements RetentionService for log files
type LogRetention struct {
	localStorage ports.StorageRepository
	bus          ports.EventBus
}

// Compile-time check to ensure LogRetention implements ports.RetentionService
var _ ports.RetentionService = (*LogRetention)(nil)

// NewLogRetention creates a new log retention service
func NewLogRetention(localStorage ports.StorageRepository, bus ports.EventBus) (*LogRetention, error) {
	if localStorage == nil {
		return nil, ErrLogRetentionStorageNil
	}

	return &LogRetention{
		localStorage: localStorage,
		bus:          bus,
	}, nil
}

// publish emits an event on the bus (nil-safe).
func (r *LogRetention) publish(evt ports.Event) {
	if r.bus != nil {
		r.bus.Publish(evt)
	}
}

// Apply removes old log files exceeding the retention limit
func (r *LogRetention) Apply(ctx context.Context) error {
	if r == nil {
		return ErrLogRetentionNil
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	// List all log files
	keys, err := r.localStorage.List(ctx, config.LogsDir)
	if err != nil {
		// If logs dir doesn't exist yet, nothing to clean
		return nil
	}

	// Filter only .log files
	var logFiles []string
	for _, key := range keys {
		if strings.HasSuffix(key, config.LogExtension) {
			logFiles = append(logFiles, key)
		}
	}

	// If within limit, nothing to do
	if len(logFiles) <= config.MaxLogFiles {
		return nil
	}

	// Sort by filename (timestamp in name, newest first)
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i] > logFiles[j]
	})

	// Delete oldest logs exceeding limit
	toDelete := logFiles[config.MaxLogFiles:]

	r.publish(ports.UpdateInfo{Operation: "retention", Message: "Applying log retention policy", Data: map[string]any{
		"total":       len(logFiles),
		"max_allowed": config.MaxLogFiles,
		"to_delete":   len(toDelete),
	}})

	for _, key := range toDelete {
		r.publish(ports.UpdateInfo{Operation: "retention", Message: "Deleting old log", Data: map[string]any{"key": key}})
		if err := r.localStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to delete log %s: %w", key, err)
		}
	}

	return nil
}
