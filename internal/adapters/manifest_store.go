package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// ErrNilManifest is returned when Save is called with a nil manifest.
var ErrNilManifest = errors.New("nil manifest")

type manifestStore struct {
	storage ports.StorageRepository
}

// NewManifestStore returns a ManifestStore backed by the given storage.
// Wire twice — once per side (local + remote) — at the composition root.
func NewManifestStore(s ports.StorageRepository) ports.ManifestStore {
	return &manifestStore{storage: s}
}

// Get reads config.ManifestFilename from storage and decodes it.
// Defaults are applied during decode via Manifest.UnmarshalJSON.
func (m *manifestStore) Get(ctx context.Context) (*domain.Manifest, error) {
	data, err := m.storage.Get(ctx, config.ManifestFilename)
	if err != nil {
		return nil, fmt.Errorf("manifest get: %w", err)
	}
	var out domain.Manifest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("manifest unmarshal: %w", err)
	}
	return &out, nil
}

// Save marshals the manifest (indented, matching the legacy format) and writes it.
func (m *manifestStore) Save(ctx context.Context, man *domain.Manifest) error {
	if man == nil {
		return ErrNilManifest
	}
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest marshal: %w", err)
	}
	if err := m.storage.Put(ctx, config.ManifestFilename, data); err != nil {
		return fmt.Errorf("manifest put: %w", err)
	}
	return nil
}
