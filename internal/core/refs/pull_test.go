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
	"ritual/internal/core/ritual"

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

func TestPuller_LeavesLocalUntouchedWhenOriginHasNoRefsAtAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err := puller.Pull(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"cold-client pull against a fully empty origin (no refs/, no objects/) must surface an error — there is no HEAD to materialise and silent success would falsely advertise one")
	assert.Empty(t, remote.keys(),
		"origin invariant: Pull must not mutate the source side — a failed pull against an empty origin must leave origin still empty")
	assert.Empty(t, local.keys(),
		"§Pull ref-last barrier: a failed ref fetch must leave the destination byte-identical to its pre-call state — no refs/, no objects/, nothing under any other prefix that a future run could misinterpret as resumable progress")
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
	assert.ErrorIs(t, err, refs.ErrBlobTransfer,
		"error chain must classify via ErrBlobTransfer so callers filter on the failure category")
	assert.ErrorIs(t, err, uplinkBroken,
		"error chain must wrap the original PutStream cause; callers that want the concrete failure reach it via errors.Is/As")
	present, existsErr := local.storage.Exists(ctx, "objects/"+hashHex("CONTENT"))
	require.NoError(t, existsErr, "post-failure sanity: Exists must answer cleanly")
	assert.False(t, present,
		"post-failure invariant: no partial bytes under the blob key — the scrub-on-failure path must leave destination clean so the next Pull sees Exists == false and starts fresh")
}

func TestPuller_ResumesWhenBlobsAlreadyLocalButRefMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, remote, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	local.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))

	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	err := puller.Pull(ctx, ref.Timestamp)

	require.NoError(t, err,
		"§Pull crash-recovery row 'all blobs pulled, before ref commit': a retry where blobs already exist locally but ref is absent must succeed on a ref fetch alone — mirror of Push's ResumesAfterBlobsUploadedButRefMissing")
	pulled, ok := local.decodeRef(t, ref.Timestamp)
	require.True(t, ok,
		"resumed pull must land the missing ref JSON locally — ref-last barrier's recovery branch")
	assert.Equal(t, ref.Objects, pulled.Objects,
		"resumed pull's ref must carry the remote's object map verbatim — no re-interpretation across the crash boundary")
	assert.Equal(t, 0, remote.getHits("objects/"+hashHex("AAAA")),
		"resumed pull must not re-fetch blobs already present locally — Exists-gate holds across the crash-resume boundary, otherwise bandwidth amplifies on every retry")
}

func TestPuller_OnPlan_AnnouncesFullRefBudgetWhenLocalIsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})

	var plans []ritual.PlanInfo
	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	puller.OnPlan(func(p ritual.PlanInfo) { plans = append(plans, p) })

	require.NoError(t, puller.Pull(ctx, ref.Timestamp), "pull must succeed against a complete remote so the plan-callback contract is exercised on the happy path — failure paths are covered by other tests")

	require.Len(t, plans, 1, "OnPlan must fire exactly once per Pull — duplicate plans would re-anchor the progress-bar denominator mid-run and confuse the user with a bar that resets")
	assert.Equal(t, "pull", plans[0].Operation, "PlanInfo.Operation must be 'pull' so the projection can disambiguate from the Pushing-stage plan when both are observed in the same run")
	assert.Equal(t, int64(4+8+16), plans[0].BytesTotal, "PlanInfo.BytesTotal must equal the delta — what will actually move. With an empty local destination the delta equals the full ref total (28B); design-log/019.")
	assert.Equal(t, 3, plans[0].FilesTotal, "PlanInfo.FilesTotal must equal the count of blobs missing locally — same 'delta == full ref' on an empty destination; same-hash duplicates collapse to one blob per Puller's collectHashes contract")
}

func TestPuller_OnPlan_FiresWithZeroBudgetWhenAllBlobsAlreadyPresentLocally(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, remote, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	local.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))

	var plans []ritual.PlanInfo
	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	puller.OnPlan(func(p ritual.PlanInfo) { plans = append(plans, p) })

	require.NoError(t, puller.Pull(ctx, ref.Timestamp), "pull must succeed when blobs are already present locally — idempotent re-runs are part of the ACID contract")

	require.Len(t, plans, 1, "OnPlan must fire exactly once per Pull regardless of how many blobs need transferring — the projection wires it to bus.Publish unconditionally so the dial can render an immediate 'complete-on-arrival' frame")
	assert.Equal(t, int64(0), plans[0].BytesTotal, "PlanInfo.BytesTotal must announce only the delta — bytes the runtime will actually move. Everything already on disk → 0 budget; projection treats 0/0 as 100%% per design-log/019.")
	assert.Equal(t, 0, plans[0].FilesTotal, "PlanInfo.FilesTotal must count only blobs missing locally — 0 here so the 'N of M files' caption doesn't lie about work that won't happen")
}

func TestPuller_OnPlan_BytesTotalIsDeltaAfterLocalExistsGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
		"worlds/playerdata": []byte("CCCCCCCCCCCCCCCC"),
	})
	local.put(t, "objects/"+hashHex("AAAA"), []byte("AAAA"))
	local.put(t, "objects/"+hashHex("BBBBBBBB"), []byte("BBBBBBBB"))

	var plans []ritual.PlanInfo
	puller := refs.NewPuller(remote.storage, local.storage, serialRunner)
	puller.OnPlan(func(p ritual.PlanInfo) { plans = append(plans, p) })

	require.NoError(t, puller.Pull(ctx, ref.Timestamp), "pull must succeed with two of three blobs already present locally")

	require.Len(t, plans, 1, "OnPlan must fire exactly once per Pull")
	assert.Equal(t, int64(16), plans[0].BytesTotal, "PlanInfo.BytesTotal must equal the bytes that will actually download — only the one missing blob (16B), NOT the full ref total (4+8+16=28B). Progress bar, ETA and speed readouts all divide by this number; including blobs the Exists-gate will skip makes the bar finish at <100%% and inflates ETA proportional to dedup ratio.")
	assert.Equal(t, 1, plans[0].FilesTotal, "PlanInfo.FilesTotal must count only blobs that need downloading — 1 missing, not 3 referenced")
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
