package mocks

import "ritual/internal/core/ports"

// MockServerRunner is a mock implementation of ServerRunner for testing
type MockServerRunner struct {
	RunFunc func() error
}

// Compile-time check to ensure MockServerRunner implements ports.ServerRunner
var _ ports.ServerRunner = (*MockServerRunner)(nil)

// NewMockServerRunner creates a new mock server runner
func NewMockServerRunner() ports.ServerRunner {
	return &MockServerRunner{}
}

// Run executes the server process
func (m *MockServerRunner) Run() error {
	if m.RunFunc != nil {
		return m.RunFunc()
	}
	return nil
}
