package services

import (
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openRoot opens a testing root and schedules cleanup.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })
	return root
}

func TestRunMigrations(t *testing.T) {
	tests := []struct {
		name            string
		manifestVersion string
		wantRun         []string
	}{
		{"nil manifest runs all", "", []string{"2.0.0"}},
		{"old version runs pending", "1.0.0", []string{"2.0.0"}},
		{"current version skips all", "2.0.0", nil},
		{"future version skips all", "3.0.0", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran []string
			testMigrations := []Migration{
				{Version: "2.0.0", Run: func(_ *os.Root) error {
					ran = append(ran, "2.0.0")
					return nil
				}},
			}
			var manifest *domain.Manifest
			if tt.manifestVersion != "" {
				manifest = &domain.Manifest{ManifestVersion: tt.manifestVersion}
			}
			root := openRoot(t, t.TempDir())
			err := RunMigrationsWithList(root, manifest, testMigrations)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRun, ran)
		})
	}
}

func TestRunMigrations_NilRoot_Errors(t *testing.T) {
	err := RunMigrationsWithList(nil, nil, nil)
	assert.Error(t, err)
}

func TestMigrateV2(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "instance"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instance", "server.jar"), []byte("jar"), 0o644))

	root := openRoot(t, dir)
	require.NoError(t, migrateV2(root))

	assert.NoDirExists(t, filepath.Join(dir, "instance"))
	data, err := os.ReadFile(filepath.Join(dir, config.WorldsDir, ".ritualsync"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(data))

	// Idempotent — run again, no error
	require.NoError(t, migrateV2(root))
}

func TestMigrateV2_NoInstanceDir(t *testing.T) {
	dir := t.TempDir()
	root := openRoot(t, dir)
	require.NoError(t, migrateV2(root))

	data, err := os.ReadFile(filepath.Join(dir, config.WorldsDir, ".ritualsync"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(data))
}

func TestMigrateV2_NoLegacyDir_NoOp(t *testing.T) {
	dir := t.TempDir()
	root := openRoot(t, dir)
	require.NoError(t, migrateV2(root))
}
