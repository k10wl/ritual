// Package refs_test — Puller story:
//
// Pull is ACID per §Pull — ACID in docs/superpowers/specs/2026-04-19-fast-
// sync-v2.1-design.md. It streams refs/{id}.json from remote to local,
// validates the written JSON (deleting the local copy on failure so a
// retry refetches), then streams every referenced objects/{hash} into
// local, skipping blobs already present. Each test below exercises one
// ACID invariant from that section.
//
// Not in scope for Puller (delegated elsewhere):
//   - Blob integrity (xxhash verify on decompress) — CompressingStorage
//     decorator; see `internal/adapters/compressing_test.go`.
//   - Session lock / isolation — orchestrator concern.
//   - FlushFileBuffers durability — FSRepository concern.
//
// Rules for writing tests in this file (per ritual_integration_test.go):
//
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
//   - Custom fakes with story-friendly names; never generic mocks with
//     opaque Func fields.
package refs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/refs"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPuller_PullsRefAndEveryReferencedBlobFromRemoteIntoLocal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})

	puller := refs.NewPuller(remote, local)
	err := puller.Pull(ctx, ref.Timestamp)

	require.NoError(t, err,
		"pull with complete remote data and empty local must succeed — ACID consistency postcondition")

	pulled, ok := local.decodeRef(t, ref.Timestamp)
	require.True(t, ok,
		"pull step 1 must write refs/{id}.json into local after streaming it from remote")
	assert.Equal(t, ref.Objects, pulled.Objects,
		"local ref's object map must equal remote — pull cannot invent or drop entries")

	assert.Equal(t, []byte("AAAA"), local.mustGet(t, "objects/"+hashHex("AAAA")),
		"pull step 3 barrier: every referenced hash must be present in local after success")
	assert.Equal(t, []byte("BBBBBBBB"), local.mustGet(t, "objects/"+hashHex("BBBBBBBB")),
		"pull step 3 barrier: every referenced hash must be present in local after success")
}

func TestPuller_SkipsBlobsAlreadyPresentLocally(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	local.put("objects/"+hashHex("AAAA"), []byte("AAAA"))

	puller := refs.NewPuller(remote, local)
	err := puller.Pull(ctx, ref.Timestamp)

	require.NoError(t, err,
		"pull with one blob already present locally must still succeed — idempotent per-blob")
	assert.Equal(t, 0, remote.getHits("objects/"+hashHex("AAAA")),
		"pull step 2 Exists-gate: blob already present locally must not be re-fetched from remote")
	assert.Equal(t, 1, remote.getHits("objects/"+hashHex("BBBBBBBB")),
		"pull step 2: missing blob must be fetched exactly once from remote")
	assert.Equal(t, []byte("BBBBBBBB"), local.mustGet(t, "objects/"+hashHex("BBBBBBBB")),
		"pull step 3 barrier: previously-missing blob must land in local after success")
}

func TestPuller_ReturnsErrorWhenRemoteRefMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	puller := refs.NewPuller(remote, local)
	err := puller.Pull(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"pull of a ref id that does not exist on remote must surface an error — silent success masks data loss")
	assert.Empty(t, local.keys(),
		"failed pull with no remote ref must leave local untouched — ref fetch is the first mutation")
}

func TestPuller_DeletesLocalRefWhenRemoteJSONInvalid(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	refID := domain.RefID("2026-04-22T10-00-00.000Z")
	remote.put("refs/"+string(refID)+".json", []byte("}{ not json"))

	puller := refs.NewPuller(remote, local)
	err := puller.Pull(ctx, refID)

	require.Error(t, err,
		"pull step 1 validate-fail path: invalid JSON at remote must produce a pull error")
	assert.Empty(t, local.keys(),
		"pull step 1 recovery: local ref must be deleted after validate failure so the next retry refetches from scratch")
}

func TestPuller_SurfacesBlobFetchError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	remote.put("refs/"+string(ref.Timestamp)+".json", body)

	puller := refs.NewPuller(remote, local)
	err = puller.Pull(ctx, ref.Timestamp)

	require.Error(t, err,
		"pull step 2: missing referenced blob on remote must surface an error — partial success violates step 3 barrier")
}

func TestPuller_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, remote, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	puller := refs.NewPuller(remote, local)
	require.NoError(t, puller.Pull(ctx, ref.Timestamp),
		"first pull must succeed on complete remote state")

	hitsBeforeRerun := remote.getHits("objects/"+hashHex("AAAA"))
	require.NoError(t, puller.Pull(ctx, ref.Timestamp),
		"second pull on fully-local state must succeed — Pull is idempotent across replays")

	assert.Equal(t, hitsBeforeRerun, remote.getHits("objects/"+hashHex("AAAA")),
		"atomicity via idempotent stage replay: a second pull must not re-fetch blobs already present locally")
}

// --- test fixtures ---

// sampleRef builds a Ref with real xxhash64 hex Hash values derived from
// each file's raw content. Target glob defaults to "worlds/**" — callers
// needing other globs construct their Ref inline.
func sampleRef(id domain.RefID, files map[string][]byte) *domain.Ref {
	objects := make(map[string]domain.Object, len(files))
	for path, data := range files {
		objects[path] = domain.Object{Hash: hashHexBytes(data), Size: int64(len(data))}
	}
	return &domain.Ref{
		Timestamp:     id,
		RitualVersion: "2.1.0",
		Targets:       []string{"worlds/**"},
		Objects:       objects,
	}
}

// seedRemote seeds a store with a ref JSON plus one blob per file. The
// `files` map mirrors sampleRef's: path → raw content. Blob keys derive
// from the ref's Object.Hash so contents and keys stay consistent.
func seedRemote(t *testing.T, remote *memStorage, ref *domain.Ref, files map[string][]byte) {
	t.Helper()
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	remote.put("refs/"+string(ref.Timestamp)+".json", body)
	for path, data := range files {
		obj, ok := ref.Objects[path]
		require.True(t, ok,
			"test fixture: seedRemote file %q not found in ref.Objects — sampleRef and seedRemote must be called with the same files map", path)
		remote.put("objects/"+obj.Hash, data)
	}
}

func hashHexBytes(data []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

func hashHex(s string) string { return hashHexBytes([]byte(s)) }

// --- memStorage — in-memory ports.StorageRepository for story tests ---

var errMemKeyNotFound = errors.New("memStorage: key not found")

type memStorage struct {
	mu    sync.Mutex
	items map[string][]byte
	hits  map[string]int
	puts  map[string]int
}

func newMemStorage() *memStorage {
	return &memStorage{items: map[string][]byte{}, hits: map[string]int{}, puts: map[string]int{}}
}

func (m *memStorage) String() string { return "mem::storage" }

func (m *memStorage) put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = append([]byte(nil), data...)
}

func (m *memStorage) mustGet(t *testing.T, key string) []byte {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.items[key]
	require.True(t, ok, "memStorage must contain key %q — fixture or code under test failed to populate it", key)
	return append([]byte(nil), data...)
}

func (m *memStorage) getHits(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[key]
}

func (m *memStorage) putHits(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[key]
}

func (m *memStorage) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.items))
	for k := range m.items {
		out = append(out, k)
	}
	return out
}

func keysWithPrefix(m *memStorage, prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for k := range m.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k)
		}
	}
	return out
}

func (m *memStorage) decodeRef(t *testing.T, id domain.RefID) (*domain.Ref, bool) {
	t.Helper()
	m.mu.Lock()
	data, ok := m.items["refs/"+string(id)+".json"]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	ref := &domain.Ref{}
	require.NoError(t, json.Unmarshal(data, ref),
		"memStorage refs/%s.json must decode as domain.Ref — invalid JSON indicates pull wrote garbage", id)
	return ref, true
}

func (m *memStorage) GetStream(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	data, ok := m.items[key]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", errMemKeyNotFound, key)
	}
	m.hits[key]++
	buf := append([]byte(nil), data...)
	m.mu.Unlock()
	return io.NopCloser(bytes.NewReader(buf)), nil
}

func (m *memStorage) PutStream(_ context.Context, key string, body io.ReadSeeker) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.put(key, data)
	m.mu.Lock()
	m.puts[key]++
	m.mu.Unlock()
	return nil
}

func (m *memStorage) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.items[key]
	return ok, nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}

func (m *memStorage) DeleteBatch(ctx context.Context, keys []string) error {
	for _, k := range keys {
		_ = m.Delete(ctx, k)
	}
	return nil
}

func (m *memStorage) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for k := range m.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memStorage) Copy(ctx context.Context, src, dst string) error {
	rc, err := m.GetStream(ctx, src)
	if err != nil {
		return err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	m.put(dst, data)
	return nil
}

func (m *memStorage) Get(_ context.Context, _ string) ([]byte, error) {
	panic("memStorage: V1 Get is not implemented — use GetStream")
}

func (m *memStorage) Put(_ context.Context, _ string, _ []byte) error {
	panic("memStorage: V1 Put is not implemented — use PutStream")
}

func (m *memStorage) Rename(_ context.Context, _, _ string) error {
	panic("memStorage: V1 Rename is not implemented")
}
