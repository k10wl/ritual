package services

import (
	"fmt"
	"os"
	"path/filepath"

	"ritual/internal/config"
	"ritual/internal/core/domain"
)

// Migration represents a version-gated schema migration.
type Migration struct {
	Version string
	Run     func(rootPath string) error
}

var migrations = []Migration{
	{Version: "2.0.0", Run: migrateV2},
}

// RunMigrations executes pending migrations based on ManifestVersion.
func RunMigrations(rootPath string, manifest *domain.Manifest) error {
	return RunMigrationsWithList(rootPath, manifest, migrations)
}

// RunMigrationsWithList runs migrations from a custom list (for testing).
func RunMigrationsWithList(rootPath string, manifest *domain.Manifest, list []Migration) error {
	currentVersion := ""
	if manifest != nil {
		currentVersion = manifest.ManifestVersion
	}
	for _, m := range list {
		if currentVersion == "" || IsVersionOlder(currentVersion, m.Version) {
			if err := m.Run(rootPath); err != nil {
				return fmt.Errorf("migration to %s failed: %w", m.Version, err)
			}
		}
	}
	return nil
}

// migrateV2: delete instance/ directory, create worlds/.ritualsync with "*"
func migrateV2(rootPath string) error {
	os.RemoveAll(filepath.Join(rootPath, "instance"))

	ritualSync := filepath.Join(rootPath, config.WorldsDir, ".ritualsync")
	if _, err := os.Stat(ritualSync); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(filepath.Dir(ritualSync), 0755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(ritualSync, []byte("*\n"), 0644)
	}
	return nil
}
