package domain

import "time"

// SyncState is the common sync tracking struct embedded by both sync targets.
type SyncState struct {
	XXHashMap    map[string]string `json:"xxhash_map,omitempty"`
	XXHashSyncAt time.Time        `json:"xxhash_sync_at,omitempty"`
}

// WorldsManifest holds sync state and backup history for worlds.
type WorldsManifest struct {
	SyncState
	Backups []World `json:"backups"`
}

// ServerManifest holds sync state and server configuration.
type ServerManifest struct {
	SyncState
	StartScript string `json:"start_script"`
}
