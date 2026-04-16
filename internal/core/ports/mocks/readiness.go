package mocks

import (
	"context"

	"ritual/internal/core/ports"
)

type MockReadinessCheck struct {
	WaitFunc func(ctx context.Context) error
}

var _ ports.ReadinessCheck = (*MockReadinessCheck)(nil)

func (m *MockReadinessCheck) Wait(ctx context.Context) error {
	if m.WaitFunc != nil {
		return m.WaitFunc(ctx)
	}
	return nil
}
