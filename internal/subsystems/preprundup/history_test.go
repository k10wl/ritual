package preprundup_test

import (
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/subsystems/preprundup"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTempRoot(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	original := config.RootPath
	config.RootPath = tempDir
	t.Cleanup(func() { config.RootPath = original })
	return tempDir
}

func TestStoreLoad_MissingFileReturnsEmptyHistory(t *testing.T) {
	withTempRoot(t)
	store := preprundup.NewStore()

	f, err := store.Load()
	require.NoError(t, err, "a missing prep-history.json is first-run, not an error")
	assert.Nil(t, f.Last, "no sample on a fresh machine")
}

func TestStoreAppend_PersistsAndReloads(t *testing.T) {
	withTempRoot(t)
	store := preprundup.NewStore()

	require.NoError(t, store.Append(preprundup.Sample{RunID: "r1", PrepMs: 14200, WrapMs: 28800}))

	f, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, f.Last)
	assert.Equal(t, "r1", f.Last.RunID)
	assert.Equal(t, int64(14200), f.Last.PrepMs)
	assert.Equal(t, int64(28800), f.Last.WrapMs)
}

func TestStoreAppend_OverwritesPreviousSample(t *testing.T) {
	// design-log/058 "just store last one": only the most recent run's
	// timing survives, not a history of every run.
	withTempRoot(t)
	store := preprundup.NewStore()

	require.NoError(t, store.Append(preprundup.Sample{RunID: "r1", PrepMs: 1000, WrapMs: 2000}))
	require.NoError(t, store.Append(preprundup.Sample{RunID: "r2", PrepMs: 3000, WrapMs: 4000}))

	f, err := store.Load()
	require.NoError(t, err)
	require.NotNil(t, f.Last)
	assert.Equal(t, "r2", f.Last.RunID, "the second Append must replace the first, not accumulate")
	assert.Equal(t, int64(3000), f.Last.PrepMs)
}

func TestStoreAppend_WritesPrettyPrintedJSON(t *testing.T) {
	tempDir := withTempRoot(t)
	store := preprundup.NewStore()

	require.NoError(t, store.Append(preprundup.Sample{RunID: "r1", PrepMs: 1000, WrapMs: 2000}))

	data, err := os.ReadFile(filepath.Join(tempDir, preprundup.HistoryFilename))
	require.NoError(t, err)
	assert.Contains(t, string(data), "\n  ", "MarshalIndent output must be pretty-printed, not a single-line blob")
}

func TestStoreAppend_LeavesNoTempFileBehind(t *testing.T) {
	tempDir := withTempRoot(t)
	store := preprundup.NewStore()

	require.NoError(t, store.Append(preprundup.Sample{RunID: "r1", PrepMs: 1000, WrapMs: 2000}))

	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".prep-history-", "atomic write must clean up its temp file on success")
	}
}
