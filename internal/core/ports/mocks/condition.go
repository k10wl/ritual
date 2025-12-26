package mocks

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// MockConditionService is a mock implementation of ConditionService for testing
type MockConditionService struct {
	CheckFunc   func(ctx context.Context) error
	CheckCalled bool
	CheckCount  int
}

// Compile-time check to ensure MockConditionService implements ports.ConditionService
var _ ports.ConditionService = (*MockConditionService)(nil)

// NewMockConditionService creates a new mock condition service
func NewMockConditionService() *MockConditionService {
	return &MockConditionService{}
}

// Check validates the condition
func (m *MockConditionService) Check(ctx context.Context) error {
	if m == nil {
		return errors.New("mock condition service cannot be nil")
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	m.CheckCalled = true
	m.CheckCount++

	if m.CheckFunc != nil {
		return m.CheckFunc(ctx)
	}
	return nil
}

// Reset clears the mock state
func (m *MockConditionService) Reset() {
	if m == nil {
		return
	}
	m.CheckCalled = false
	m.CheckCount = 0
	m.CheckFunc = nil
}
