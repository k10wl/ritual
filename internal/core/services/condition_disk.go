package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
)

// Disk condition error constants
var (
	ErrDiskConditionNil    = errors.New("disk condition cannot be nil")
	ErrDiskConditionCtxNil = errors.New("context cannot be nil")
	ErrInsufficientDisk    = errors.New("insufficient disk space")
)

// DiskInfoProvider abstracts disk information for testability
type DiskInfoProvider interface {
	GetFreeDiskMB(path string) (int, error)
}

// DiskSpaceCondition checks if the system has sufficient free disk space
type DiskSpaceCondition struct {
	minDiskMB int
	path      string
	diskInfo  DiskInfoProvider
}

// Compile-time check to ensure DiskSpaceCondition implements ports.ConditionService
var _ ports.ConditionService = (*DiskSpaceCondition)(nil)

// NewDiskSpaceCondition creates a new disk space condition
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewDiskSpaceCondition(minDiskMB int, path string, diskInfo DiskInfoProvider) (*DiskSpaceCondition, error) {
	if diskInfo == nil {
		return nil, errors.New("disk info provider cannot be nil")
	}
	if minDiskMB <= 0 {
		return nil, errors.New("min disk space must be positive")
	}
	if path == "" {
		return nil, errors.New("path cannot be empty")
	}

	condition := &DiskSpaceCondition{
		minDiskMB: minDiskMB,
		path:      path,
		diskInfo:  diskInfo,
	}

	return condition, nil
}

// Check validates that sufficient free disk space is available
func (c *DiskSpaceCondition) Check(ctx context.Context) error {
	if c == nil {
		return ErrDiskConditionNil
	}
	if ctx == nil {
		return ErrDiskConditionCtxNil
	}

	freeDisk, err := c.diskInfo.GetFreeDiskMB(c.path)
	if err != nil {
		return fmt.Errorf("failed to get free disk space: %w", err)
	}

	if freeDisk < c.minDiskMB {
		return fmt.Errorf("%w: have %d MB, need %d MB", ErrInsufficientDisk, freeDisk, c.minDiskMB)
	}

	return nil
}
