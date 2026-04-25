// Package checks defines pre-flight gate functions used by the Checking
// stage. A Check is a context-bound predicate: it probes a host capability
// and returns nil when the threshold is met, or a sentinel-wrapped error
// when it is not.
//
// The package intentionally avoids interfaces and structs — closures over
// provider deps are enough. Observability (per-check lifecycle events) lives
// in the Observed decorator, leaving pure check factories bus-free and
// trivially unit-testable.
package checks

import (
	"context"
	"errors"
	"fmt"

)

// Sentinel errors returned (wrapped with details) by the built-in checks.
var (
	ErrInsufficientRAM   = errors.New("insufficient RAM")
	ErrInsufficientDisk  = errors.New("insufficient disk space")
	ErrJavaNotFound      = errors.New("java not found")
	ErrJavaVersionTooOld = errors.New("java version too old")
)

// Check is the unit of pre-flight work the Checking stage iterates over.
// It returns nil on pass; non-nil on fail aborts the stage.
type Check func(ctx context.Context) error

// RAM returns a Check that fails when the host has less free RAM than min
// megabytes.
func RAM(min int, hw SystemInfoProvider) Check {
	return func(_ context.Context) error {
		free, err := hw.GetFreeRAMMB()
		if err != nil {
			return fmt.Errorf("read free RAM: %w", err)
		}
		if free < min {
			return fmt.Errorf("%w: have %d MB, need %d MB", ErrInsufficientRAM, free, min)
		}
		return nil
	}
}

// Disk returns a Check that fails when path's volume has less free space
// than min megabytes.
func Disk(min int, path string, hw DiskInfoProvider) Check {
	return func(_ context.Context) error {
		free, err := hw.GetFreeDiskMB(path)
		if err != nil {
			return fmt.Errorf("read free disk %q: %w", path, err)
		}
		if free < min {
			return fmt.Errorf("%w on %s: have %d MB, need %d MB", ErrInsufficientDisk, path, free, min)
		}
		return nil
	}
}

// Java returns a Check that fails when the installed Java major version is
// below min.
func Java(min int, p JavaVersionProvider) Check {
	return func(_ context.Context) error {
		version, err := p.GetJavaVersion()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrJavaNotFound, err)
		}
		if version < min {
			return fmt.Errorf("%w: have %d, need %d", ErrJavaVersionTooOld, version, min)
		}
		return nil
	}
}
