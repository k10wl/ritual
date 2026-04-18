package domain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name     string
		local    map[string]FileEntry
		remote   map[string]FileEntry
		expected DiffResult
	}{
		{
			name:     "both empty",
			local:    map[string]FileEntry{},
			remote:   map[string]FileEntry{},
			expected: DiffResult{},
		},
		{
			name:     "both nil",
			local:    nil,
			remote:   nil,
			expected: DiffResult{},
		},
		{
			name:  "nil local, populated remote",
			local: nil,
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			expected: DiffResult{
				Download: []string{"a.dat", "b.dat"},
				Delete:   []string{"a.dat", "b.dat"},
			},
		},
		{
			name: "populated local, nil remote",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			remote: nil,
			expected: DiffResult{
				Upload: []string{"a.dat", "b.dat"},
			},
		},
		{
			name:  "empty local, populated remote downloads all",
			local: map[string]FileEntry{},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			expected: DiffResult{
				Download: []string{"a.dat", "b.dat"},
				Delete:   []string{"a.dat", "b.dat"},
			},
		},
		{
			name: "populated local, empty remote uploads all",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			remote: map[string]FileEntry{},
			expected: DiffResult{
				Upload: []string{"a.dat", "b.dat"},
			},
		},
		{
			name: "matching maps produce nothing",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			expected: DiffResult{},
		},
		{
			name: "one file changed",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1_new", Size: 12},
			},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1_old", Size: 10},
			},
			expected: DiffResult{
				Upload:   []string{"a.dat"},
				Download: []string{"a.dat"},
			},
		},
		{
			name: "file added locally",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
			},
			expected: DiffResult{
				Upload: []string{"b.dat"},
			},
		},
		{
			name: "file deleted locally appears in delete set",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
			},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 10},
				"b.dat": {Hash: "h2", Size: 20},
			},
			expected: DiffResult{
				Download: []string{"b.dat"},
				Delete:   []string{"b.dat"},
			},
		},
		{
			name: "multiple mixed changes",
			local: map[string]FileEntry{
				"unchanged.dat": {Hash: "same", Size: 10},
				"changed.dat":   {Hash: "new_hash", Size: 22},
				"added.dat":     {Hash: "local_only", Size: 30},
			},
			remote: map[string]FileEntry{
				"unchanged.dat": {Hash: "same", Size: 10},
				"changed.dat":   {Hash: "old_hash", Size: 20},
				"deleted.dat":   {Hash: "remote_only", Size: 40},
			},
			expected: DiffResult{
				Upload:   []string{"added.dat", "changed.dat"},
				Download: []string{"changed.dat", "deleted.dat"},
				Delete:   []string{"deleted.dat"},
			},
		},
		{
			name: "size differs but hash same — no diff",
			local: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 100},
			},
			remote: map[string]FileEntry{
				"a.dat": {Hash: "h1", Size: 200},
			},
			expected: DiffResult{},
		},
		{
			name: "nested paths sorted correctly",
			local: map[string]FileEntry{
				"world/region/r.0.1.mca": {Hash: "h1", Size: 1024},
				"world/region/r.0.0.mca": {Hash: "h2", Size: 1024},
				"world/level.dat":        {Hash: "h3", Size: 256},
			},
			remote: map[string]FileEntry{},
			expected: DiffResult{
				Upload: []string{"world/level.dat", "world/region/r.0.0.mca", "world/region/r.0.1.mca"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeDiff(tt.local, tt.remote)
			assert.Equal(t, tt.expected.Upload, result.Upload, "Upload mismatch")
			assert.Equal(t, tt.expected.Download, result.Download, "Download mismatch")
			assert.Equal(t, tt.expected.Delete, result.Delete, "Delete mismatch")
		})
	}
}

func TestComputeDiff_LargeMap(t *testing.T) {
	local := make(map[string]FileEntry, 1000)
	remote := make(map[string]FileEntry, 1000)

	for i := range 900 {
		key := fmt.Sprintf("world/region/r.%d.%d.mca", i/30, i%30)
		entry := FileEntry{Hash: fmt.Sprintf("hash_%d", i), Size: int64(i + 1)}
		local[key] = entry
		remote[key] = entry
	}

	for i := 900; i < 950; i++ {
		key := fmt.Sprintf("world/region/r.%d.%d.mca", i/30, i%30)
		local[key] = FileEntry{Hash: fmt.Sprintf("local_hash_%d", i), Size: int64(i + 1)}
		remote[key] = FileEntry{Hash: fmt.Sprintf("remote_hash_%d", i), Size: int64(i + 1)}
	}

	for i := 950; i < 975; i++ {
		key := fmt.Sprintf("world/new/file_%d.dat", i)
		local[key] = FileEntry{Hash: fmt.Sprintf("new_hash_%d", i), Size: int64(i + 1)}
	}

	for i := 975; i < 1000; i++ {
		key := fmt.Sprintf("world/old/file_%d.dat", i)
		remote[key] = FileEntry{Hash: fmt.Sprintf("old_hash_%d", i), Size: int64(i + 1)}
	}

	result := ComputeDiff(local, remote)

	assert.Len(t, result.Upload, 75, "50 changed + 25 new locally")
	assert.Len(t, result.Download, 75, "50 changed + 25 new on remote")
	assert.Len(t, result.Delete, 25, "25 remote-only files")

	for i := 1; i < len(result.Upload); i++ {
		assert.True(t, result.Upload[i-1] <= result.Upload[i], "Upload not sorted at index %d", i)
	}
	for i := 1; i < len(result.Download); i++ {
		assert.True(t, result.Download[i-1] <= result.Download[i], "Download not sorted at index %d", i)
	}
	for i := 1; i < len(result.Delete); i++ {
		assert.True(t, result.Delete[i-1] <= result.Delete[i], "Delete not sorted at index %d", i)
	}
}
