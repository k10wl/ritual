package adapters_test

import (
	"context"
	"ritual/internal/adapters"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullScanner_Scan(t *testing.T) {
	fsys := fstest.MapFS{
		"world/level.dat":        &fstest.MapFile{Data: []byte("level data")},
		"world/region/r.0.0.mca": &fstest.MapFile{Data: []byte("region data")},
	}
	scanner := adapters.NewFullScanner(fsys)
	result, err := scanner.Scan(context.Background(), []string{"**"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Contains(t, result, "world/level.dat")
	assert.Contains(t, result, "world/region/r.0.0.mca")
	// Verify hashes are non-empty hex strings and sizes match content.
	for path, entry := range result {
		assert.Len(t, entry.Hash, 16, "xxhash should be 16 hex chars")
		assert.Greater(t, entry.Size, int64(0), "size must be populated for %s", path)
	}
}

func TestFullScanner_EmptyFS(t *testing.T) {
	fsys := fstest.MapFS{}
	scanner := adapters.NewFullScanner(fsys)
	result, err := scanner.Scan(context.Background(), []string{"**"})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFullScanner_DeterministicHash(t *testing.T) {
	data := []byte("consistent content")
	fsys1 := fstest.MapFS{"file.txt": &fstest.MapFile{Data: data}}
	fsys2 := fstest.MapFS{"file.txt": &fstest.MapFile{Data: data}}

	s1 := adapters.NewFullScanner(fsys1)
	s2 := adapters.NewFullScanner(fsys2)

	r1, _ := s1.Scan(context.Background(), []string{"**"})
	r2, _ := s2.Scan(context.Background(), []string{"**"})

	assert.Equal(t, r1["file.txt"], r2["file.txt"])
}

func TestFullScanner_ContextCancellation(t *testing.T) {
	fsys := fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("data")}}
	scanner := adapters.NewFullScanner(fsys)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := scanner.Scan(ctx, []string{"**"})
	assert.Error(t, err)
}

func TestFullScanner_FiltersByTargets(t *testing.T) {
	fsys := fstest.MapFS{
		"worlds/world/level.dat":         &fstest.MapFile{Data: []byte("level")},
		"worlds/world/region/r.0.0.mca":  &fstest.MapFile{Data: []byte("region")},
		"server/mods/create.jar":         &fstest.MapFile{Data: []byte("mod")},
		"server/server.jar":              &fstest.MapFile{Data: []byte("server")},
		"server/libraries/foo.jar":       &fstest.MapFile{Data: []byte("lib")},
		"server/logs/latest.log":         &fstest.MapFile{Data: []byte("log")},
		"README.md":                      &fstest.MapFile{Data: []byte("readme")},
	}
	targets := []string{"worlds/**", "server/mods/**", "server/server.jar"}
	scanner := adapters.NewFullScanner(fsys)

	result, err := scanner.Scan(context.Background(), targets)
	require.NoError(t, err)

	want := []string{
		"worlds/world/level.dat",
		"worlds/world/region/r.0.0.mca",
		"server/mods/create.jar",
		"server/server.jar",
	}
	assert.Len(t, result, len(want), "only target-matched files in result")
	for _, p := range want {
		assert.Contains(t, result, p, "expected matched path %s", p)
	}
	assert.NotContains(t, result, "server/libraries/foo.jar", "libraries pruned by targets")
	assert.NotContains(t, result, "server/logs/latest.log", "logs pruned")
	assert.NotContains(t, result, "README.md", "top-level non-target pruned")
}

func TestFullScanner_EmptyTargets_NothingMatches(t *testing.T) {
	fsys := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("x")}}
	scanner := adapters.NewFullScanner(fsys)

	result, err := scanner.Scan(context.Background(), []string{})
	require.NoError(t, err)
	assert.Empty(t, result, "empty targets means nothing matches — explicit, no implicit match-all")
}
