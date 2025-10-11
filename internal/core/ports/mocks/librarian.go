package mocks

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockLibrarianService is a mock implementation of LibrarianService for testing
type MockLibrarianService struct {
	GetLocalManifestFunc   func(ctx context.Context) (*domain.Manifest, error)
	GetRemoteManifestFunc  func(ctx context.Context) (*domain.Manifest, error)
	SaveLocalManifestFunc  func(ctx context.Context, manifest *domain.Manifest) error
	SaveRemoteManifestFunc func(ctx context.Context, manifest *domain.Manifest) error
}

// NewMockLibrarianService creates a new mock Librarian service
func NewMockLibrarianService() ports.LibrarianService {
	return &MockLibrarianService{}
}

// GetLocalManifest retrieves the local manifest
func (m *MockLibrarianService) GetLocalManifest(ctx context.Context) (*domain.Manifest, error) {
	if m.GetLocalManifestFunc != nil {
		return m.GetLocalManifestFunc(ctx)
	}
	return nil, nil
}

// GetRemoteManifest retrieves the remote manifest
func (m *MockLibrarianService) GetRemoteManifest(ctx context.Context) (*domain.Manifest, error) {
	if m.GetRemoteManifestFunc != nil {
		return m.GetRemoteManifestFunc(ctx)
	}
	return nil, nil
}

// SaveLocalManifest stores the manifest locally
func (m *MockLibrarianService) SaveLocalManifest(ctx context.Context, manifest *domain.Manifest) error {
	if m.SaveLocalManifestFunc != nil {
		return m.SaveLocalManifestFunc(ctx, manifest)
	}
	return nil
}

// SaveRemoteManifest stores the manifest remotely
func (m *MockLibrarianService) SaveRemoteManifest(ctx context.Context, manifest *domain.Manifest) error {
	if m.SaveRemoteManifestFunc != nil {
		return m.SaveRemoteManifestFunc(ctx, manifest)
	}
	return nil
}
