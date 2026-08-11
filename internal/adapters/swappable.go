package adapters

import (
	"context"
	"io"
	"ritual/internal/core/ports"
	"sync/atomic"
)

// SwappableStorage implements ports.StorageRepository by forwarding every
// call to whatever repository was last Store()'d, via an atomic.Pointer.
// Lets downstream consumers (puller, applier, committer, pusher,
// dirtyProber, versionLister, localCollector/Deleter, locker) be built once
// at boot and keep working unmodified across a workroot relocate
// (design-log/055 Q4) — they hold this facade as an interface value, never
// the concrete FSRepository.
type SwappableStorage struct {
	p atomic.Pointer[ports.StorageRepository]
}

// NewSwappableStorage returns an unset facade — Store must be called before
// any forwarding method is used.
func NewSwappableStorage() *SwappableStorage {
	return &SwappableStorage{}
}

// Store swaps the backing repository. inner must not be nil.
func (s *SwappableStorage) Store(inner ports.StorageRepository) {
	s.p.Store(&inner)
}

// Current returns the currently active backing repository — used by
// relocating for snapshot/rollback bookkeeping, never by pipeline stages.
func (s *SwappableStorage) Current() ports.StorageRepository {
	v := s.p.Load()
	if v == nil {
		return nil
	}
	return *v
}

func (s *SwappableStorage) String() string {
	if v := s.Current(); v != nil {
		return v.String()
	}
	return "swappable::unset"
}

func (s *SwappableStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.Current().GetStream(ctx, key)
}

func (s *SwappableStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	return s.Current().PutStream(ctx, key, body)
}

func (s *SwappableStorage) Exists(ctx context.Context, key string) (bool, error) {
	return s.Current().Exists(ctx, key)
}

func (s *SwappableStorage) Delete(ctx context.Context, key string) error {
	return s.Current().Delete(ctx, key)
}

func (s *SwappableStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return s.Current().DeleteBatch(ctx, keys)
}

func (s *SwappableStorage) List(ctx context.Context, prefix string) ([]string, error) {
	return s.Current().List(ctx, prefix)
}

func (s *SwappableStorage) Copy(ctx context.Context, sourceKey string, destKey string) error {
	return s.Current().Copy(ctx, sourceKey, destKey)
}

var _ ports.StorageRepository = (*SwappableStorage)(nil)
