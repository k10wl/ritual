package services_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"testing"
)

func TestCreateBackup_IntegrationFS(t *testing.T) {
	root := t.TempDir()

	// Seed worlds/
	worldsDir := filepath.Join(root, "worlds", "world")
	if err := os.MkdirAll(worldsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldsDir, "level.dat"), []byte("LEVEL"), 0o644); err != nil {
		t.Fatal(err)
	}

	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fsRoot.Close() })

	storage, err := adapters.NewFSRepository(fsRoot)
	if err != nil {
		t.Fatal(err)
	}

	manifest := &domain.Manifest{ManifestVersion: "v2", RitualVersion: "2.0.0"}

	ctx := context.Background()
	if err := services.CreateBackup(ctx, storage, "worlds", config.BackupsDir, manifest); err != nil {
		t.Fatal(err)
	}

	// Walk backups/ to verify snapshot exists with expected files
	backupsRoot := filepath.Join(root, config.BackupsDir)
	entries, err := os.ReadDir(backupsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup dir, got %d", len(entries))
	}

	tsDir := filepath.Join(backupsRoot, entries[0].Name())

	// Verify worlds/world/level.dat exists in backup
	data, err := os.ReadFile(filepath.Join(tsDir, "worlds", "world", "level.dat"))
	if err != nil {
		t.Fatalf("level.dat not copied: %v", err)
	}
	if string(data) != "LEVEL" {
		t.Errorf("level.dat content mismatch: %s", data)
	}

	// Verify manifest.json exists and round-trips
	manifestData, err := os.ReadFile(filepath.Join(tsDir, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	var decoded domain.Manifest
	if err := json.Unmarshal(manifestData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ManifestVersion != "v2" {
		t.Errorf("manifest decoded: %+v", decoded)
	}
}
