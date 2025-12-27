package domain

import (
	"ritual/internal/config"
	"time"
)

// Manifest represents the central manifest tracking instance/worlds versions, locks, and metadata
type Manifest struct {
	ManifestVersion string    `json:"manifest_version"`
	RitualVersion   string    `json:"ritual_version"`
	LockedBy        string    `json:"locked_by"` // {hostname}::{nanosecond timestamp}, or empty string if not locked
	InstanceVersion string    `json:"instance_version"`
	StartScript     string    `json:"start_script"` // path to bat file that starts the server (relative to ritual root)
	WorldDirs       []string  `json:"world_dirs"`   // directories to archive (relative to instance dir)
	Backups         []World   `json:"backups"`      // queue of latest backups
	UpdatedAt       time.Time `json:"updated_at"`
	MinRAMMB        int       `json:"min_ram_mb"`       // minimum free RAM in MB required to run (0 = use config default)
	MinDiskMB       int       `json:"min_disk_mb"`      // minimum free disk space in MB required (0 = use config default)
	MinJavaVersion  int       `json:"min_java_version"` // minimum Java version required (0 = use config default)
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
	m.Backups = append(m.Backups, world)
	m.UpdatedAt = time.Now()
}

// GetLatestWorld returns the most recently created world
func (m *Manifest) GetLatestWorld() *World {
	if len(m.Backups) == 0 {
		return nil
	}

	var latest *World
	for i := range m.Backups {
		if latest == nil || m.Backups[i].CreatedAt.After(latest.CreatedAt) {
			latest = &m.Backups[i]
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
		Backups:    make([]World, len(m.Backups)),
		UpdatedAt:       time.Now(),
		MinRAMMB:        m.MinRAMMB,
		MinDiskMB:       m.MinDiskMB,
		MinJavaVersion:  m.MinJavaVersion,
	}

	copy(clone.WorldDirs, m.WorldDirs)
	copy(clone.Backups, m.Backups)
	return clone
}

// RemoveOldestWorlds removes the oldest worlds from the manifest, keeping only the specified count
func (m *Manifest) RemoveOldestWorlds(maxCount int) []World {
	if maxCount <= 0 {
		return nil
	}
	if len(m.Backups) <= maxCount {
		return nil
	}

	// Sort worlds by creation time (oldest first)
	sortedWorlds := make([]World, len(m.Backups))
	copy(sortedWorlds, m.Backups)

	for i := 0; i < len(sortedWorlds)-1; i++ {
		for j := i + 1; j < len(sortedWorlds); j++ {
			if sortedWorlds[i].CreatedAt.After(sortedWorlds[j].CreatedAt) {
				sortedWorlds[i], sortedWorlds[j] = sortedWorlds[j], sortedWorlds[i]
			}
		}
	}

	removedCount := len(m.Backups) - maxCount
	removed := make([]World, removedCount)
	copy(removed, sortedWorlds[:removedCount])

	// Keep only the newest worlds
	m.Backups = sortedWorlds[removedCount:]
	m.UpdatedAt = time.Now()

	return removed
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
}
