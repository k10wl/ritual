package mocks

import (
	"ritual/internal/core/ports"
)

// MockBackupperService is a mock implementation of BackupperService for testing
type MockBackupperService struct {
	RunFunc func() (func() error, error)
}

// NewMockBackupperService creates a new mock backupper service
func NewMockBackupperService() ports.BackupperService {
	return &MockBackupperService{}
}

// Run executes the backup orchestration process
func (m *MockBackupperService) Run() (func() error, error) {
	if m.RunFunc != nil {
		return m.RunFunc()
	}
	return func() error { return nil }, nil
}

// Exit gracefully shuts down the backup service
func (m *MockBackupperService) Exit() error {
	return nil
}
