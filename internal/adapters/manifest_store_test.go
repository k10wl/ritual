package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"strings"
	"testing"
)

// validManifestJSON returns marshaled bytes of a valid, non-empty manifest.
// Deliberately omits default fields so Get can prove defaults-on-decode.
func validManifestJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(&domain.Manifest{
		ManifestVersion: "1.0",
		RitualVersion:   "9.9.9",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestManifestStore_Get_Happy(t *testing.T) {
	storage := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) {
			if key != config.ManifestFilename {
				t.Errorf("key = %q, want %q", key, config.ManifestFilename)
			}
			return validManifestJSON(t), nil
		},
	}
	store := NewManifestStore(storage)
	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil manifest")
	}
	if got.MinRAMMB != config.DefaultMinRAMMB {
		t.Errorf("MinRAMMB = %d, want default %d (decode-time defaults broken)", got.MinRAMMB, config.DefaultMinRAMMB)
	}
}

func TestManifestStore_Get_StorageErr(t *testing.T) {
	boom := errors.New("boom")
	storage := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) { return nil, boom },
	}
	store := NewManifestStore(storage)
	_, err := store.Get(context.Background())
	if err == nil {
		t.Fatal("Get returned nil err")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err does not wrap boom: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest get") {
		t.Errorf("err message does not mention manifest get: %v", err)
	}
}

func TestManifestStore_Get_BadJSON(t *testing.T) {
	storage := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) {
			return []byte("not json {"), nil
		},
	}
	store := NewManifestStore(storage)
	_, err := store.Get(context.Background())
	if err == nil {
		t.Fatal("Get returned nil err on bad JSON")
	}
	if !strings.Contains(err.Error(), "manifest unmarshal") {
		t.Errorf("err does not mention unmarshal: %v", err)
	}
}

// TestManifestStore_Get_EmptyBytes documents behavior on (nil, nil) from storage.
// Decision: surface unmarshal error. json.Unmarshal(nil, ...) returns a typed
// error; we wrap it as "manifest unmarshal". Callers who care about "missing"
// should differentiate via errors.Is on the underlying storage error.
func TestManifestStore_Get_EmptyBytes(t *testing.T) {
	storage := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) { return nil, nil },
	}
	store := NewManifestStore(storage)
	_, err := store.Get(context.Background())
	if err == nil {
		t.Fatal("Get returned nil err for empty bytes")
	}
	if !strings.Contains(err.Error(), "manifest unmarshal") {
		t.Errorf("err classification drifted: %v", err)
	}
}

func TestManifestStore_Save_Happy(t *testing.T) {
	var capturedKey string
	var capturedData []byte
	storage := &mocks.MockStorageRepository{
		PutFunc: func(ctx context.Context, key string, data []byte) error {
			capturedKey = key
			capturedData = data
			return nil
		},
	}
	store := NewManifestStore(storage)
	m := &domain.Manifest{ManifestVersion: "1.0", MinRAMMB: 4096}
	if err := store.Save(context.Background(), m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if capturedKey != config.ManifestFilename {
		t.Errorf("key = %q, want %q", capturedKey, config.ManifestFilename)
	}
	// Round-trip: decode saved bytes and check fields match.
	var got domain.Manifest
	if err := json.Unmarshal(capturedData, &got); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if got.ManifestVersion != "1.0" {
		t.Errorf("ManifestVersion = %q, want %q", got.ManifestVersion, "1.0")
	}
	if got.MinRAMMB != 4096 {
		t.Errorf("MinRAMMB = %d, want 4096", got.MinRAMMB)
	}
}

func TestManifestStore_Save_StorageErr(t *testing.T) {
	boom := errors.New("put failed")
	storage := &mocks.MockStorageRepository{
		PutFunc: func(ctx context.Context, key string, data []byte) error { return boom },
	}
	store := NewManifestStore(storage)
	err := store.Save(context.Background(), &domain.Manifest{ManifestVersion: "1.0"})
	if err == nil {
		t.Fatal("Save returned nil err")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err does not wrap boom: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest put") {
		t.Errorf("err message does not mention manifest put: %v", err)
	}
}

func TestManifestStore_Save_NilManifest(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	store := NewManifestStore(storage)
	err := store.Save(context.Background(), nil)
	if !errors.Is(err, ErrNilManifest) {
		t.Fatalf("err = %v, want ErrNilManifest", err)
	}
}

func TestManifestStore_Save_JSONIndent(t *testing.T) {
	var captured []byte
	storage := &mocks.MockStorageRepository{
		PutFunc: func(ctx context.Context, key string, data []byte) error {
			captured = data
			return nil
		},
	}
	store := NewManifestStore(storage)
	m := &domain.Manifest{ManifestVersion: "1.0"}
	if err := store.Save(context.Background(), m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.Contains(string(captured), "\n  ") {
		t.Errorf("output not indented with two spaces:\n%s", captured)
	}
}
