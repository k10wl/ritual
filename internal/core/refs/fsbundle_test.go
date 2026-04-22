package refs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/stretchr/testify/require"
)

// fsBundle wraps a t.TempDir()-backed FSRepository with per-key call
// observability so tests can assert skip-gate correctness (e.g. "blob
// already present must not be re-fetched"). Three views are exposed:
// storage (the wrapped ports.StorageRepository), scanner() (a FullScanner
// over the same root), and counters. `inner` is the raw FSRepository
// used by fixture seeders so pre-seeding does not bump the counters.
type fsBundle struct {
	storage ports.StorageRepository
	inner   ports.StorageRepository
	root    string
	fsys    fs.FS
	counter *keyCounter
}

func newFSBundle(t *testing.T) *fsBundle {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err,
		"fsBundle fixture: os.OpenRoot over t.TempDir must succeed — setup failure before any test logic runs")
	t.Cleanup(func() { _ = root.Close() })
	repo, err := adapters.NewFSRepository(root)
	require.NoError(t, err,
		"fsBundle fixture: adapters.NewFSRepository must accept the opened root — wiring failure hides test intent")
	counter := newKeyCounter(repo)
	return &fsBundle{
		storage: counter,
		inner:   repo,
		root:    dir,
		fsys:    os.DirFS(dir),
		counter: counter,
	}
}

func (b *fsBundle) scanner() ports.DirectoryScanner {
	return adapters.NewFullScanner(b.fsys)
}

// put seeds the underlying filesystem without bumping the keyCounter. This
// matches the old memStorage's lowercase `put(key, data)` helper, which
// fixtures used for pre-population; PutStream (the V2 write path) still
// counts so tests can assert "code under test uploaded once / not at all".
func (b *fsBundle) put(t *testing.T, key string, data []byte) {
	t.Helper()
	err := b.inner.PutStream(context.Background(), key, bytes.NewReader(data))
	require.NoError(t, err,
		"fsBundle fixture: PutStream(%q) must succeed — test setup precondition", key)
}

// mustGet reads the raw bytes at key from the underlying filesystem without
// bumping the keyCounter — assertions about final state must not poison the
// "code under test fetched exactly once" counts.
func (b *fsBundle) mustGet(t *testing.T, key string) []byte {
	t.Helper()
	rc, err := b.inner.GetStream(context.Background(), key)
	require.NoError(t, err,
		"fsBundle must contain key %q — fixture or code under test failed to populate it", key)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err,
		"fsBundle fixture: GetStream(%q) body must drain cleanly", key)
	return data
}

// keys walks the filesystem root and returns every regular file's relative
// path in POSIX form (forward slashes), mirroring the flat key enumeration
// the former memStorage offered. Directories are omitted.
func (b *fsBundle) keys() []string {
	out := []string{}
	_ = filepath.WalkDir(b.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(b.root, p)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

func (b *fsBundle) decodeRef(t *testing.T, id domain.RefID) (*domain.Ref, bool) {
	t.Helper()
	rc, err := b.inner.GetStream(context.Background(), "refs/"+string(id)+".json")
	if err != nil {
		return nil, false
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	require.NoError(t, err,
		"fsBundle decodeRef: reading refs/%s.json body must not fail once the key exists", id)
	ref := &domain.Ref{}
	require.NoError(t, json.Unmarshal(raw, ref),
		"fsBundle refs/%s.json must decode as domain.Ref — invalid JSON indicates code under test wrote garbage", id)
	return ref, true
}

func (b *fsBundle) getHits(key string) int { return b.counter.getHits(key) }
func (b *fsBundle) putHits(key string) int { return b.counter.putHits(key) }

// keysWithPrefix enumerates every fsBundle key whose POSIX form starts with
// the given prefix — replaces the former memStorage helper used by commit
// crash-recovery tests to assert "no refs/ entries".
func keysWithPrefix(b *fsBundle, prefix string) []string {
	all := b.keys()
	out := []string{}
	for _, k := range all {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// keyCounter decorates a ports.StorageRepository with per-key get/put
// counters so tests can assert skip-gate correctness ("blob already
// present must not be re-fetched"). GetStream increments on success
// before returning the body; PutStream increments on success after the
// inner write.
type keyCounter struct {
	inner ports.StorageRepository
	mu    sync.Mutex
	gets  map[string]int
	puts  map[string]int
}

func newKeyCounter(inner ports.StorageRepository) *keyCounter {
	return &keyCounter{inner: inner, gets: map[string]int{}, puts: map[string]int{}}
}

func (c *keyCounter) getHits(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets[key]
}

func (c *keyCounter) putHits(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.puts[key]
}

func (c *keyCounter) String() string { return "keyCounter::" + c.inner.String() }

func (c *keyCounter) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := c.inner.GetStream(ctx, key)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.gets[key]++
	c.mu.Unlock()
	return rc, nil
}

func (c *keyCounter) PutStream(ctx context.Context, key string, body io.ReadSeeker) error {
	err := c.inner.PutStream(ctx, key, body)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.puts[key]++
	c.mu.Unlock()
	return nil
}

func (c *keyCounter) Exists(ctx context.Context, key string) (bool, error) {
	return c.inner.Exists(ctx, key)
}

func (c *keyCounter) Delete(ctx context.Context, key string) error {
	return c.inner.Delete(ctx, key)
}

func (c *keyCounter) DeleteBatch(ctx context.Context, keys []string) error {
	return c.inner.DeleteBatch(ctx, keys)
}

func (c *keyCounter) List(ctx context.Context, prefix string) ([]string, error) {
	return c.inner.List(ctx, prefix)
}

func (c *keyCounter) Copy(ctx context.Context, src, dst string) error {
	return c.inner.Copy(ctx, src, dst)
}

func (c *keyCounter) Get(ctx context.Context, key string) ([]byte, error) {
	return c.inner.Get(ctx, key)
}

func (c *keyCounter) Put(ctx context.Context, key string, data []byte) error {
	return c.inner.Put(ctx, key, data)
}

func (c *keyCounter) Rename(ctx context.Context, src, dst string) error {
	return c.inner.Rename(ctx, src, dst)
}
