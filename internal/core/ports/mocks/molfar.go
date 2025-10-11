package mocks

import "ritual/internal/core/ports"

// MockMolfarService is a mock implementation of MolfarService for testing
type MockMolfarService struct {
	PrepareFunc func() error
	RunFunc     func() error
	ExitFunc    func() error
}

// NewMockMolfarService creates a new mock Molfar service
func NewMockMolfarService() ports.MolfarService {
	return &MockMolfarService{}
}

// Prepare initializes the environment and validates prerequisites
func (m *MockMolfarService) Prepare() error {
	if m.PrepareFunc != nil {
		return m.PrepareFunc()
	}
	return nil
}

// Run executes the main server orchestration process
func (m *MockMolfarService) Run() error {
	if m.RunFunc != nil {
		return m.RunFunc()
	}
	return nil
}

// Exit gracefully shuts down the server and cleans up resources
func (m *MockMolfarService) Exit() error {
	if m.ExitFunc != nil {
		return m.ExitFunc()
	}
	return nil
}
