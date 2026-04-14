package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestRetention_IntegrationFS_MixedFormats(t *testing.T) {
	root := t.TempDir()
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0755); err != nil {
		t.Fatal(err)
	}

	// v2 directory backups (timestamp-named)
	for _, ts := range []string{"20260414160000", "20260413160000", "20260412160000"} {
		d := filepath.Join(backups, ts, "worlds")
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "level.dat"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// v1 tar backups
	if err := os.WriteFile(filepath.Join(backups, "20260411160000.tar"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, "20260410160000.tar"), []byte("older"), 0644); err != nil {
		t.Fatal(err)
	}

	// Unknown file (should be deleted by sacred-dir rule)
	if err := os.WriteFile(filepath.Join(backups, "garbage.txt"), []byte("noise"), 0644); err != nil {
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
	parse := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)

	r, err := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", parse)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	mustNotExist(t, filepath.Join(backups, "garbage.txt"))
	mustNotExist(t, filepath.Join(backups, "20260411160000.tar"))
	mustNotExist(t, filepath.Join(backups, "20260410160000.tar"))

	mustExist(t, filepath.Join(backups, "20260414160000"))
	mustExist(t, filepath.Join(backups, "20260413160000"))
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}
func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected %s to be deleted", path)
	}
}
