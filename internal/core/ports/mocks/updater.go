package mocks

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// MockUpdaterService is a mock implementation of UpdaterService for testing
type MockUpdaterService struct {
	RunFunc   func(ctx context.Context) error
	RunCalled bool
	RunCount  int
}

// Compile-time check to ensure MockUpdaterService implements ports.UpdaterService
var _ ports.UpdaterService = (*MockUpdaterService)(nil)

// NewMockUpdaterService creates a new mock updater service
func NewMockUpdaterService() *MockUpdaterService {
	return &MockUpdaterService{}
}

// Run executes the update process
func (m *MockUpdaterService) Run(ctx context.Context) error {
	if m == nil {
		return errors.New("mock updater service cannot be nil")
	}
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	m.RunCalled = true
	m.RunCount++

	if m.RunFunc != nil {
		return m.RunFunc(ctx)
	}
	return nil
}

// Reset clears the mock state
func (m *MockUpdaterService) Reset() {
	if m == nil {
		return
	}
	m.RunCalled = false
	m.RunCount = 0
	m.RunFunc = nil
}
