package adapters_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
)

func TestParseRitualSync(t *testing.T) {
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		paths   []string
		expect  []bool
		wantErr bool
	}{
		{
			name:   "wildcard matches everything",
			fsys:   fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("*\n")}},
			paths:  []string{"a.txt", "dir/b.txt", "deep/nested/c.txt"},
			expect: []bool{true, true, true},
		},
		{
			name:   "exact file match",
			fsys:   fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("server.jar\n")}},
			paths:  []string{"server.jar", "server.jar.bak", "other.txt"},
			expect: []bool{true, false, false},
		},
		{
			name:   "directory match with trailing slash",
			fsys:   fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("config/\n")}},
			paths:  []string{"config/a.cfg", "config/sub/b.cfg", "configstuff.txt"},
			expect: []bool{true, true, false},
		},
		{
			name:   "mods/ rejects modstuff.txt",
			fsys:   fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("mods/\n")}},
			paths:  []string{"mods/a.jar", "modstuff.txt"},
			expect: []bool{true, false},
		},
		{
			name:    "empty file errors",
			fsys:    fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("")}},
			wantErr: true,
		},
		{
			name:    "missing file errors",
			fsys:    fstest.MapFS{},
			wantErr: true,
		},
		{
			name:   "comments and blank lines skipped",
			fsys:   fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("# comment\n\nserver.jar\n")}},
			paths:  []string{"server.jar", "other"},
			expect: []bool{true, false},
		},
		{
			name:    "path traversal rejected",
			fsys:    fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("../escape\n")}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := adapters.ParseRitualSync(tt.fsys)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			for i, path := range tt.paths {
				assert.Equal(t, tt.expect[i], filter(path), "path: %s", path)
			}
		})
	}
}

func TestFilteredScanner_WhitelistApplied(t *testing.T) {
	inner := &mockScanner{result: map[string]domain.FileEntry{
		".ritualsync":     {Hash: "hash0", Size: 1},
		"server.jar":      {Hash: "hash1", Size: 100},
		"config/a.cfg":    {Hash: "hash2", Size: 50},
		"logs/latest.log": {Hash: "hash3", Size: 200},
		"cache/data":      {Hash: "hash4", Size: 80},
	}}
	filter := func(path string) bool {
		return path == "server.jar" || strings.HasPrefix(path, "config/")
	}
	scanner := adapters.NewFilteredScanner(inner, filter)
	result, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 3) // .ritualsync + server.jar + config/a.cfg
	assert.Contains(t, result, ".ritualsync")
	assert.Contains(t, result, "server.jar")
	assert.Contains(t, result, "config/a.cfg")
	assert.NotContains(t, result, "logs/latest.log")
	assert.NotContains(t, result, "cache/data")
}

func TestFilteredScanner_InnerError(t *testing.T) {
	inner := &mockScanner{err: errors.New("scan failed")}
	scanner := adapters.NewFilteredScanner(inner, func(string) bool { return true })
	_, err := scanner.Scan(context.Background())
	assert.Error(t, err)
}

type mockScanner struct {
	result map[string]domain.FileEntry
	err    error
}

func (m *mockScanner) Scan(ctx context.Context) (map[string]domain.FileEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return maps.Clone(m.result), nil
}
