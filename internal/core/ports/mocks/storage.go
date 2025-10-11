package mocks

import "ritual/internal/core/ports"

// MockStorageRepository is a mock implementation of StorageRepository for testing
type MockStorageRepository struct {
	GetFunc    func(key string) ([]byte, error)
	PutFunc    func(key string, data []byte) error
	DeleteFunc func(key string) error
	ListFunc   func(prefix string) ([]string, error)
}

// NewMockStorageRepository creates a new mock storage repository
func NewMockStorageRepository() ports.StorageRepository {
	return &MockStorageRepository{}
}

// Get retrieves data by key
func (m *MockStorageRepository) Get(key string) ([]byte, error) {
	if m.GetFunc != nil {
		return m.GetFunc(key)
	}
	return nil, nil
}

// Put stores data with the given key
func (m *MockStorageRepository) Put(key string, data []byte) error {
	if m.PutFunc != nil {
		return m.PutFunc(key, data)
	}
	return nil
}

// Delete removes data by key
func (m *MockStorageRepository) Delete(key string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(key)
	}
	return nil
}

// List returns all keys with the given prefix
func (m *MockStorageRepository) List(prefix string) ([]string, error) {
	if m.ListFunc != nil {
		return m.ListFunc(prefix)
	}
	return []string{}, nil
}
