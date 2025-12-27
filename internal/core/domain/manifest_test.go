package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
				LockedBy: "PC123::1640995200",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.manifest.IsLocked()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManifest_Lock(t *testing.T) {
	manifest := Manifest{
		RitualVersion:   "1.0.0",
		InstanceVersion: "test-instance",
		UpdatedAt:       time.Now().Add(-time.Hour), // Set to past time
	}

	lockBy := "PC123::1640995200"
	manifest.Lock(lockBy)

	assert.Equal(t, lockBy, manifest.LockedBy)
	assert.False(t, manifest.UpdatedAt.IsZero(), "Lock() should update UpdatedAt timestamp")
	assert.True(t, manifest.UpdatedAt.After(time.Now().Add(-time.Minute)), "UpdatedAt should be set to current time")
}

func TestManifest_Unlock(t *testing.T) {
	manifest := Manifest{
		LockedBy:  "PC123::1640995200",
		UpdatedAt: time.Now().Add(-time.Hour), // Set to past time
	}

	manifest.Unlock()

	assert.Empty(t, manifest.LockedBy, "Unlock() should clear LockedBy")
	assert.False(t, manifest.UpdatedAt.IsZero(), "Unlock() should update UpdatedAt timestamp")
	assert.True(t, manifest.UpdatedAt.After(time.Now().Add(-time.Minute)), "UpdatedAt should be set to current time")
}

func TestManifest_AddWorld(t *testing.T) {
	manifest := Manifest{
		Backups: []World{},
		UpdatedAt:    time.Now().Add(-time.Hour), // Set to past time
	}

	world := World{
		URI:       "file:///worlds/test-world",
		CreatedAt: time.Now(),
	}

	manifest.AddWorld(world)

	assert.Len(t, manifest.Backups, 1, "AddWorld() should add 1 world")
	assert.Equal(t, world.URI, manifest.Backups[0].URI)
	assert.False(t, manifest.UpdatedAt.IsZero(), "AddWorld() should update UpdatedAt timestamp")
	assert.True(t, manifest.UpdatedAt.After(time.Now().Add(-time.Minute)), "UpdatedAt should be set to current time")
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
				Backups: []World{},
			},
			expected: nil,
		},
		{
			name: "single world",
			manifest: Manifest{
				Backups: []World{
					{URI: "world1", CreatedAt: time.Now()},
				},
			},
			expected: &World{URI: "world1", CreatedAt: time.Now()},
		},
		{
			name: "multiple worlds - latest first",
			manifest: Manifest{
				Backups: []World{
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
				Backups: []World{
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
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result, "GetLatestWorld() returned nil, expected a world")
			assert.Equal(t, tt.expected.URI, result.URI)
		})
	}
}
