package mocks

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestMockStorageRepository_GetStream_Default(t *testing.T) {
	mock := NewMockStorageRepository()

	rc, err := mock.GetStream(context.Background(), "k")
	if err != nil {
		t.Fatalf("default GetStream should not error, got %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("default GetStream body read failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("default GetStream body expected empty, got %d bytes", len(data))
	}
}

func TestMockStorageRepository_GetStream_Delegates(t *testing.T) {
	mock := NewMockStorageRepository().(*MockStorageRepository)

	want := []byte("streamed")
	mock.GetStreamFunc = func(ctx context.Context, key string) (io.ReadCloser, error) {
		if key != "expected-key" {
			t.Errorf("GetStreamFunc got key %q, want expected-key", key)
		}
		return io.NopCloser(bytes.NewReader(want)), nil
	}

	rc, err := mock.GetStream(context.Background(), "expected-key")
	if err != nil {
		t.Fatalf("delegate returned err: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("body read failed: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMockStorageRepository_PutStream_DefaultIsNoop(t *testing.T) {
	mock := NewMockStorageRepository()

	if err := mock.PutStream(context.Background(), "k", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("default PutStream should be no-op, got %v", err)
	}
}

func TestMockStorageRepository_PutStream_Delegates(t *testing.T) {
	mock := NewMockStorageRepository().(*MockStorageRepository)

	var seenKey string
	var seenBytes []byte
	mock.PutStreamFunc = func(ctx context.Context, key string, body io.ReadSeeker) error {
		seenKey = key
		data, _ := io.ReadAll(body)
		seenBytes = data
		return nil
	}

	payload := []byte("body")
	if err := mock.PutStream(context.Background(), "k", bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutStream returned err: %v", err)
	}
	if seenKey != "k" {
		t.Errorf("delegate got key %q, want k", seenKey)
	}
	if string(seenBytes) != string(payload) {
		t.Errorf("delegate got body %q, want %q", seenBytes, payload)
	}
}

func TestMockStorageRepository_Exists_Default(t *testing.T) {
	mock := NewMockStorageRepository()

	ok, err := mock.Exists(context.Background(), "k")
	if err != nil {
		t.Fatalf("default Exists should not error, got %v", err)
	}
	if ok {
		t.Error("default Exists should return false")
	}
}

func TestMockStorageRepository_Exists_Delegates(t *testing.T) {
	mock := NewMockStorageRepository().(*MockStorageRepository)

	sentinel := errors.New("exists failed")
	mock.ExistsFunc = func(ctx context.Context, key string) (bool, error) {
		if key == "hit" {
			return true, nil
		}
		if key == "err" {
			return false, sentinel
		}
		return false, nil
	}

	ok, err := mock.Exists(context.Background(), "hit")
	if err != nil || !ok {
		t.Errorf("hit: got ok=%v err=%v, want true nil", ok, err)
	}
	if _, err := mock.Exists(context.Background(), "err"); !errors.Is(err, sentinel) {
		t.Errorf("err: got %v, want %v", err, sentinel)
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
