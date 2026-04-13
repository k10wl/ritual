package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullWorldScanner_NewFullWorldScanner_EmptyRoot(t *testing.T) {
	_, err := NewFullWorldScanner("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "root directory cannot be empty")
}

func TestFullWorldScanner_Scan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestFullWorldScanner_Scan_SingleFile(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "level.dat"), []byte("test data"), 0644)
	require.NoError(t, err)

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Contains(t, result, "level.dat")
	assert.NotEmpty(t, result["level.dat"])
}

func TestFullWorldScanner_Scan_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	regionDir := filepath.Join(dir, "world", "region")
	require.NoError(t, os.MkdirAll(regionDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("level"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(regionDir, "r.0.0.mca"), []byte("region0"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(regionDir, "r.0.1.mca"), []byte("region1"), 0644))

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Contains(t, result, "world/level.dat")
	assert.Contains(t, result, "world/region/r.0.0.mca")
	assert.Contains(t, result, "world/region/r.0.1.mca")
}

func TestFullWorldScanner_Scan_ForwardSlashes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "file.dat"), []byte("data"), 0644))

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, result, "a/b/c/file.dat")
}

func TestFullWorldScanner_Scan_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.dat"), []byte("aaa"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.dat"), []byte("bbb"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.dat"), []byte("ccc"), 0644))

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Contains(t, result, "a.dat")
	assert.Contains(t, result, "b.dat")
	assert.Contains(t, result, "c.dat")
}

func TestFullWorldScanner_Scan_DeterministicHash(t *testing.T) {
	dir := t.TempDir()
	content := []byte("deterministic content")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), content, 0644))

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	result1, err := scanner.Scan(context.Background())
	require.NoError(t, err)

	result2, err := scanner.Scan(context.Background())
	require.NoError(t, err)

	assert.Equal(t, result1["file.dat"], result2["file.dat"])
}

func TestFullWorldScanner_Scan_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), []byte("data"), 0644))

	scanner, err := NewFullWorldScanner(dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = scanner.Scan(ctx)
	assert.Error(t, err)
}

func TestFullWorldScanner_Scan_NonExistentRoot(t *testing.T) {
	scanner, err := NewFullWorldScanner("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err)

	_, err = scanner.Scan(context.Background())
	assert.Error(t, err)
}
