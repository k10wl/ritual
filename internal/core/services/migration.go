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

// migrateV2: delete instance/, create worlds/.ritualsync, move legacy world_backups/ → backups/.
func migrateV2(rootPath string) error {
	os.RemoveAll(filepath.Join(rootPath, "instance"))

	ritualSync := filepath.Join(rootPath, config.WorldsDir, ".ritualsync")
	if _, err := os.Stat(ritualSync); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(filepath.Dir(ritualSync), 0755); mkErr != nil {
			return mkErr
		}
		if wErr := os.WriteFile(ritualSync, []byte("*\n"), 0644); wErr != nil {
			return wErr
		}
	}

	return migrateLegacyBackups(rootPath)
}

// migrateLegacyBackups moves any files from world_backups/ into backups/ so
// unified retention handles them. Idempotent: no-op if world_backups/ is gone.
func migrateLegacyBackups(rootPath string) error {
	legacy := filepath.Join(rootPath, "world_backups")
	target := filepath.Join(rootPath, config.BackupsDir)

	entries, err := os.ReadDir(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy backups: %w", err)
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("mkdir backups: %w", err)
	}

	for _, e := range entries {
		src := filepath.Join(legacy, e.Name())
		dst := filepath.Join(target, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s: %w", e.Name(), err)
		}
	}

	return os.Remove(legacy)
}
