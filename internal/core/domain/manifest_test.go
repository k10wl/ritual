package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		RitualVersion: "1.0.0",
		UpdatedAt:     time.Now().Add(-time.Hour),
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
		UpdatedAt: time.Now().Add(-time.Hour),
	}

	manifest.Unlock()

	assert.Empty(t, manifest.LockedBy, "Unlock() should clear LockedBy")
	assert.False(t, manifest.UpdatedAt.IsZero(), "Unlock() should update UpdatedAt timestamp")
	assert.True(t, manifest.UpdatedAt.After(time.Now().Add(-time.Minute)), "UpdatedAt should be set to current time")
}

func TestManifest_AddWorld(t *testing.T) {
	manifest := Manifest{
		Worlds:    WorldsManifest{Backups: []World{}},
		UpdatedAt: time.Now().Add(-time.Hour),
	}

	world := World{
		URI:       "file:///worlds/test-world",
		CreatedAt: time.Now(),
	}

	manifest.AddWorld(world)

	assert.Len(t, manifest.Worlds.Backups, 1, "AddWorld() should add 1 world")
	assert.Equal(t, world.URI, manifest.Worlds.Backups[0].URI)
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
				Worlds: WorldsManifest{Backups: []World{}},
			},
			expected: nil,
		},
		{
			name: "single world",
			manifest: Manifest{
				Worlds: WorldsManifest{
					Backups: []World{
						{URI: "world1", CreatedAt: time.Now()},
					},
				},
			},
			expected: &World{URI: "world1", CreatedAt: time.Now()},
		},
		{
			name: "multiple worlds - latest first",
			manifest: Manifest{
				Worlds: WorldsManifest{
					Backups: []World{
						{URI: "world3", CreatedAt: time.Now()},
						{URI: "world2", CreatedAt: time.Now().Add(-time.Hour)},
						{URI: "world1", CreatedAt: time.Now().Add(-2 * time.Hour)},
					},
				},
			},
			expected: &World{URI: "world3", CreatedAt: time.Now()},
		},
		{
			name: "multiple worlds - latest in middle",
			manifest: Manifest{
				Worlds: WorldsManifest{
					Backups: []World{
						{URI: "world1", CreatedAt: time.Now().Add(-2 * time.Hour)},
						{URI: "world3", CreatedAt: time.Now()},
						{URI: "world2", CreatedAt: time.Now().Add(-time.Hour)},
					},
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

func TestManifest_XXHashMap_MarshalRoundtrip(t *testing.T) {
	original := Manifest{
		ManifestVersion: "1.0.0",
		RitualVersion:   "2.0.0",
		Worlds: WorldsManifest{
			SyncState: SyncState{
				XXHashMap: map[string]string{
					"world/region/r.0.0.mca": "a1b2c3d4e5f6",
					"world/level.dat":        "1a2b3c4d5e6f",
				},
				XXHashSyncAt: time.Date(2026, 4, 14, 10, 30, 0, 0, time.UTC),
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Manifest
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.Worlds.XXHashMap, restored.Worlds.XXHashMap)
	assert.True(t, original.Worlds.XXHashSyncAt.Equal(restored.Worlds.XXHashSyncAt))
}

func TestManifest_XXHashMap_V1BackwardsCompat(t *testing.T) {
	v1JSON := `{"manifest_version":"1.0.0","ritual_version":"1.0.0","locked_by":"","updated_at":"2026-01-01T00:00:00Z","worlds":{"backups":null},"server":{}}`

	var manifest Manifest
	err := json.Unmarshal([]byte(v1JSON), &manifest)
	require.NoError(t, err)

	assert.Nil(t, manifest.Worlds.XXHashMap, "v1 manifest should have nil XXHashMap")
	assert.True(t, manifest.Worlds.XXHashSyncAt.IsZero(), "v1 manifest should have zero XXHashSyncAt")
}

func TestManifest_Clone_DeepCopiesXXHashMap(t *testing.T) {
	original := &Manifest{
		Worlds: WorldsManifest{
			SyncState: SyncState{
				XXHashMap: map[string]string{
					"world/level.dat": "abc123",
				},
				XXHashSyncAt: time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	clone := original.Clone()

	assert.Equal(t, original.Worlds.XXHashMap, clone.Worlds.XXHashMap)
	assert.True(t, original.Worlds.XXHashSyncAt.Equal(clone.Worlds.XXHashSyncAt))

	// mutating clone must not affect original
	clone.Worlds.XXHashMap["world/level.dat"] = "modified"
	assert.Equal(t, "abc123", original.Worlds.XXHashMap["world/level.dat"])
}

func TestManifest_Clone_NilXXHashMap(t *testing.T) {
	original := &Manifest{
		Worlds: WorldsManifest{
			SyncState: SyncState{XXHashMap: nil},
		},
	}

	clone := original.Clone()
	assert.Nil(t, clone.Worlds.XXHashMap)
}
