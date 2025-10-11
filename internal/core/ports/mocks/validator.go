package mocks

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockValidatorService is a mock implementation of ValidatorService for testing
type MockValidatorService struct {
	CheckInstanceFunc func(local *domain.Manifest, remote *domain.Manifest) error
	CheckWorldFunc    func(local *domain.Manifest, remote *domain.Manifest) error
	CheckLockFunc     func(local *domain.Manifest, remote *domain.Manifest) error
}

// NewMockValidatorService creates a new mock Validator service
func NewMockValidatorService() ports.ValidatorService {
	return &MockValidatorService{}
}

// CheckInstance validates manifest structure and content
func (m *MockValidatorService) CheckInstance(local *domain.Manifest, remote *domain.Manifest) error {
	if m.CheckInstanceFunc != nil {
		return m.CheckInstanceFunc(local, remote)
	}
	return nil
}

// CheckWorld validates world data integrity
func (m *MockValidatorService) CheckWorld(local *domain.Manifest, remote *domain.Manifest) error {
	if m.CheckWorldFunc != nil {
		return m.CheckWorldFunc(local, remote)
	}
	return nil
}

// CheckLock validates lock mechanism compliance
func (m *MockValidatorService) CheckLock(local *domain.Manifest, remote *domain.Manifest) error {
	if m.CheckLockFunc != nil {
		return m.CheckLockFunc(local, remote)
	}
	return nil
}
