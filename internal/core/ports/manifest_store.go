package ports

import (
	"context"

	"ritual/internal/core/domain"
)

// ManifestStore persists and retrieves a single Manifest from one side
// (local filesystem or remote object storage). Two instances are wired:
// one for local, one for remote. Side is a wiring concern — method names
// do not encode it.
//
// Get returns a fully-defaulted manifest (defaults applied in UnmarshalJSON).
// Save serializes the manifest as-is; callers apply domain mutations first.
type ManifestStore interface {
	Get(ctx context.Context) (*domain.Manifest, error)
	Save(ctx context.Context, m *domain.Manifest) error
}
