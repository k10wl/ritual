package mocks

import (
	"github.com/stretchr/testify/mock"
)

// MockCommandExecutor is a mock implementation of CommandExecutor interface
type MockCommandExecutor struct {
	mock.Mock
}

// NewMockCommandExecutor creates a new MockCommandExecutor instance
func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{}
}

// Execute mocks the Execute method
func (m *MockCommandExecutor) Execute(command string, args []string, workingDir string) error {
	argsMock := m.Called(command, args, workingDir)
	return argsMock.Error(0)
}
