package mocks

import (
	"context"
	"testing"
)

func TestMockStorageRepository(t *testing.T) {
	mock := NewMockStorageRepository()

	storage := mock
	if storage == nil {
		t.Error("MockStorageRepository does not implement StorageRepository interface")
	}

	testKey := "test-key"
	testData := []byte("test-data")

	mockStorage := mock.(*MockStorageRepository)
	mockStorage.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		if key != testKey {
			t.Errorf("Expected key %s, got %s", testKey, key)
		}
		return testData, nil
	}

	result, err := storage.Get(context.Background(), testKey)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if string(result) != string(testData) {
		t.Errorf("Expected %s, got %s", string(testData), string(result))
	}

	mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		if key != testKey {
			t.Errorf("Expected key %s, got %s", testKey, key)
		}
		if string(data) != string(testData) {
			t.Errorf("Expected data %s, got %s", string(testData), string(data))
		}
		return nil
	}

	err = storage.Put(context.Background(), testKey, testData)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
		if key != testKey {
			t.Errorf("Expected key %s, got %s", testKey, key)
		}
		return nil
	}

	err = storage.Delete(context.Background(), testKey)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockStorage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"key1", "key2"}, nil
	}

	keys, err := storage.List(context.Background(), "prefix")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
}

func TestMockStorageRepository_DeleteBatch_WithFunc(t *testing.T) {
	mock := NewMockStorageRepository()
	mockStorage := mock.(*MockStorageRepository)

	calledWith := []string{}
	mockStorage.DeleteBatchFunc = func(ctx context.Context, keys []string) error {
		calledWith = keys
		return nil
	}

	err := mockStorage.DeleteBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(calledWith) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(calledWith))
	}
}

func TestMockStorageRepository_DeleteBatch_FallbackToDelete(t *testing.T) {
	mock := NewMockStorageRepository()
	mockStorage := mock.(*MockStorageRepository)

	deleted := []string{}
	mockStorage.DeleteFunc = func(ctx context.Context, key string) error {
		deleted = append(deleted, key)
		return nil
	}

	err := mockStorage.DeleteBatch(context.Background(), []string{"x", "y"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("Expected 2 deletes, got %d", len(deleted))
	}
}
