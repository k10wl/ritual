package mocks

import (
	"context"
	"ritual/internal/core/ports"
)

// MockReadinessCheck is a test double for ports.ReadinessCheck.
type MockReadinessCheck struct {
	WaitFunc func(ctx context.Context) error
}

var _ ports.ReadinessCheck = (*MockReadinessCheck)(nil)

// Wait calls WaitFunc when set, otherwise returns nil immediately.
func (m *MockReadinessCheck) Wait(ctx context.Context) error {
	if m.WaitFunc != nil {
		return m.WaitFunc(ctx)
	}
	return nil
}
