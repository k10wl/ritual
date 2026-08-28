package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveDirFS_ReResolvesPathFnOnEachScan(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(firstDir, "a.txt"), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secondDir, "b.txt"), []byte("second"), 0o644))

	current := firstDir
	fsys := LiveDirFS(func() string { return current })
	scanner := NewFullScanner(fsys)

	entries, err := scanner.Scan(context.Background(), []string{"**"})
	require.NoError(t, err)
	_, hasA := entries["a.txt"]
	assert.True(t, hasA, "first Scan must read from firstDir")

	current = secondDir
	entries, err = scanner.Scan(context.Background(), []string{"**"})
	require.NoError(t, err, "a second Scan against the SAME FullScanner instance must succeed after pathFn's return value changes")
	_, hasB := entries["b.txt"]
	assert.True(t, hasB, "the second Scan must read from secondDir without reconstructing FullScanner — this is what protects a relocate from leaving applier/committer reading the old work root (design-log/055 Q4)")
	_, hasAStill := entries["a.txt"]
	assert.False(t, hasAStill, "the second Scan must NOT see firstDir's content anymore")
}
