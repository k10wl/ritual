package mocks

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockLibrarianService is a mock implementation of LibrarianService for testing
type MockLibrarianService struct {
	GetLocalManifestFunc   func() (*domain.Manifest, error)
	GetRemoteManifestFunc  func() (*domain.Manifest, error)
	SaveLocalManifestFunc  func(manifest *domain.Manifest) error
	SaveRemoteManifestFunc func(manifest *domain.Manifest) error
}

// NewMockLibrarianService creates a new mock Librarian service
func NewMockLibrarianService() ports.LibrarianService {
	return &MockLibrarianService{}
}

// GetLocalManifest retrieves the local manifest
func (m *MockLibrarianService) GetLocalManifest() (*domain.Manifest, error) {
	if m.GetLocalManifestFunc != nil {
		return m.GetLocalManifestFunc()
	}
	return nil, nil
}

// GetRemoteManifest retrieves the remote manifest
func (m *MockLibrarianService) GetRemoteManifest() (*domain.Manifest, error) {
	if m.GetRemoteManifestFunc != nil {
		return m.GetRemoteManifestFunc()
	}
	return nil, nil
}

// SaveLocalManifest stores the manifest locally
func (m *MockLibrarianService) SaveLocalManifest(manifest *domain.Manifest) error {
	if m.SaveLocalManifestFunc != nil {
		return m.SaveLocalManifestFunc(manifest)
	}
	return nil
}

// SaveRemoteManifest stores the manifest remotely
func (m *MockLibrarianService) SaveRemoteManifest(manifest *domain.Manifest) error {
	if m.SaveRemoteManifestFunc != nil {
		return m.SaveRemoteManifestFunc(manifest)
	}
	return nil
}
