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
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
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

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	local.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
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

	remote := newFSBundle(t)
	local := newFSBundle(t)

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err := puller.Pull(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"pull of a ref id that does not exist on remote must surface an error — silent success masks data loss")
	assert.Empty(t, local.keys(),
		"failed pull with no remote ref must leave local untouched — ref fetch is the first mutation")
}

func TestPuller_DeletesLocalRefWhenRemoteJSONInvalid(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	refID := domain.RefID("2026-04-22T10-00-00.000Z")
	remote.put(t, "refs/"+string(refID)+".json", []byte("}{ not json"))

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err := puller.Pull(ctx, refID)

	require.Error(t, err,
		"pull step 1 validate-fail path: invalid JSON at remote must produce a pull error")
	assert.Empty(t, local.keys(),
		"pull step 1 recovery: local ref must be deleted after validate failure so the next retry refetches from scratch")
}

func TestPuller_SurfacesBlobFetchError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	remote.put(t, "refs/"+string(ref.Timestamp)+".json", body)

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err = puller.Pull(ctx, ref.Timestamp)

	require.Error(t, err,
		"pull step 2: missing referenced blob on remote must surface an error — partial success violates step 3 barrier")
}

func TestPuller_DoesNotCommitLocalRefUntilEveryBlobLanded(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("CONTENT"),
	})
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	remote.put(t, "refs/"+string(ref.Timestamp)+".json", body)

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err = puller.Pull(ctx, ref.Timestamp)

	require.Error(t, err,
		"pull with missing referenced blob on remote must surface an error — step 3 barrier is violated")
	present, existsErr := local.storage.Exists(ctx, "refs/"+string(ref.Timestamp)+".json")
	require.NoError(t, existsErr, "post-failure sanity: Exists must answer cleanly")
	assert.False(t, present,
		"pull commit barrier: refs/{id}.json must NOT exist locally after a failed pull — else retention treats the ref as live and apply tries to materialize missing blobs")
}

func TestPuller_OnlyFetchesBlobsForFilesThatChangedAcrossRefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	version1 := map[string][]byte{
		"worlds/level.dat":  []byte("LEVEL_V1"),
		"worlds/region.mca": []byte("REGION_STABLE"),
	}
	ref1 := sampleRef("2026-04-22T09-00-00.000Z", version1)
	seedRemote(t, remote, ref1, version1)

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	require.NoError(t, puller.Pull(ctx, ref1.Timestamp),
		"first pull must hydrate local with every blob referenced by ref1")

	version2 := map[string][]byte{
		"worlds/level.dat":  []byte("LEVEL_V2"),
		"worlds/region.mca": []byte("REGION_STABLE"),
	}
	ref2 := sampleRef("2026-04-22T10-00-00.000Z", version2)
	seedRemote(t, remote, ref2, version2)

	hitsBeforeSecondPull := map[string]int{
		"objects/" + hashHex("LEVEL_V1"):      remote.getHits("objects/" + hashHex("LEVEL_V1")),
		"objects/" + hashHex("LEVEL_V2"):      remote.getHits("objects/" + hashHex("LEVEL_V2")),
		"objects/" + hashHex("REGION_STABLE"): remote.getHits("objects/" + hashHex("REGION_STABLE")),
	}

	require.NoError(t, puller.Pull(ctx, ref2.Timestamp),
		"second pull (with only level.dat changed between refs) must succeed")

	assert.Equal(t,
		hitsBeforeSecondPull["objects/"+hashHex("LEVEL_V2")]+1,
		remote.getHits("objects/"+hashHex("LEVEL_V2")),
		"content-addressed incremental pull: the blob for the file that CHANGED must be fetched exactly once — new hash, new key, not present locally")
	assert.Equal(t,
		hitsBeforeSecondPull["objects/"+hashHex("REGION_STABLE")],
		remote.getHits("objects/"+hashHex("REGION_STABLE")),
		"content-addressed incremental pull: the blob for the file whose content did NOT change must not be re-fetched — same content → same hash → local Exists-gate skips remote GET")
}

func TestPuller_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, remote, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	require.NoError(t, puller.Pull(ctx, ref.Timestamp),
		"first pull must succeed on complete remote state")

	hitsBeforeRerun := remote.getHits("objects/"+hashHex("AAAA"))
	require.NoError(t, puller.Pull(ctx, ref.Timestamp),
		"second pull on fully-local state must succeed — Pull is idempotent across replays")

	assert.Equal(t, hitsBeforeRerun, remote.getHits("objects/"+hashHex("AAAA")),
		"atomicity via idempotent stage replay: a second pull must not re-fetch blobs already present locally")
}

func TestPuller_SurfacesDownloadSentinelAndScrubsPartialOnWriteFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("CONTENT"),
	})
	seedRemote(t, remote, ref, map[string][]byte{"worlds/level.dat": []byte("CONTENT")})

	faulty := newFaultyStorage(local)
	uplinkBroken := errors.New("simulated uplink failure on put")
	faulty.putFail["objects/"+hashHex("CONTENT")] = uplinkBroken

	puller := refs.NewPuller(remote.storage, faulty, serialRunner)
	err := puller.Pull(ctx, ref.Timestamp)

	require.Error(t, err,
		"download failure MUST surface — silent success would let the Pull ACID barrier lie about blob presence")
	assert.ErrorIs(t, err, refs.ErrBlobDownload,
		"error chain must classify via ErrBlobDownload so callers filter on the failure category")
	assert.ErrorIs(t, err, uplinkBroken,
		"error chain must wrap the original PutStream cause; callers that want the concrete failure reach it via errors.Is/As")
	present, existsErr := local.storage.Exists(ctx, "objects/"+hashHex("CONTENT"))
	require.NoError(t, existsErr, "post-failure sanity: Exists must answer cleanly")
	assert.False(t, present,
		"post-failure invariant: no partial bytes under the blob key — the scrub-on-failure path must leave destination clean so the next Pull sees Exists == false and starts fresh")
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

// seedRemote seeds an fsBundle with a ref JSON plus one blob per file. The
// `files` map mirrors sampleRef's: path → raw content. Blob keys derive
// from the ref's Object.Hash so contents and keys stay consistent.
func seedRemote(t *testing.T, remote *fsBundle, ref *domain.Ref, files map[string][]byte) {
	t.Helper()
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	remote.put(t, "refs/"+string(ref.Timestamp)+".json", body)
	for path, data := range files {
		obj, ok := ref.Objects[path]
		require.True(t, ok,
			"test fixture: seedRemote file %q not found in ref.Objects — sampleRef and seedRemote must be called with the same files map", path)
		remote.put(t, "objects/"+obj.Hash, data)
	}
}

func hashHexBytes(data []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

func hashHex(s string) string { return hashHexBytes([]byte(s)) }
