package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"strings"
	"testing"
)

func TestCreateBackup_CopiesAllKeys(t *testing.T) {
	storage := &mocks.MockStorageRepository{}

	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		if prefix != "worlds" {
			t.Errorf("List prefix=%s, want worlds", prefix)
		}
		return []string{
			"worlds/world/level.dat",
			"worlds/world/region/r.0.0.mca",
		}, nil
	}

	copies := map[string]string{}
	storage.CopyFunc = func(ctx context.Context, src, dst string) error {
		copies[src] = dst
		return nil
	}
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		return nil
	}

	manifest := &domain.Manifest{ManifestVersion: "v2"}
	if err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest); err != nil {
		t.Fatal(err)
	}

	if len(copies) != 2 {
		t.Fatalf("got %d copies, want 2: %v", len(copies), copies)
	}

	for src, dst := range copies {
		if !strings.HasPrefix(dst, "backups/") {
			t.Errorf("dst=%s missing backups prefix", dst)
		}
		if !strings.Contains(dst, "/"+src) {
			t.Errorf("dst=%s should contain src=%s", dst, src)
		}
	}
}

func TestCreateBackup_WritesManifestJSON(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, nil
	}
	storage.CopyFunc = func(ctx context.Context, src, dst string) error { return nil }

	var manifestKey string
	var manifestData []byte
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		manifestKey = key
		manifestData = data
		return nil
	}

	manifest := &domain.Manifest{ManifestVersion: "v2", RitualVersion: "2.0.0"}
	if err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(manifestKey, "/manifest.json") {
		t.Errorf("manifest key=%s, want suffix /manifest.json", manifestKey)
	}
	if !strings.HasPrefix(manifestKey, "backups/") {
		t.Errorf("manifest key=%s, want backups/ prefix", manifestKey)
	}

	var decoded domain.Manifest
	if err := json.Unmarshal(manifestData, &decoded); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if decoded.ManifestVersion != "v2" || decoded.RitualVersion != "2.0.0" {
		t.Errorf("manifest round-trip mismatch: %+v", decoded)
	}

	if !strings.Contains(string(manifestData), "\n") {
		t.Error("manifest should be pretty-printed (MarshalIndent)")
	}
}

func TestCreateBackup_ListError_Propagates(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	want := errors.New("list boom")
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, want
	}

	manifest := &domain.Manifest{}
	err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest)
	if !errors.Is(err, want) {
		t.Errorf("got %v, want wrapping %v", err, want)
	}
}

func TestCreateBackup_PathsUseForwardSlashes(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"worlds/world/level.dat"}, nil
	}
	var copyDst string
	storage.CopyFunc = func(ctx context.Context, src, dst string) error {
		copyDst = dst
		return nil
	}
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error { return nil }

	manifest := &domain.Manifest{}
	if err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(copyDst, `\`) {
		t.Errorf("dst=%s contains backslash — storage keys must use /", copyDst)
	}
}
