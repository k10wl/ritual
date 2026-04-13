package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMtimeWorldScanner_NewMtimeWorldScanner_EmptyRoot(t *testing.T) {
	_, err := NewMtimeWorldScanner("", time.Now(), map[string]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "root directory cannot be empty")
}

func TestMtimeWorldScanner_NewMtimeWorldScanner_NilPreviousTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), []byte("data"), 0644))

	scanner, err := NewMtimeWorldScanner(dir, time.Now().Add(-time.Hour), nil)
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, result, "file.dat", "nil previous should hash all files like empty map")
}

func TestMtimeWorldScanner_Scan_ModifiedAfterThreshold(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "modified.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("new content"), 0644))

	// Set threshold to past — file is "modified after"
	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeWorldScanner(dir, threshold, map[string]string{
		"modified.dat": "old_hash",
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, result, "modified.dat")
	assert.NotEqual(t, "old_hash", result["modified.dat"], "should compute fresh hash, not carry forward")
}

func TestMtimeWorldScanner_Scan_UnmodifiedCarriesForward(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "unchanged.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))

	// Set mtime to past
	pastTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, pastTime, pastTime))

	// Threshold is after file mtime
	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeWorldScanner(dir, threshold, map[string]string{
		"unchanged.dat": "carried_hash",
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "carried_hash", result["unchanged.dat"], "should carry forward from previous")
}

func TestMtimeWorldScanner_Scan_NewFileNotInPrevious(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "newfile.dat")
	require.NoError(t, os.WriteFile(filePath, []byte("brand new"), 0644))

	// Set mtime to past — file is "old" but not in previous map
	pastTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, pastTime, pastTime))

	threshold := time.Now().Add(-time.Hour)

	scanner, err := NewMtimeWorldScanner(dir, threshold, map[string]string{})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, result, "newfile.dat")
	assert.NotEmpty(t, result["newfile.dat"], "should compute hash for new file regardless of mtime")
}

func TestMtimeWorldScanner_Scan_DeletedFileOmitted(t *testing.T) {
	dir := t.TempDir()
	// previous map has file that no longer exists on disk

	scanner, err := NewMtimeWorldScanner(dir, time.Now().Add(-time.Hour), map[string]string{
		"deleted.dat": "old_hash",
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.NotContains(t, result, "deleted.dat", "deleted file should be omitted")
	assert.Empty(t, result)
}

func TestMtimeWorldScanner_Scan_EmptyDirNonEmptyPrevious(t *testing.T) {
	dir := t.TempDir()

	scanner, err := NewMtimeWorldScanner(dir, time.Now().Add(-time.Hour), map[string]string{
		"a.dat": "h1",
		"b.dat": "h2",
		"c.dat": "h3",
	})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, result, "all previous files deleted — empty result")
}

func TestMtimeWorldScanner_Scan_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.dat"), []byte("data"), 0644))

	scanner, err := NewMtimeWorldScanner(dir, time.Now().Add(-time.Hour), map[string]string{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = scanner.Scan(ctx)
	assert.Error(t, err)
}

func TestMtimeWorldScanner_Scan_NestedForwardSlashes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "world", "region")
	require.NoError(t, os.MkdirAll(nested, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "r.0.0.mca"), []byte("data"), 0644))

	scanner, err := NewMtimeWorldScanner(dir, time.Now().Add(-time.Hour), map[string]string{})
	require.NoError(t, err)

	result, err := scanner.Scan(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, result, "world/region/r.0.0.mca")
}
