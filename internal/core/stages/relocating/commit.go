package relocating

import (
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/domain"
)

// commit is the Durability step (Crash safety §, design-log/055): writes
// settings.WorkRoot = dst and persists it via Settings.Save() (now an
// atomic temp-file+fsync+rename write). Also updates the in-process
// config.WorkRoot var so every live-rederivation call site in THIS process
// (scanner FS, disk-check, OpenRootFolder, the cmdBuilder/consoleReader
// rebuild) observes the new root immediately — settings.json alone only
// matters across a process restart; within this process config.WorkRoot is
// what live-rederiving readers actually consult.
func commit(settings *domain.Settings, dst string) error {
	settings.WorkRoot = dst
	if err := settings.Save(); err != nil {
		return err
	}
	config.WorkRoot = dst
	return nil
}

// cleanup best-effort closes the old root and removes ONLY the CONTENT
// subdirectories it held (contentDirs — objects/refs/server/worlds), never
// the old root directory itself. This is load-bearing, not cosmetic: on a
// never-relocated install config.WorkRoot defaults to config.RootPath, so
// the "old content root" IS the CONTROL root — a blanket
// os.RemoveAll(oldDir) there would delete settings.json/lock/logs/ right
// after commit() durably wrote the new work_root into that same
// settings.json, orphaning it (live repro, 2026-08-11: exactly this
// happened — the relocated data landed safely at the new destination, but
// the pointer to it, and the whole control root, was wiped seconds later).
// Leftover stale content-subdir files on a crash mid-cleanup are still
// explicitly out of scope (Crash safety § "stale files are not our
// concern") — errors are swallowed, not surfaced.
func cleanup(oldRoot *os.Root, oldDir string) {
	_ = oldRoot.Close()
	for _, dir := range contentDirs {
		_ = os.RemoveAll(filepath.Join(oldDir, dir))
	}
}
