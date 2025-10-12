package domain

import "time"

// Manifest represents the central manifest tracking instance/worlds versions, locks, and metadata
type Manifest struct {
	RitualVersion   string    `json:"ritual_version"`
	LockedBy        string    `json:"locked_by"` // {PC name}__{UNIX timestamp on 0 meridian}, or empty string if not locked
	InstanceVersion string    `json:"instance_version"`
	StoredWorlds    []World   `json:"worlds"` // queue of latest worlds
	UpdatedAt       time.Time `json:"updated_at"`
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
		RitualVersion:   m.RitualVersion,
		LockedBy:        m.LockedBy,
		InstanceVersion: m.InstanceVersion,
		StoredWorlds:    make([]World, len(m.StoredWorlds)),
		UpdatedAt:       time.Now(),
	}

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
