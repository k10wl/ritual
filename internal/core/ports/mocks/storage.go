package mocks

import (
	"context"
	"ritual/internal/core/ports"
)

// MockStorageRepository is a mock implementation of StorageRepository for testing
type MockStorageRepository struct {
	Label           string
	GetFunc         func(ctx context.Context, key string) ([]byte, error)
	PutFunc         func(ctx context.Context, key string, data []byte) error
	DeleteFunc      func(ctx context.Context, key string) error
	DeleteBatchFunc func(ctx context.Context, keys []string) error
	ListFunc        func(ctx context.Context, prefix string) ([]string, error)
	CopyFunc        func(ctx context.Context, sourceKey string, destKey string) error
	RenameFunc      func(ctx context.Context, sourceKey string, destKey string) error
}

// String returns adapter label, defaulting to "mock::storage" when unset.
func (m *MockStorageRepository) String() string {
	if m.Label != "" {
		return m.Label
	}
	return "mock::storage"
}

// Rename moves data from sourceKey to destKey.
func (m *MockStorageRepository) Rename(ctx context.Context, sourceKey string, destKey string) error {
	if m.RenameFunc != nil {
		return m.RenameFunc(ctx, sourceKey, destKey)
	}
	if err := m.Copy(ctx, sourceKey, destKey); err != nil {
		return err
	}
	return m.Delete(ctx, sourceKey)
}

// NewMockStorageRepository creates a new mock storage repository
func NewMockStorageRepository() ports.StorageRepository {
	return &MockStorageRepository{}
}

// Get retrieves data by key
func (m *MockStorageRepository) Get(ctx context.Context, key string) ([]byte, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key)
	}
	return nil, nil
}

// Put stores data with the given key
func (m *MockStorageRepository) Put(ctx context.Context, key string, data []byte) error {
	if m.PutFunc != nil {
		return m.PutFunc(ctx, key, data)
	}
	return nil
}

// Delete removes data by key
func (m *MockStorageRepository) Delete(ctx context.Context, key string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, key)
	}
	return nil
}

// DeleteBatch removes multiple keys in a single operation
func (m *MockStorageRepository) DeleteBatch(ctx context.Context, keys []string) error {
	if m.DeleteBatchFunc != nil {
		return m.DeleteBatchFunc(ctx, keys)
	}
	for _, key := range keys {
		if err := m.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// List returns all keys with the given prefix
func (m *MockStorageRepository) List(ctx context.Context, prefix string) ([]string, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, prefix)
	}
	return []string{}, nil
}

// Copy copies data from source key to destination key
func (m *MockStorageRepository) Copy(ctx context.Context, sourceKey string, destKey string) error {
	if m.CopyFunc != nil {
		return m.CopyFunc(ctx, sourceKey, destKey)
	}
	return nil
}
