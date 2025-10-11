package mocks

import (
	"context"
	"ritual/internal/core/ports"
)

// MockStorageRepository is a mock implementation of StorageRepository for testing
type MockStorageRepository struct {
	GetFunc    func(ctx context.Context, key string) ([]byte, error)
	PutFunc    func(ctx context.Context, key string, data []byte) error
	DeleteFunc func(ctx context.Context, key string) error
	ListFunc   func(ctx context.Context, prefix string) ([]string, error)
	CopyFunc   func(ctx context.Context, sourceKey string, destKey string) error
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
