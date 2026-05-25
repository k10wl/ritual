// Package refs_test — Pusher story:
//
// Push is ACID per §Push — ACID in docs/superpowers/specs/2026-04-19-fast-
// sync-v2.1-design.md. It loads a ref from `from`, transfers every
// referenced blob to `to` (skipping blobs already present via an Exists
// gate), then writes the ref as the single commit point. Each test below
// exercises one ACID invariant or crash-recovery row from that section.
//
// Delegated elsewhere:
//   - Blob compression + hash verification — CompressingStorage decorator.
//   - Isolation and conditional-write semantics — storage decorator or
//     the composition root.
//   - Write durability — the storage adapter.
package refs_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/refs"
	"ritual/internal/core/ritual"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPusher_UploadsRefAndEveryReferencedBlobToRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
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

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	remote.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
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

	local := newFSBundle(t)
	remote := newFSBundle(t)

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	err := pusher.Push(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"§Push step 1: missing local ref must surface an error — Pusher cannot push what it cannot load")
	assert.Empty(t, remote.keys(),
		"failed push with no local ref must leave remote untouched — ref load is the first action")
}

func TestPusher_ReturnsErrorWhenLocalRefInvalidJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	id := domain.RefID("2026-04-22T10-00-00.000Z")
	local.put(t, "refs/"+string(id)+".json", []byte("}{ not json"))

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	err := pusher.Push(ctx, id)

	require.Error(t, err,
		"§Push step 1 parse: an unparseable local ref must produce an error rather than an arbitrary remote write")
	assert.Empty(t, remote.keys(),
		"failed parse of local ref must leave remote untouched — nothing meaningful to upload from a broken ref")
}

func TestPusher_DoesNotWriteRefWhenLocalBlobMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	local.put(t, "refs/"+string(ref.Timestamp)+".json", body)

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	err = pusher.Push(ctx, ref.Timestamp)

	require.Error(t, err,
		"§Push step 2: a referenced blob absent locally must surface an error — Pusher cannot upload what it cannot read")
	assert.Empty(t, remoteRefKeys(remote),
		"§Push step 3 barrier: ref must NOT reach remote if any referenced blob failed to upload — ordering invariant")
	assertNoRefOnRemote(t, remote, ref.Timestamp)
}

func TestPusher_OnlyUploadsBlobsForFilesThatChangedAcrossRefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	version1 := map[string][]byte{
		"worlds/level.dat":  []byte("LEVEL_V1"),
		"worlds/region.mca": []byte("REGION_STABLE"),
	}
	ref1 := sampleRef("2026-04-22T09-00-00.000Z", version1)
	seedLocalForPush(t, local, ref1, version1)

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	require.NoError(t, pusher.Push(ctx, ref1.Timestamp),
		"first push must upload every referenced blob to the empty remote")

	version2 := map[string][]byte{
		"worlds/level.dat":  []byte("LEVEL_V2"),
		"worlds/region.mca": []byte("REGION_STABLE"),
	}
	ref2 := sampleRef("2026-04-22T10-00-00.000Z", version2)
	seedLocalForPush(t, local, ref2, version2)

	putsBeforeSecondPush := map[string]int{
		"objects/" + hashHex("LEVEL_V2"):      remote.putHits("objects/" + hashHex("LEVEL_V2")),
		"objects/" + hashHex("REGION_STABLE"): remote.putHits("objects/" + hashHex("REGION_STABLE")),
	}

	require.NoError(t, pusher.Push(ctx, ref2.Timestamp),
		"second push (with only level.dat changed between refs) must succeed")

	assert.Equal(t,
		putsBeforeSecondPush["objects/"+hashHex("LEVEL_V2")]+1,
		remote.putHits("objects/"+hashHex("LEVEL_V2")),
		"content-addressed incremental push: the blob for the file that CHANGED must be uploaded exactly once — new hash, not present on remote")
	assert.Equal(t,
		putsBeforeSecondPush["objects/"+hashHex("REGION_STABLE")],
		remote.putHits("objects/"+hashHex("REGION_STABLE")),
		"content-addressed incremental push: the blob for the file whose content did NOT change must not be re-uploaded — same content → same hash → remote Exists-gate skips the PUT")
}

func TestPusher_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
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

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	remote.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))

	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
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

func TestPusher_SurfacesTransferSentinelAndScrubsPartialOnBlobUploadFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFaultyStorage(newFSBundle(t))

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("CONTENT"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("CONTENT")})

	uplinkBroken := errors.New("simulated uplink failure on remote blob put")
	remote.putFail["objects/"+hashHex("CONTENT")] = uplinkBroken

	pusher := refs.NewPusher(local.storage, remote, serialRunner)
	err := pusher.Push(ctx, ref.Timestamp)

	require.Error(t, err,
		"§Push step 2: blob upload failure MUST surface — silent success would let the ref-last barrier lie about blob presence on remote")
	assert.ErrorIs(t, err, refs.ErrBlobTransfer,
		"error chain must classify via the shared blob-transfer sentinel — pull and push both mirror blobs across storages; callers filter on one category")
	assert.ErrorIs(t, err, uplinkBroken,
		"error chain must wrap the original PutStream cause — callers reaching for the concrete failure go via errors.Is/As")

	present, existsErr := remote.Exists(ctx, "objects/"+hashHex("CONTENT"))
	require.NoError(t, existsErr, "post-failure sanity: Exists probe against faulty remote must answer cleanly")
	assert.False(t, present,
		"§Push scrub-on-failure: no partial bytes under blob key after a failed upload — the next Push must see Exists == false so it retries the upload cleanly; without scrub a future push would observe Exists == true, skip, and commit a ref pointing at corrupt remote bytes (hanging-ref-via-corrupt-blob)")

	assertNoRefOnRemote(t, remote.bundle, ref.Timestamp)
}

func TestPusher_SurfacesRefPutFailureAfterBlobsLanded(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFaultyStorage(newFSBundle(t))

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	refKey := "refs/" + string(ref.Timestamp) + ".json"
	refPutFailure := errors.New("simulated ref PUT failure after all blobs uploaded")
	remote.putFail[refKey] = refPutFailure

	pusher := refs.NewPusher(local.storage, remote, serialRunner)
	err := pusher.Push(ctx, ref.Timestamp)

	require.Error(t, err,
		"§Push step 5 commit point: a failing ref PUT MUST surface — silent success would let the caller believe the remote HEAD was minted when no refs/{id}.json landed")
	blobPresent, existsErr := remote.Exists(ctx, "objects/"+hashHex("AAAA"))
	require.NoError(t, existsErr, "Exists probe against the faulty remote must not fail — only the ref key is faulted")
	assert.True(t, blobPresent,
		"§Push crash-recovery 'all blobs uploaded, before manifest PUT': blobs already durable on remote must remain — the next Push sees them via Exists-gate and retries only the ref write")
	assertNoRefOnRemote(t, remote.bundle, ref.Timestamp)
}

func TestPusher_OnPlan_AnnouncesFullRefBudgetWhenRemoteIsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})

	var plans []ritual.PlanInfo
	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	pusher.OnPlan(func(p ritual.PlanInfo) { plans = append(plans, p) })

	require.NoError(t, pusher.Push(ctx, ref.Timestamp), "push must succeed against an empty remote so the plan-callback contract is exercised on the happy path — failure paths are covered by other tests")

	require.Len(t, plans, 1, "OnPlan must fire exactly once per Push — duplicate plans would re-anchor the progress-bar denominator mid-run and confuse the user with a bar that resets")
	assert.Equal(t, "push", plans[0].Operation, "PlanInfo.Operation must be 'push' so the projection can disambiguate from the Pulling-stage plan when both are observed in the same run")
	assert.Equal(t, int64(4+8+16), plans[0].BytesTotal, "PlanInfo.BytesTotal must equal the delta — what will actually move. With an empty remote destination the delta equals the full ref total (28B); design-log/019.")
	assert.Equal(t, 3, plans[0].FilesTotal, "PlanInfo.FilesTotal must equal the count of blobs missing remotely — same 'delta == full ref' on an empty destination; same-hash duplicates collapse to one blob per Pusher's collectHashes contract")
}

func TestPusher_OnPlan_BytesTotalIsDeltaAfterRemoteExistsGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	local := newFSBundle(t)
	remote := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	seedLocalForPush(t, local, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	remote.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))
	remote.put(t, "objects/"+hashHex("BBBBBBBB"), []byte("BBBBBBBB"))

	var plans []ritual.PlanInfo
	pusher := refs.NewPusher(local.storage, remote.storage, serialRunner)
	pusher.OnPlan(func(p ritual.PlanInfo) { plans = append(plans, p) })

	require.NoError(t, pusher.Push(ctx, ref.Timestamp), "push must succeed with two of three blobs already on remote — idempotent per-blob")

	require.Len(t, plans, 1, "OnPlan must fire exactly once per Push")
	assert.Equal(t, int64(16), plans[0].BytesTotal, "PlanInfo.BytesTotal must equal the bytes that will actually move over the wire — only the one missing blob (16B), NOT the full ref total (4+8+16=28B). Progress bar, ETA and speed readouts all divide by this number; including blobs the Exists-gate will skip makes the bar finish at <100%% and inflates ETA proportional to dedup ratio.")
	assert.Equal(t, 1, plans[0].FilesTotal, "PlanInfo.FilesTotal must count only blobs that need uploading — 1 missing, not 3 referenced — so the 'N of M files' caption matches reality")
}

// --- push fixtures (prefix-named to avoid collision with Pull/Commit/Apply helpers) ---

func seedLocalForPush(t *testing.T, local *fsBundle, ref *domain.Ref, files map[string][]byte) {
	t.Helper()
	seedRemote(t, local, ref, files)
}

func remoteRefKeys(remote *fsBundle) []string {
	out := []string{}
	for _, k := range remote.keys() {
		if strings.HasPrefix(k, "refs/") {
			out = append(out, k)
		}
	}
	return out
}

func assertNoRefOnRemote(t *testing.T, remote *fsBundle, id domain.RefID) {
	t.Helper()
	present, err := remote.storage.Exists(context.Background(), "refs/"+string(id)+".json")
	require.NoError(t, err, "fixture: Exists probe must not fail")
	assert.False(t, present,
		"remote must not contain refs/%s.json after a failed push — §Push step 3 barrier: ref PUT is gated on blob barrier success", id)
}
