package domain

import (
	"testing"
	"time"
)

func TestManifest_IsLocked(t *testing.T) {
	tests := []struct {
		name     string
		manifest Manifest
		expected bool
	}{
		{
			name: "unlocked manifest",
			manifest: Manifest{
				LockedBy: "",
			},
			expected: false,
		},
		{
			name: "locked manifest",
			manifest: Manifest{
				LockedBy: "PC123__1640995200",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.manifest.IsLocked()
			if result != tt.expected {
				t.Errorf("IsLocked() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestManifest_Lock(t *testing.T) {
	manifest := Manifest{
		Version:    "1.0.0",
		InstanceID: "test-instance",
		UpdatedAt:  time.Now().Add(-time.Hour), // Set to past time
	}

	lockBy := "PC123__1640995200"
	manifest.Lock(lockBy)

	if manifest.LockedBy != lockBy {
		t.Errorf("Lock() set LockedBy = %v, expected %v", manifest.LockedBy, lockBy)
	}

	if manifest.UpdatedAt.IsZero() {
		t.Error("Lock() should update UpdatedAt timestamp")
	}

	// Check that UpdatedAt was updated to a more recent time
	if manifest.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Error("UpdatedAt should be set to current time")
	}
}

func TestManifest_Unlock(t *testing.T) {
	manifest := Manifest{
		LockedBy:  "PC123__1640995200",
		UpdatedAt: time.Now().Add(-time.Hour), // Set to past time
	}

	manifest.Unlock()

	if manifest.LockedBy != "" {
		t.Errorf("Unlock() should clear LockedBy, got %v", manifest.LockedBy)
	}

	if manifest.UpdatedAt.IsZero() {
		t.Error("Unlock() should update UpdatedAt timestamp")
	}

	// Check that UpdatedAt was updated to a more recent time
	if manifest.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Error("UpdatedAt should be set to current time")
	}
}

func TestManifest_AddWorld(t *testing.T) {
	manifest := Manifest{
		StoredWorlds: []World{},
		UpdatedAt:    time.Now().Add(-time.Hour), // Set to past time
	}

	world := World{
		URI:       "file:///worlds/test-world",
		CreatedAt: time.Now(),
	}

	manifest.AddWorld(world)

	if len(manifest.StoredWorlds) != 1 {
		t.Errorf("AddWorld() should add 1 world, got %d", len(manifest.StoredWorlds))
	}

	if manifest.StoredWorlds[0].URI != world.URI {
		t.Errorf("AddWorld() URI = %v, expected %v", manifest.StoredWorlds[0].URI, world.URI)
	}

	if manifest.UpdatedAt.IsZero() {
		t.Error("AddWorld() should update UpdatedAt timestamp")
	}

	// Check that UpdatedAt was updated to a more recent time
	if manifest.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Error("UpdatedAt should be set to current time")
	}
}

func TestManifest_GetLatestWorld(t *testing.T) {
	tests := []struct {
		name     string
		manifest Manifest
		expected *World
	}{
		{
			name: "empty worlds list",
			manifest: Manifest{
				StoredWorlds: []World{},
			},
			expected: nil,
		},
		{
			name: "single world",
			manifest: Manifest{
				StoredWorlds: []World{
					{URI: "world1", CreatedAt: time.Now()},
				},
			},
			expected: &World{URI: "world1", CreatedAt: time.Now()},
		},
		{
			name: "multiple worlds - latest first",
			manifest: Manifest{
				StoredWorlds: []World{
					{URI: "world3", CreatedAt: time.Now()},
					{URI: "world2", CreatedAt: time.Now().Add(-time.Hour)},
					{URI: "world1", CreatedAt: time.Now().Add(-2 * time.Hour)},
				},
			},
			expected: &World{URI: "world3", CreatedAt: time.Now()},
		},
		{
			name: "multiple worlds - latest in middle",
			manifest: Manifest{
				StoredWorlds: []World{
					{URI: "world1", CreatedAt: time.Now().Add(-2 * time.Hour)},
					{URI: "world3", CreatedAt: time.Now()},
					{URI: "world2", CreatedAt: time.Now().Add(-time.Hour)},
				},
			},
			expected: &World{URI: "world3", CreatedAt: time.Now()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.manifest.GetLatestWorld()

			if tt.expected == nil {
				if result != nil {
					t.Errorf("GetLatestWorld() = %v, expected nil", result)
				}
				return
			}

			if result == nil {
				t.Error("GetLatestWorld() returned nil, expected a world")
				return
			}

			if result.URI != tt.expected.URI {
				t.Errorf("GetLatestWorld() URI = %v, expected %v", result.URI, tt.expected.URI)
			}
		})
	}
}
