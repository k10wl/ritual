package selfupdate_test

import (
	"os"
	"path/filepath"
	"ritual/internal/subsystems/selfupdate"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupPath_MatchesMinioConvention(t *testing.T) {
	exe := filepath.Join("C:", "app", "ritual.exe")
	want := filepath.Join("C:", "app", ".ritual.exe.old")
	require.Equal(t, want, selfupdate.BackupPath(exe),
		"BackupPath must equal minio/selfupdate's .<exe>.old sidecar or the startup sweep targets the wrong file")
}

func TestCleanupBackup_RemovesSidecarThenReportsAbsent(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ritualdev-local.exe")
	require.NoError(t, os.WriteFile(selfupdate.BackupPath(exe), []byte("old bytes"), 0o644),
		"seed the .old sidecar a prior in-place update would have left")

	removed, err := selfupdate.CleanupBackup(exe)
	require.NoError(t, err)
	require.True(t, removed, "first sweep must remove the existing backup")
	require.NoFileExists(t, selfupdate.BackupPath(exe))

	removed, err = selfupdate.CleanupBackup(exe)
	require.NoError(t, err)
	require.False(t, removed, "a missing backup is not an error and reports nothing removed")
}
