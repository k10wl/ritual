package relocating

import (
	"os"
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

// cleanup best-effort closes and removes the old root. Explicitly out of
// scope to make this robust (Crash safety § "stale files are not our
// concern") — errors are swallowed, not surfaced.
func cleanup(oldRoot *os.Root, oldDir string) {
	_ = oldRoot.Close()
	_ = os.RemoveAll(oldDir)
}
