package adapters_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
)

func TestFullScanner_Scan(t *testing.T) {
	fsys := fstest.MapFS{
		"world/level.dat":        &fstest.MapFile{Data: []byte("level data")},
		"world/region/r.0.0.mca": &fstest.MapFile{Data: []byte("region data")},
	}
	scanner := adapters.NewFullScanner(fsys)
	result, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Contains(t, result, "world/level.dat")
	assert.Contains(t, result, "world/region/r.0.0.mca")
	// Verify hashes are non-empty hex strings
	for _, hash := range result {
		assert.Len(t, hash, 16, "xxhash should be 16 hex chars")
	}
}

func TestFullScanner_EmptyFS(t *testing.T) {
	fsys := fstest.MapFS{}
	scanner := adapters.NewFullScanner(fsys)
	result, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFullScanner_DeterministicHash(t *testing.T) {
	data := []byte("consistent content")
	fsys1 := fstest.MapFS{"file.txt": &fstest.MapFile{Data: data}}
	fsys2 := fstest.MapFS{"file.txt": &fstest.MapFile{Data: data}}

	s1 := adapters.NewFullScanner(fsys1)
	s2 := adapters.NewFullScanner(fsys2)

	r1, _ := s1.Scan(context.Background())
	r2, _ := s2.Scan(context.Background())

	assert.Equal(t, r1["file.txt"], r2["file.txt"])
}

func TestFullScanner_ContextCancellation(t *testing.T) {
	fsys := fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("data")}}
	scanner := adapters.NewFullScanner(fsys)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := scanner.Scan(ctx)
	assert.Error(t, err)
}
