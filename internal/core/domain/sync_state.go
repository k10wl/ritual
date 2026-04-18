package domain

import "time"

// FileEntry pairs a file's content hash with its size in bytes.
// Hash is used for change detection; Size feeds plan/progress events.
type FileEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// SyncState is the common sync tracking struct embedded by both sync targets.
type SyncState struct {
	XXHashMap    map[string]FileEntry `json:"xxhash_map,omitempty"`
	XXHashSyncAt time.Time            `json:"xxhash_sync_at,omitempty"`
}

// WorldsManifest holds sync state for worlds.
type WorldsManifest struct {
	SyncState
}

// ServerManifest holds sync state and server configuration.
type ServerManifest struct {
	SyncState
	StartScript string `json:"start_script"`
}
