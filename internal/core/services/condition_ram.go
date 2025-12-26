package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
)

// RAM condition error constants
var (
	ErrRAMConditionNil    = errors.New("RAM condition cannot be nil")
	ErrRAMConditionCtxNil = errors.New("context cannot be nil")
	ErrInsufficientRAM    = errors.New("insufficient RAM")
)

// SystemInfoProvider abstracts system information for testability
type SystemInfoProvider interface {
	GetFreeRAMMB() (int, error)
}

// RAMCondition checks if the system has sufficient free RAM
type RAMCondition struct {
	minRAMMB   int
	systemInfo SystemInfoProvider
}

// Compile-time check to ensure RAMCondition implements ports.ConditionService
var _ ports.ConditionService = (*RAMCondition)(nil)

// NewRAMCondition creates a new RAM condition
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewRAMCondition(minRAMMB int, systemInfo SystemInfoProvider) (*RAMCondition, error) {
	if systemInfo == nil {
		return nil, errors.New("system info provider cannot be nil")
	}
	if minRAMMB <= 0 {
		return nil, errors.New("min RAM must be positive")
	}

	condition := &RAMCondition{
		minRAMMB:   minRAMMB,
		systemInfo: systemInfo,
	}

	return condition, nil
}

// Check validates that sufficient free RAM is available
func (c *RAMCondition) Check(ctx context.Context) error {
	if c == nil {
		return ErrRAMConditionNil
	}
	if ctx == nil {
		return ErrRAMConditionCtxNil
	}

	freeRAM, err := c.systemInfo.GetFreeRAMMB()
	if err != nil {
		return fmt.Errorf("failed to get free RAM: %w", err)
	}

	if freeRAM < c.minRAMMB {
		return fmt.Errorf("%w: have %d MB, need %d MB", ErrInsufficientRAM, freeRAM, c.minRAMMB)
	}

	return nil
}
