package selfupdate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// BackupPath returns the sidecar path minio/selfupdate renames the running
// binary to during an in-place update: ".<exe>.old" alongside the executable
// (minio/selfupdate apply.go — fmt.Sprintf(".%s.old", filename)). On Windows the
// library cannot delete this while the old process is still alive, so it lingers
// (hidden) until something removes it. We sweep it on the next launch.
func BackupPath(exePath string) string {
	return filepath.Join(filepath.Dir(exePath), "."+filepath.Base(exePath)+".old")
}

// CleanupBackup removes the leftover .old backup from a prior self-update, if
// present. Safe to call every launch: a missing file is not an error (returns
// false, nil), and a still-locked file is surfaced as an error for the caller
// to log and retry next launch. Returns true when a file was actually removed.
func CleanupBackup(exePath string) (bool, error) {
	if err := os.Remove(BackupPath(exePath)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
