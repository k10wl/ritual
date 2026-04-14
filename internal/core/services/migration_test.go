package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/config"
	"ritual/internal/core/domain"
)

func TestRunMigrations(t *testing.T) {
	tests := []struct {
		name            string
		manifestVersion string
		wantRun         []string
	}{
		{"nil manifest runs all", "", []string{"2.0.0"}},
		{"old version runs pending", "1.0.0", []string{"2.0.0"}},
		{"current version skips all", "2.0.0", []string{}},
		{"future version skips all", "3.0.0", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran []string
			testMigrations := []Migration{
				{Version: "2.0.0", Run: func(rootPath string) error {
					ran = append(ran, "2.0.0")
					return nil
				}},
			}
			var manifest *domain.Manifest
			if tt.manifestVersion != "" {
				manifest = &domain.Manifest{ManifestVersion: tt.manifestVersion}
			}
			err := RunMigrationsWithList(t.TempDir(), manifest, testMigrations)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRun, ran)
		})
	}
}

func TestMigrateV2(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "instance"), 0755))
	// Also create a file inside instance/ to verify full removal
	require.NoError(t, os.WriteFile(filepath.Join(root, "instance", "server.jar"), []byte("jar"), 0644))

	require.NoError(t, migrateV2(root))

	assert.NoDirExists(t, filepath.Join(root, "instance"))
	data, err := os.ReadFile(filepath.Join(root, config.WorldsDir, ".ritualsync"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(data))

	// Idempotent — run again, no error
	require.NoError(t, migrateV2(root))
}

func TestMigrateV2_NoInstanceDir(t *testing.T) {
	root := t.TempDir()
	// No instance/ dir exists — should not error
	require.NoError(t, migrateV2(root))
	// .ritualsync still created
	data, err := os.ReadFile(filepath.Join(root, config.WorldsDir, ".ritualsync"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(data))
}
