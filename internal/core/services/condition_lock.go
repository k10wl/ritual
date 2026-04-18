package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
)

// ManifestLockCondition error constants
var (
	ErrLockConditionNil      = errors.New("lock condition cannot be nil")
	ErrLockConditionCtxNil   = errors.New("context cannot be nil")
	ErrLockConditionStoreNil = errors.New("manifest store cannot be nil")
	ErrManifestLocked        = errors.New("manifest is locked")
)

// ManifestLockCondition checks if the remote manifest is unlocked.
type ManifestLockCondition struct {
	remote ports.ManifestStore
}

// Compile-time check to ensure ManifestLockCondition implements ports.ConditionService
var _ ports.ConditionService = (*ManifestLockCondition)(nil)

// NewManifestLockCondition creates a new manifest lock condition.
// Takes the remote manifest store directly (no Librarian indirection).
func NewManifestLockCondition(remote ports.ManifestStore) (*ManifestLockCondition, error) {
	if remote == nil {
		return nil, ErrLockConditionStoreNil
	}
	return &ManifestLockCondition{remote: remote}, nil
}

// Check validates that the remote manifest is not locked.
func (c *ManifestLockCondition) Check(ctx context.Context) error {
	if c == nil {
		return ErrLockConditionNil
	}
	if ctx == nil {
		return ErrLockConditionCtxNil
	}

	remoteManifest, err := c.remote.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	if remoteManifest.IsLocked() {
		return fmt.Errorf("%w by %s", ErrManifestLocked, remoteManifest.LockedBy)
	}

	return nil
}
