package services

import (
	"maps"

	"ritual/internal/core/domain"
)

// ShouldBackup returns true if local and remote sync states differ.
// Pure function — used as gate before creating a backup snapshot.
func ShouldBackup(local, remote domain.SyncState) bool {
	return !maps.Equal(local.XXHashMap, remote.XXHashMap)
}
