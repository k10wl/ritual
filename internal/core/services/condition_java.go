package services

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
)

// Java condition error constants
var (
	ErrJavaConditionNil    = errors.New("Java condition cannot be nil")
	ErrJavaConditionCtxNil = errors.New("context cannot be nil")
	ErrJavaNotFound        = errors.New("Java not found")
	ErrJavaVersionTooOld   = errors.New("Java version too old")
)

// JavaVersionProvider abstracts Java version detection for testability
type JavaVersionProvider interface {
	GetJavaVersion() (int, error)
}

// JavaVersionCondition checks if the system has a compatible Java version
type JavaVersionCondition struct {
	minVersion int
	javaInfo   JavaVersionProvider
}

// Compile-time check to ensure JavaVersionCondition implements ports.ConditionService
var _ ports.ConditionService = (*JavaVersionCondition)(nil)

// NewJavaVersionCondition creates a new Java version condition
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewJavaVersionCondition(minVersion int, javaInfo JavaVersionProvider) (*JavaVersionCondition, error) {
	if javaInfo == nil {
		return nil, errors.New("Java info provider cannot be nil")
	}
	if minVersion <= 0 {
		return nil, errors.New("min Java version must be positive")
	}

	condition := &JavaVersionCondition{
		minVersion: minVersion,
		javaInfo:   javaInfo,
	}

	return condition, nil
}

// Check validates that a compatible Java version is installed
func (c *JavaVersionCondition) Check(ctx context.Context) error {
	if c == nil {
		return ErrJavaConditionNil
	}
	if ctx == nil {
		return ErrJavaConditionCtxNil
	}

	version, err := c.javaInfo.GetJavaVersion()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJavaNotFound, err)
	}

	if version < c.minVersion {
		return fmt.Errorf("%w: have %d, need %d", ErrJavaVersionTooOld, version, c.minVersion)
	}

	return nil
}
