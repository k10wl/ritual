package mocks

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockManifestStore is a handwritten mock for ports.ManifestStore.
// Funcs default to no-op returning (nil, nil) / nil so tests configure
// only the behavior they care about.
type MockManifestStore struct {
	GetFunc  func(ctx context.Context) (*domain.Manifest, error)
	SaveFunc func(ctx context.Context, m *domain.Manifest) error

	GetCalls  int
	SaveCalls int
}

var _ ports.ManifestStore = (*MockManifestStore)(nil)

// Get calls GetFunc when set, otherwise returns (nil, nil).
func (m *MockManifestStore) Get(ctx context.Context) (*domain.Manifest, error) {
	m.GetCalls++
	if m.GetFunc != nil {
		return m.GetFunc(ctx)
	}
	return nil, nil //nolint:nilnil // mock default: no manifest, no error
}

// Save calls SaveFunc when set.
func (m *MockManifestStore) Save(ctx context.Context, man *domain.Manifest) error {
	m.SaveCalls++
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, man)
	}
	return nil
}
