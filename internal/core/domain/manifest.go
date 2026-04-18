package domain

import (
	"encoding/json"
	"ritual/internal/config"
	"time"
)

// Manifest represents the central manifest tracking instance/worlds versions, locks, and metadata
type Manifest struct {
	ManifestVersion string    `json:"manifest_version"`
	RitualVersion   string    `json:"ritual_version"`
	LockedBy        string    `json:"locked_by"`
	UpdatedAt       time.Time `json:"updated_at"`
	HeartbeatAt     time.Time `json:"heartbeat_at,omitzero"`

	MinRAMMB       int `json:"min_ram_mb"`
	MinDiskMB      int `json:"min_disk_mb"`
	MinJavaVersion int `json:"min_java_version"`

	Lease           LeaseSettings  `json:"lease"`
	Worlds          WorldsManifest `json:"worlds"`
	Server          ServerManifest `json:"server"`
	RemoteRetention RetentionRules `json:"remote_retention"`
}

// IsLocked returns true if the manifest is currently locked
func (m *Manifest) IsLocked() bool {
	return m.LockedBy != ""
}

// Lock locks the manifest with the provided lock identifier
func (m *Manifest) Lock(lockBy string) {
	m.LockedBy = lockBy
	m.UpdatedAt = time.Now()
}

// Unlock removes the lock from the manifest
func (m *Manifest) Unlock() {
	m.LockedBy = ""
	m.UpdatedAt = time.Now()
}

// Clone creates a deep copy of the manifest
func (m *Manifest) Clone() *Manifest {
	if m == nil {
		return nil
	}

	clone := &Manifest{
		ManifestVersion: m.ManifestVersion,
		RitualVersion:   m.RitualVersion,
		LockedBy:        m.LockedBy,
		UpdatedAt:       time.Now(),
		HeartbeatAt:     m.HeartbeatAt,
		MinRAMMB:        m.MinRAMMB,
		MinDiskMB:       m.MinDiskMB,
		MinJavaVersion:  m.MinJavaVersion,
		Lease:           m.Lease,
		Worlds: WorldsManifest{
			SyncState: SyncState{
				XXHashSyncAt: m.Worlds.XXHashSyncAt,
			},
		},
		Server: ServerManifest{
			SyncState: SyncState{
				XXHashSyncAt: m.Server.XXHashSyncAt,
			},
			StartScript: m.Server.StartScript,
		},
		RemoteRetention: m.RemoteRetention,
	}

	if m.Worlds.XXHashMap != nil {
		clone.Worlds.XXHashMap = make(map[string]FileEntry, len(m.Worlds.XXHashMap))
		for k, v := range m.Worlds.XXHashMap {
			clone.Worlds.XXHashMap[k] = v
		}
	}

	if m.Server.XXHashMap != nil {
		clone.Server.XXHashMap = make(map[string]FileEntry, len(m.Server.XXHashMap))
		for k, v := range m.Server.XXHashMap {
			clone.Server.XXHashMap[k] = v
		}
	}

	return clone
}

// GetMinRAMMB returns the minimum RAM requirement in MB
func (m *Manifest) GetMinRAMMB() int {
	if m.MinRAMMB <= 0 {
		return config.DefaultMinRAMMB
	}
	return m.MinRAMMB
}

// GetMinDiskMB returns the minimum disk space requirement in MB
func (m *Manifest) GetMinDiskMB() int {
	if m.MinDiskMB <= 0 {
		return config.DefaultMinDiskMB
	}
	return m.MinDiskMB
}

// GetMinJavaVersion returns the minimum Java version requirement
func (m *Manifest) GetMinJavaVersion() int {
	if m.MinJavaVersion <= 0 {
		return config.DefaultMinJavaVersion
	}
	return m.MinJavaVersion
}

// ApplyDefaults sets default values for fields that are zero
func (m *Manifest) ApplyDefaults() {
	if m.MinRAMMB <= 0 {
		m.MinRAMMB = config.DefaultMinRAMMB
	}
	if m.MinDiskMB <= 0 {
		m.MinDiskMB = config.DefaultMinDiskMB
	}
	if m.MinJavaVersion <= 0 {
		m.MinJavaVersion = config.DefaultMinJavaVersion
	}
	if m.RemoteRetention == (RetentionRules{}) {
		m.RemoteRetention = DefaultRetentionRules()
	}
	if m.Lease.HeartbeatInterval == 0 {
		m.Lease.HeartbeatInterval = Duration(config.DefaultHeartbeatInterval)
	}
	if m.Lease.TTL == 0 {
		m.Lease.TTL = Duration(config.DefaultLeaseTTL)
	}
}

// IsLeaseActive reports whether the lock is held by a still-living client
// per lease TTL. A stale lease (no heartbeat within TTL) is considered
// free for takeover by the next Acquiring.
func (m *Manifest) IsLeaseActive(now time.Time) bool {
	if m.LockedBy == "" {
		return false
	}
	return now.Sub(m.HeartbeatAt) < time.Duration(m.Lease.TTL)
}

// UnmarshalJSON decodes a Manifest and applies domain defaults as the last
// step. Persistence layers (see adapters.manifestStore) rely on this for
// defaults-on-decode so Save paths can be pure codec+IO.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type alias Manifest
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = Manifest(a)
	m.ApplyDefaults()
	return nil
}
