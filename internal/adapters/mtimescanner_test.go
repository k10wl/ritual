package adapters

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/core/domain"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMtimeScanner_NewMtimeScanner_EmptyRoot(t *testing.T) {
	_, err := NewMtimeScanner("", time.Now(), map[string]domain.FileEntry{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "root directory cannot be empty")
}

func TestMtimeScanner_NewMtimeScanner_NilPreviousTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), []byte("data"), 0o644))

	scanner, err := NewMtimeScanner(dir, time.Now().Add(-time.Hour), nil)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})
	assert.NoError(t, err)
	assert.Contains(t, result, "file.dat", "nil previous should hash all files like empty map")
}

func TestMtimeScanner_Scan_ModifiedAfterThreshold(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "modified.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("new content"), 0o644))

	// Set threshold to past — file is "modified after"
	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeScanner(dir, threshold, map[string]domain.FileEntry{
		"modified.dat": {Hash: "old_hash", Size: 11},
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.Contains(t, result, "modified.dat")
	assert.NotEqual(t, "old_hash", result["modified.dat"].Hash, "should compute fresh hash, not carry forward")
}

func TestMtimeScanner_Scan_UnmodifiedCarriesForward(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "unchanged.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	// Set mtime to past
	pastTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, pastTime, pastTime))

	// Threshold is after file mtime
	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeScanner(dir, threshold, map[string]domain.FileEntry{
		"unchanged.dat": {Hash: "carried_hash", Size: 7},
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.Equal(t, "carried_hash", result["unchanged.dat"].Hash, "should carry forward hash from previous")
}

func TestMtimeScanner_Scan_NewFileNotInPrevious(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "newfile.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("brand new"), 0o644))

	// Set mtime to past — file is "old" but not in previous map
	pastTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, pastTime, pastTime))

	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeScanner(dir, threshold, map[string]domain.FileEntry{})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.Contains(t, result, "newfile.dat")
	assert.NotEmpty(t, result["newfile.dat"].Hash, "should compute hash for new file regardless of mtime")
}

func TestMtimeScanner_Scan_DeletedFileOmitted(t *testing.T) {
	dir := t.TempDir()
	// previous map has file that no longer exists on disk

	scanner, err := NewMtimeScanner(dir, time.Now().Add(-time.Hour), map[string]domain.FileEntry{
		"deleted.dat": {Hash: "old_hash", Size: 8},
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.NotContains(t, result, "deleted.dat", "deleted file should be omitted")
	assert.Empty(t, result)
}

func TestMtimeScanner_Scan_EmptyDirNonEmptyPrevious(t *testing.T) {
	dir := t.TempDir()

	scanner, err := NewMtimeScanner(dir, time.Now().Add(-time.Hour), map[string]domain.FileEntry{
		"a.dat": {Hash: "h1", Size: 1},
		"b.dat": {Hash: "h2", Size: 2},
		"c.dat": {Hash: "h3", Size: 3},
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.Empty(t, result, "all previous files deleted — empty result")
}

func TestMtimeScanner_Scan_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), []byte("data"), 0o644))

	scanner, err := NewMtimeScanner(dir, time.Now().Add(-time.Hour), map[string]domain.FileEntry{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = scanner.Scan(ctx, []string{"**"})
	assert.Error(t, err)
}

func TestMtimeScanner_Scan_NestedForwardSlashes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "world", "region")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "r.0.0.mca"), []byte("data"), 0o644))

	scanner, err := NewMtimeScanner(dir, time.Now().Add(-time.Hour), map[string]domain.FileEntry{})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background(), []string{"**"})

	assert.NoError(t, err)
	assert.Contains(t, result, "world/region/r.0.0.mca")
}
