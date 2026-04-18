package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// Retention service sentinel errors.
var (
	ErrRetentionStorageNil = errors.New("retention storage cannot be nil")
	ErrRetentionParseNil   = errors.New("retention parse strategy cannot be nil")
)

// retention is the generic retention engine.
// Storage-agnostic via StorageRepository. Format-agnostic via ParseStrategy.
type retention struct {
	storage ports.StorageRepository
	rules   domain.RetentionRules
	prefix  string
	parse   ParseStrategy
}

var _ ports.RetentionService = (*retention)(nil)

// NewRetention creates a retention service for the given storage, rules, prefix, and parse strategy.
func NewRetention(storage ports.StorageRepository, rules domain.RetentionRules, prefix string, parse ParseStrategy) (ports.RetentionService, error) {
	if storage == nil {
		return nil, ErrRetentionStorageNil
	}
	if parse == nil {
		return nil, ErrRetentionParseNil
	}
	return &retention{storage: storage, rules: rules, prefix: prefix, parse: parse}, nil
}

// Apply lists the prefix, marks expired entries, and deletes them.
func (r *retention) Apply(ctx context.Context) error {
	keys, err := r.storage.List(ctx, r.prefix)
	if err != nil {
		return fmt.Errorf("list %s: %w", r.prefix, err)
	}

	toDelete := Mark(keys, r.rules, r.parse)
	if len(toDelete) == 0 {
		return nil
	}

	if err := r.storage.DeleteBatch(ctx, toDelete); err != nil {
		return fmt.Errorf("delete batch: %w", err)
	}
	return nil
}
