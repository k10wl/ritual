package mocks

import (
	"ritual/internal/core/ports"
)

// MockBackupperService is a mock implementation of BackupperService for testing
type MockBackupperService struct {
	RunFunc func() (string, error)
}

// NewMockBackupperService creates a new mock backupper service
func NewMockBackupperService() ports.BackupperService {
	return &MockBackupperService{}
}

// Run executes the backup orchestration process
func (m *MockBackupperService) Run() (string, error) {
	if m.RunFunc != nil {
		return m.RunFunc()
	}
	return "mock-archive.zip", nil
}
