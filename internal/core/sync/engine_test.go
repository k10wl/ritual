package sync_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncpkg "ritual/internal/core/sync"
)

// drainTypes consumes events on ch into a list of type-name strings,
// keeping only those whose package portion equals pkgName.
func drainTypes(ch <-chan ports.Event, pkgName string) []string {
	var names []string
	for {
		select {
		case evt := <-ch:
			tn := reflect.TypeOf(evt).String()
			if pkgName == "" || hasPkgPrefix(tn, pkgName+".") {
				names = append(names, tn)
			}
		default:
			return names
		}
	}
}

func hasPkgPrefix(typeName, prefix string) bool {
	return len(typeName) >= len(prefix) && typeName[:len(prefix)] == prefix
}

func setupFS(t *testing.T, name string) (*adapters.FSRepository, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	repo, err := adapters.NewFSRepository(root, name+"::"+dir)
	require.NoError(t, err)
	return repo, dir
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, data, 0o644))
}

func readFile(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	return data
}

func scanFS(t *testing.T, root string) map[string]domain.FileEntry {
	t.Helper()
	scanner := adapters.NewFullScanner(os.DirFS(root))
	m, err := scanner.Scan(t.Context())
	require.NoError(t, err)
	return m
}

func TestSyncEngine_HappyPath_LocalToLocal(t *testing.T) {
	srcRepo, srcDir := setupFS(t, "fs-src")
	dstRepo, dstDir := setupFS(t, "fs-dst")

	writeFile(t, srcDir, "a.dat", []byte("alpha"))
	writeFile(t, srcDir, "nested/b.dat", []byte("beta"))

	bus := adapters.NewEventBus(64)
	ch, cancel := bus.Subscribe()
	defer cancel()

	src := observed.NewStorage(srcRepo, bus)
	dst := observed.NewStorage(dstRepo, bus)

	srcMap := scanFS(t, srcDir)
	dstMap := map[string]domain.FileEntry{}

	rs, err := syncpkg.Run(t.Context(), src, dst, srcMap, dstMap, syncpkg.DirectionUpload, bus)
	require.NoError(t, err)
	require.NoError(t, rs.Err)

	assert.Equal(t, []byte("alpha"), readFile(t, dstDir, "a.dat"))
	assert.Equal(t, []byte("beta"), readFile(t, dstDir, "nested/b.dat"))

	stagingPath := filepath.Join(dstDir, rs.StagingPath)
	_, statErr := os.Stat(stagingPath)
	assert.True(t, os.IsNotExist(statErr), "staging dir must be removed after success: stat err = %v", statErr)

	syncTypes := drainTypes(ch, "sync")
	assert.Contains(t, syncTypes, "sync.SyncStartedInfo")
	assert.Contains(t, syncTypes, "sync.SyncPlanInfo")
	assert.Contains(t, syncTypes, "sync.SyncStagingDirCreatedInfo")
	assert.Contains(t, syncTypes, "sync.SyncStageStartedInfo")
	assert.Contains(t, syncTypes, "sync.SyncStageFinishedInfo")
	assert.Contains(t, syncTypes, "sync.SyncCommitStartedInfo")
	assert.Contains(t, syncTypes, "sync.SyncCommitFinishedInfo")
	assert.Contains(t, syncTypes, "sync.SyncStagingDirCleanedInfo")
	assert.Contains(t, syncTypes, "sync.SyncFinishedInfo")
}

func TestSyncEngine_EmptyDiff_ShortCircuits(t *testing.T) {
	srcRepo, _ := setupFS(t, "fs-src")
	dstRepo, _ := setupFS(t, "fs-dst")

	bus := adapters.NewEventBus(64)
	ch, cancel := bus.Subscribe()
	defer cancel()

	src := observed.NewStorage(srcRepo, bus)
	dst := observed.NewStorage(dstRepo, bus)

	rs, err := syncpkg.Run(t.Context(), src, dst,
		map[string]domain.FileEntry{}, map[string]domain.FileEntry{},
		syncpkg.DirectionUpload, bus)
	require.NoError(t, err)
	assert.Empty(t, rs.StagingID, "no staging dir should be created on empty diff")

	syncTypes := drainTypes(ch, "sync")
	assert.Contains(t, syncTypes, "sync.SyncStartedInfo")
	assert.Contains(t, syncTypes, "sync.SyncPlanInfo")
	assert.NotContains(t, syncTypes, "sync.SyncStagingDirCreatedInfo")
	assert.Contains(t, syncTypes, "sync.SyncFinishedInfo")
}

func TestSyncEngine_StagingFailure_CleansUp(t *testing.T) {
	srcRepo, srcDir := setupFS(t, "fs-src")
	writeFile(t, srcDir, "a.dat", []byte("alpha"))

	dstRepo, _ := setupFS(t, "fs-dst")

	bus := adapters.NewEventBus(64)
	ch, cancel := bus.Subscribe()
	defer cancel()

	src := observed.NewStorage(srcRepo, bus)
	dst := &failingPutStorage{inner: dstRepo, failOn: "a.dat"}
	dstObserved := observed.NewStorage(dst, bus)

	srcMap := scanFS(t, srcDir)

	_, err := syncpkg.Run(t.Context(), src, dstObserved,
		srcMap, map[string]domain.FileEntry{},
		syncpkg.DirectionUpload, bus)
	require.Error(t, err, "expected stage failure to surface")
	assert.Contains(t, err.Error(), "put a.dat")

	syncTypes := drainTypes(ch, "sync")
	assert.Contains(t, syncTypes, "sync.SyncStageFailedInfo")
	assert.Contains(t, syncTypes, "sync.SyncFailedInfo")
	assert.Contains(t, syncTypes, "sync.SyncStagingDirCleanedInfo")
	assert.NotContains(t, syncTypes, "sync.SyncFinishedInfo")
}

// failingPutStorage wraps an adapter and forces Put to fail when key
// contains the configured failOn substring.
type failingPutStorage struct {
	inner  ports.StorageRepository
	failOn string
}

func (f *failingPutStorage) String() string { return "failing::" + f.failOn }

func (f *failingPutStorage) Get(ctx context.Context, key string) ([]byte, error) {
	return f.inner.Get(ctx, key)
}

func (f *failingPutStorage) Put(ctx context.Context, key string, data []byte) error {
	if f.failOn != "" && contains(key, f.failOn) {
		return errors.New("forced put failure")
	}
	return f.inner.Put(ctx, key, data)
}

func (f *failingPutStorage) Copy(ctx context.Context, src, dst string) error {
	return f.inner.Copy(ctx, src, dst)
}

func (f *failingPutStorage) Rename(ctx context.Context, src, dst string) error {
	return f.inner.Rename(ctx, src, dst)
}

func (f *failingPutStorage) Delete(ctx context.Context, key string) error {
	return f.inner.Delete(ctx, key)
}

func (f *failingPutStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return f.inner.DeleteBatch(ctx, keys)
}

func (f *failingPutStorage) List(ctx context.Context, prefix string) ([]string, error) {
	return f.inner.List(ctx, prefix)
}

func (f *failingPutStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	return f.inner.GetStream(ctx, key)
}

func (f *failingPutStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	if f.failOn != "" && contains(key, f.failOn) {
		return errors.New("forced put failure")
	}
	return f.inner.PutStream(ctx, key, body)
}

func (f *failingPutStorage) Exists(ctx context.Context, key string) (bool, error) {
	return f.inner.Exists(ctx, key)
}

func contains(s, substr string) bool {
	if substr == "" {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
