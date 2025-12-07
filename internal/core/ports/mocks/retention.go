package mocks

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockRetentionService is a mock implementation of RetentionService for testing
type MockRetentionService struct {
	ApplyFunc func(ctx context.Context, manifest *domain.Manifest) error
}

// Compile-time check to ensure MockRetentionService implements ports.RetentionService
var _ ports.RetentionService = (*MockRetentionService)(nil)

// NewMockRetentionService creates a new mock retention service
func NewMockRetentionService() *MockRetentionService {
	return &MockRetentionService{}
}

// Apply removes old backups exceeding the retention limit
func (m *MockRetentionService) Apply(ctx context.Context, manifest *domain.Manifest) error {
	if m.ApplyFunc != nil {
		return m.ApplyFunc(ctx, manifest)
	}
	return nil
}
