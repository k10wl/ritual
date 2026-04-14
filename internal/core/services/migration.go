package services

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"ritual/internal/config"
	"ritual/internal/core/domain"
)

// Migration represents a version-gated schema migration.
// Run receives the project work root (already confined via *os.Root).
type Migration struct {
	Version string
	Run     func(root *os.Root) error
}

var migrations = []Migration{
	{Version: "2.0.0", Run: migrateV2},
}

// RunMigrations executes pending migrations based on ManifestVersion.
// root is the project work root — all migration IO must go through it.
func RunMigrations(root *os.Root, manifest *domain.Manifest) error {
	return RunMigrationsWithList(root, manifest, migrations)
}

// RunMigrationsWithList runs migrations from a custom list (for testing).
func RunMigrationsWithList(root *os.Root, manifest *domain.Manifest, list []Migration) error {
	if root == nil {
		return errors.New("migration root cannot be nil")
	}
	currentVersion := ""
	if manifest != nil {
		currentVersion = manifest.ManifestVersion
	}
	for _, m := range list {
		if currentVersion == "" || IsVersionOlder(currentVersion, m.Version) {
			if err := m.Run(root); err != nil {
				return fmt.Errorf("migration to %s failed: %w", m.Version, err)
			}
		}
	}
	return nil
}

// migrateV2: delete instance/, create worlds/.ritualsync, move legacy world_backups/ → backups/.
func migrateV2(root *os.Root) error {
	if err := root.RemoveAll("instance"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove instance: %w", err)
	}

	ritualSync := filepath.Join(config.WorldsDir, ".ritualsync")
	if _, err := root.Stat(ritualSync); errors.Is(err, fs.ErrNotExist) {
		if mkErr := root.MkdirAll(config.WorldsDir, 0755); mkErr != nil {
			return fmt.Errorf("mkdir worlds: %w", mkErr)
		}
		if wErr := root.WriteFile(ritualSync, []byte("*\n"), 0644); wErr != nil {
			return fmt.Errorf("write ritualsync: %w", wErr)
		}
	}

	return migrateLegacyBackups(root)
}

// migrateLegacyBackups moves entries from world_backups/ into backups/ so
// unified retention handles them. Idempotent: no-op if world_backups/ is absent.
func migrateLegacyBackups(root *os.Root) error {
	const legacy = "world_backups"

	entries, err := fs.ReadDir(root.FS(), legacy)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read legacy backups: %w", err)
	}

	if err := root.MkdirAll(config.BackupsDir, 0755); err != nil {
		return fmt.Errorf("mkdir backups: %w", err)
	}

	for _, e := range entries {
		src := filepath.Join(legacy, e.Name())
		dst := filepath.Join(config.BackupsDir, e.Name())
		if err := root.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s: %w", e.Name(), err)
		}
	}

	return root.Remove(legacy)
}
