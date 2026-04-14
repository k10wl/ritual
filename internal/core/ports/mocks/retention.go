package mocks

import (
	"context"

	"ritual/internal/core/ports"
)

// MockRetentionService is a mock implementation of RetentionService for testing
type MockRetentionService struct {
	ApplyFunc  func(ctx context.Context) error
	ApplyCalls int
}

var _ ports.RetentionService = (*MockRetentionService)(nil)

func NewMockRetentionService() *MockRetentionService {
	return &MockRetentionService{}
}

func (m *MockRetentionService) Apply(ctx context.Context) error {
	m.ApplyCalls++
	if m.ApplyFunc != nil {
		return m.ApplyFunc(ctx)
	}
	return nil
}
