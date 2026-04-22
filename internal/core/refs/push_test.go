// Package refs_test — Pusher story:
//
// Push is ACID per §Push — ACID in docs/superpowers/specs/2026-04-19-fast-
// sync-v2.1-design.md. It loads a local ref, uploads every referenced blob
// to the destination (skipping blobs already present via an Exists gate),
// then uploads the ref as the single commit point. Each test below
// exercises one ACID invariant or crash-recovery row from that section.
//
// MVP deferrals (not yet tested; require the session-lock port):
//   - Step 4 pre-PUT fence verify (lock sessionId check).
//   - Step 5 `If-None-Match: *` conditional PUT on first-ever push.
//   - Step 6 post-PUT fence verify + zombie self-DELETE rollback.
//
// Not in scope for Pusher (delegated elsewhere):
//   - Blob compression + hash verification — CompressingStorage decorator.
//   - FlushFileBuffers durability — FSRepository concern.
//   - Session lock — orchestrator concern once the lock port lands.
package refs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/refs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPusher_UploadsRefAndEveryReferencedBlobToRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})

	pusher := refs.NewPusher(local, remote)
	err := pusher.Push(ctx, ref.Timestamp)

	require.NoError(t, err,
		"push with complete local state and empty remote must succeed — §Push consistency postcondition")

	pushed, ok := remote.decodeRef(t, ref.Timestamp)
	require.True(t, ok,
		"§Push step 5 commit point: refs/{id}.json must land on remote after success")
	assert.Equal(t, ref.Objects, pushed.Objects,
		"remote ref's object map must equal local — push cannot invent or drop entries")

	assert.Equal(t, []byte("AAAA"), remote.mustGet(t, "objects/"+hashHex("AAAA")),
		"§Push consistency postcondition: every referenced blob must exist on remote after success")
	assert.Equal(t, []byte("BBBBBBBB"), remote.mustGet(t, "objects/"+hashHex("BBBBBBBB")),
		"§Push consistency postcondition: every referenced blob must exist on remote after success")
}

func TestPusher_SkipsBlobsAlreadyOnRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	remote.put("objects/"+hashHex("AAAA"), []byte("AAAA"))

	pusher := refs.NewPusher(local, remote)
	err := pusher.Push(ctx, ref.Timestamp)

	require.NoError(t, err,
		"push with one blob already on remote must still succeed — idempotent per-blob")
	assert.Equal(t, 0, remote.putHits("objects/"+hashHex("AAAA")),
		"§Push step 2 Exists-gate: blob already on remote must not be re-uploaded")
	assert.Equal(t, 1, remote.putHits("objects/"+hashHex("BBBBBBBB")),
		"§Push step 2: missing blob must be uploaded exactly once")
}

func TestPusher_ReturnsErrorWhenLocalRefMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	pusher := refs.NewPusher(local, remote)
	err := pusher.Push(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"§Push step 1: missing local ref must surface an error — Pusher cannot push what it cannot load")
	assert.Empty(t, remote.keys(),
		"failed push with no local ref must leave remote untouched — ref load is the first action")
}

func TestPusher_ReturnsErrorWhenLocalRefInvalidJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	id := domain.RefID("2026-04-22T10-00-00.000Z")
	local.put("refs/"+string(id)+".json", []byte("}{ not json"))

	pusher := refs.NewPusher(local, remote)
	err := pusher.Push(ctx, id)

	require.Error(t, err,
		"§Push step 1 parse: an unparseable local ref must produce an error rather than an arbitrary remote write")
	assert.Empty(t, remote.keys(),
		"failed parse of local ref must leave remote untouched — nothing meaningful to upload from a broken ref")
}

func TestPusher_DoesNotWriteRefWhenLocalBlobMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	local.put("refs/"+string(ref.Timestamp)+".json", body)

	pusher := refs.NewPusher(local, remote)
	err = pusher.Push(ctx, ref.Timestamp)

	require.Error(t, err,
		"§Push step 2: a referenced blob absent locally must surface an error — Pusher cannot upload what it cannot read")
	assert.Empty(t, remote.decodeRefKeys(),
		"§Push step 3 barrier: ref must NOT reach remote if any referenced blob failed to upload — ordering invariant")
	assertNoRefOnRemote(t, remote, ref.Timestamp)
}

func TestPusher_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	pusher := refs.NewPusher(local, remote)
	require.NoError(t, pusher.Push(ctx, ref.Timestamp),
		"first push on empty remote must succeed")

	blobPutsAfterFirst := remote.putHits("objects/"+hashHex("AAAA"))
	require.NoError(t, pusher.Push(ctx, ref.Timestamp),
		"second push on fully-populated remote must succeed — §Push atomicity: idempotent stage replay")

	assert.Equal(t, blobPutsAfterFirst, remote.putHits("objects/"+hashHex("AAAA")),
		"§Push crash-recovery row 'mid blob upload': re-running push must skip blobs already durable on remote")
}

func TestPusher_ResumesAfterBlobsUploadedButRefMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newMemStorage()
	remote := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	remote.put("objects/"+hashHex("AAAA"), []byte("AAAA"))

	pusher := refs.NewPusher(local, remote)
	err := pusher.Push(ctx, ref.Timestamp)

	require.NoError(t, err,
		"§Push crash-recovery row 'all blobs uploaded, before manifest PUT': retry must succeed on a ref PUT alone")
	pushed, ok := remote.decodeRef(t, ref.Timestamp)
	require.True(t, ok,
		"resumed push must land the missing ref JSON on remote — step 5 commit point")
	assert.Equal(t, ref.Objects, pushed.Objects,
		"resumed push's ref must carry the original object map — byte-identical retry")
	assert.Equal(t, 0, remote.putHits("objects/"+hashHex("AAAA")),
		"resumed push must not re-upload blobs already durable on remote")
}

// --- push fixtures (prefix-named to avoid collision with Pull/Commit/Apply helpers) ---

func seedLocalForPush(t *testing.T, local *memStorage, ref *domain.Ref, files map[string][]byte) {
	t.Helper()
	seedRemote(t, local, ref, files)
}

func (m *memStorage) decodeRefKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for k := range m.items {
		if len(k) >= len("refs/") && k[:len("refs/")] == "refs/" {
			out = append(out, k)
		}
	}
	return out
}

func assertNoRefOnRemote(t *testing.T, remote *memStorage, id domain.RefID) {
	t.Helper()
	remote.mu.Lock()
	defer remote.mu.Unlock()
	_, exists := remote.items["refs/"+string(id)+".json"]
	assert.False(t, exists,
		"remote must not contain refs/%s.json after a failed push — §Push step 3 barrier: ref PUT is gated on blob barrier success", id)
}
