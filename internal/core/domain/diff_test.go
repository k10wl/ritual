package domain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name     string
		local    map[string]string
		remote   map[string]string
		expected DiffResult
	}{
		{
			name:   "both empty",
			local:  map[string]string{},
			remote: map[string]string{},
			expected: DiffResult{
				Upload:   nil,
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:  "both nil",
			local: nil,
			remote: nil,
			expected: DiffResult{
				Upload:   nil,
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:   "nil local, populated remote",
			local:  nil,
			remote: map[string]string{"a.dat": "h1", "b.dat": "h2"},
			expected: DiffResult{
				Upload:   nil,
				Download: []string{"a.dat", "b.dat"},
				Delete:   []string{"a.dat", "b.dat"},
			},
		},
		{
			name:   "populated local, nil remote",
			local:  map[string]string{"a.dat": "h1", "b.dat": "h2"},
			remote: nil,
			expected: DiffResult{
				Upload:   []string{"a.dat", "b.dat"},
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:   "empty local, populated remote downloads all",
			local:  map[string]string{},
			remote: map[string]string{"a.dat": "h1", "b.dat": "h2"},
			expected: DiffResult{
				Upload:   nil,
				Download: []string{"a.dat", "b.dat"},
				Delete:   []string{"a.dat", "b.dat"},
			},
		},
		{
			name:   "populated local, empty remote uploads all",
			local:  map[string]string{"a.dat": "h1", "b.dat": "h2"},
			remote: map[string]string{},
			expected: DiffResult{
				Upload:   []string{"a.dat", "b.dat"},
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:   "matching maps produce nothing",
			local:  map[string]string{"a.dat": "h1", "b.dat": "h2"},
			remote: map[string]string{"a.dat": "h1", "b.dat": "h2"},
			expected: DiffResult{
				Upload:   nil,
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:   "one file changed",
			local:  map[string]string{"a.dat": "h1_new"},
			remote: map[string]string{"a.dat": "h1_old"},
			expected: DiffResult{
				Upload:   []string{"a.dat"},
				Download: []string{"a.dat"},
				Delete:   nil,
			},
		},
		{
			name:   "file added locally",
			local:  map[string]string{"a.dat": "h1", "b.dat": "h2"},
			remote: map[string]string{"a.dat": "h1"},
			expected: DiffResult{
				Upload:   []string{"b.dat"},
				Download: nil,
				Delete:   nil,
			},
		},
		{
			name:   "file deleted locally appears in delete set",
			local:  map[string]string{"a.dat": "h1"},
			remote: map[string]string{"a.dat": "h1", "b.dat": "h2"},
			expected: DiffResult{
				Upload:   nil,
				Download: []string{"b.dat"},
				Delete:   []string{"b.dat"},
			},
		},
		{
			name: "multiple mixed changes",
			local: map[string]string{
				"unchanged.dat": "same",
				"changed.dat":   "new_hash",
				"added.dat":     "local_only",
			},
			remote: map[string]string{
				"unchanged.dat": "same",
				"changed.dat":   "old_hash",
				"deleted.dat":   "remote_only",
			},
			expected: DiffResult{
				Upload:   []string{"added.dat", "changed.dat"},
				Download: []string{"changed.dat", "deleted.dat"},
				Delete:   []string{"deleted.dat"},
			},
		},
		{
			name: "nested paths sorted correctly",
			local: map[string]string{
				"world/region/r.0.1.mca": "h1",
				"world/region/r.0.0.mca": "h2",
				"world/level.dat":        "h3",
			},
			remote: map[string]string{},
			expected: DiffResult{
				Upload:   []string{"world/level.dat", "world/region/r.0.0.mca", "world/region/r.0.1.mca"},
				Download: nil,
				Delete:   nil,
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
	local := make(map[string]string, 1000)
	remote := make(map[string]string, 1000)

	// 900 unchanged files
	for i := 0; i < 900; i++ {
		key := fmt.Sprintf("world/region/r.%d.%d.mca", i/30, i%30)
		hash := fmt.Sprintf("hash_%d", i)
		local[key] = hash
		remote[key] = hash
	}

	// 50 changed files
	for i := 900; i < 950; i++ {
		key := fmt.Sprintf("world/region/r.%d.%d.mca", i/30, i%30)
		local[key] = fmt.Sprintf("local_hash_%d", i)
		remote[key] = fmt.Sprintf("remote_hash_%d", i)
	}

	// 25 local-only files (new locally)
	for i := 950; i < 975; i++ {
		key := fmt.Sprintf("world/new/file_%d.dat", i)
		local[key] = fmt.Sprintf("new_hash_%d", i)
	}

	// 25 remote-only files (deleted locally)
	for i := 975; i < 1000; i++ {
		key := fmt.Sprintf("world/old/file_%d.dat", i)
		remote[key] = fmt.Sprintf("old_hash_%d", i)
	}

	result := ComputeDiff(local, remote)

	assert.Len(t, result.Upload, 75, "50 changed + 25 new locally")
	assert.Len(t, result.Download, 75, "50 changed + 25 new on remote")
	assert.Len(t, result.Delete, 25, "25 remote-only files")

	// verify sorted
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
