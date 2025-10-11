package mocks

import (
	"ritual/internal/core/domain"

	"github.com/stretchr/testify/mock"
)

// MockServerRunner is a mock implementation of ServerRunner interface
type MockServerRunner struct {
	mock.Mock
}

// NewMockServerRunner creates a new MockServerRunner instance
func NewMockServerRunner() *MockServerRunner {
	return &MockServerRunner{}
}

// Run mocks the Run method
func (m *MockServerRunner) Run(server *domain.Server) error {
	args := m.Called(server)
	return args.Error(0)
}
