package domain

import "time"

// Manifest represents the central manifest tracking instance/worlds versions, locks, and metadata
type Manifest struct {
	ManifestVersion string    `json:"manifest_version"`
	RitualVersion   string    `json:"ritual_version"`
	LockedBy        string    `json:"locked_by"` // {hostname}::{nanosecond timestamp}, or empty string if not locked
	InstanceVersion string    `json:"instance_version"`
	StartScript     string    `json:"start_script"` // path to bat file that starts the server (relative to ritual root)
	WorldDirs       []string  `json:"world_dirs"`   // directories to archive (relative to instance dir)
	StoredWorlds    []World   `json:"worlds"`       // queue of latest worlds
	UpdatedAt       time.Time `json:"updated_at"`
	MinRAMMB        int       `json:"min_ram_mb"`       // minimum free RAM in MB required to run (0 = use default 4096)
	MinDiskMB       int       `json:"min_disk_mb"`      // minimum free disk space in MB required (0 = use default 5120)
	MinJavaVersion  int       `json:"min_java_version"` // minimum Java version required (0 = use default 21)
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

// AddWorld adds a new world to the stored worlds queue
func (m *Manifest) AddWorld(world World) {
	m.StoredWorlds = append(m.StoredWorlds, world)
	m.UpdatedAt = time.Now()
}

// GetLatestWorld returns the most recently created world
func (m *Manifest) GetLatestWorld() *World {
	if len(m.StoredWorlds) == 0 {
		return nil
	}

	var latest *World
	for i := range m.StoredWorlds {
		if latest == nil || m.StoredWorlds[i].CreatedAt.After(latest.CreatedAt) {
			latest = &m.StoredWorlds[i]
		}
	}
	return latest
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
		InstanceVersion: m.InstanceVersion,
		StartScript:     m.StartScript,
		WorldDirs:       make([]string, len(m.WorldDirs)),
		StoredWorlds:    make([]World, len(m.StoredWorlds)),
		UpdatedAt:       time.Now(),
		MinRAMMB:        m.MinRAMMB,
		MinDiskMB:       m.MinDiskMB,
		MinJavaVersion:  m.MinJavaVersion,
	}

	copy(clone.WorldDirs, m.WorldDirs)
	copy(clone.StoredWorlds, m.StoredWorlds)
	return clone
}

// RemoveOldestWorlds removes the oldest worlds from the manifest, keeping only the specified count
func (m *Manifest) RemoveOldestWorlds(maxCount int) []World {
	if maxCount <= 0 {
		return nil
	}
	if len(m.StoredWorlds) <= maxCount {
		return nil
	}

	// Sort worlds by creation time (oldest first)
	sortedWorlds := make([]World, len(m.StoredWorlds))
	copy(sortedWorlds, m.StoredWorlds)

	for i := 0; i < len(sortedWorlds)-1; i++ {
		for j := i + 1; j < len(sortedWorlds); j++ {
			if sortedWorlds[i].CreatedAt.After(sortedWorlds[j].CreatedAt) {
				sortedWorlds[i], sortedWorlds[j] = sortedWorlds[j], sortedWorlds[i]
			}
		}
	}

	removedCount := len(m.StoredWorlds) - maxCount
	removed := make([]World, removedCount)
	copy(removed, sortedWorlds[:removedCount])

	// Keep only the newest worlds
	m.StoredWorlds = sortedWorlds[removedCount:]
	m.UpdatedAt = time.Now()

	return removed
}

// GetMinRAMMB returns the minimum RAM requirement in MB, defaulting to 4096 (4GB) if not set
func (m *Manifest) GetMinRAMMB() int {
	if m.MinRAMMB <= 0 {
		return 4096
	}
	return m.MinRAMMB
}

// GetMinDiskMB returns the minimum disk space requirement in MB, defaulting to 5120 (5GB) if not set
func (m *Manifest) GetMinDiskMB() int {
	if m.MinDiskMB <= 0 {
		return 5120
	}
	return m.MinDiskMB
}

// GetMinJavaVersion returns the minimum Java version requirement, defaulting to 21 if not set
func (m *Manifest) GetMinJavaVersion() int {
	if m.MinJavaVersion <= 0 {
		return 21
	}
	return m.MinJavaVersion
}
