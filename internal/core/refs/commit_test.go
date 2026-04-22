// Package refs_test — Committer story:
//
// Commit is ACID per §Commit — ACID in docs/superpowers/specs/2026-04-19-
// fast-sync-v2.1-design.md. It walks the workdir against Targets globs,
// streams each matched file's raw bytes through xxhash into the blob store
// under objects/{hash} (skipping content already present), then writes a
// refs/{id}.json with a freshly-minted millisecond RefID and the captured
// (path, hash, size) map. Amend replaces a draft: new ref is written first,
// old draft ref is deleted second, and the new parent pointer inherits
// from the OLD draft's parent (no chain lengthening).
//
// Not in scope for Committer here (delegated elsewhere):
//   - Compression — CompressingStorage decorates blobs at composition root.
//   - Session lock + tick-mutex — orchestrator concern.
//   - Per-tick `save-all flush` — caller's responsibility in the live-
//     ticker loop; boot-time `save-off` already fires inside the
//     running-stage strategy.
//   - Remote amend-rejection (HeadObject 404 check) — Pusher concern.
//   - Parallelism (10 workers) — serial walk is the MVP.
//
// Rules for writing tests in this file (per ritual_integration_test.go):
//
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
//   - Custom fakes with story-friendly names; never generic mocks.
package refs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitter_WritesRefAndBlobsForEveryMatchedWorkdirFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))
	workdir.put(t, "worlds/region.mca", []byte("BBBBBBBB"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	id, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.NoError(t, err,
		"commit with matching targets and populated workdir must succeed — ACID consistency postcondition")
	assert.Equal(t, domain.RefID("2026-04-22T10-00-00.000Z"), id,
		"returned RefID must be the millisecond-precision dash-separated UTC timestamp minted from the clock")

	ref, ok := blobs.decodeRef(t, id)
	require.True(t, ok,
		"commit step 6 must write refs/{id}.json into the blob store after walking the workdir")

	levelHash := commitXXHashHex(t, []byte("AAAA"))
	regionHash := commitXXHashHex(t, []byte("BBBBBBBB"))
	assert.Equal(t, domain.Object{Hash: levelHash, Size: 4}, ref.Objects["worlds/level.dat"],
		"ref.Objects must record the matched file's xxhash64 hex and raw byte size — natural content-addressed identity")
	assert.Equal(t, domain.Object{Hash: regionHash, Size: 8}, ref.Objects["worlds/region.mca"],
		"ref.Objects must record every matched workdir file — walk must not drop entries")

	assert.Equal(t, []byte("AAAA"), blobs.mustGet(t, "objects/"+levelHash),
		"commit step 3 barrier: every referenced hash must exist at objects/{hash} after success")
	assert.Equal(t, []byte("BBBBBBBB"), blobs.mustGet(t, "objects/"+regionHash),
		"commit step 3 barrier: every referenced hash must exist at objects/{hash} after success")
}

func TestCommitter_IgnoresFilesNotMatchingTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))
	workdir.put(t, "server.jar", []byte("CCCC"))
	workdir.put(t, "logs/latest.log", []byte("DDDD"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	id, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.NoError(t, err,
		"commit with at least one matched file must succeed even when other files exist outside the globs")

	ref, ok := blobs.decodeRef(t, id)
	require.True(t, ok, "commit step 6 must write refs/{id}.json into the blob store")

	_, hasLevel := ref.Objects["worlds/level.dat"]
	assert.True(t, hasLevel,
		"ref.Objects must contain files matching the target glob — worlds/** is the declared tracked subtree")

	_, hasServer := ref.Objects["server.jar"]
	assert.False(t, hasServer,
		"ref.Objects must NOT contain files outside Targets — scope is data-driven by globs, not hardcoded")

	_, hasLog := ref.Objects["logs/latest.log"]
	assert.False(t, hasLog,
		"ref.Objects must NOT contain files outside Targets — logs/ is not listed in Targets")
}

func TestCommitter_SkipsBlobPutWhenContentAlreadyStored(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	hash := commitXXHashHex(t, []byte("AAAA"))

	firstClock := commitFixedClock(t, "2026-04-22T10:00:00.000Z")
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).WithClock(firstClock)
	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "first commit must succeed to populate the blob store")

	firstBlob := blobs.mustGet(t, "objects/"+hash)
	putsBeforeRecommit := blobs.putHits("objects/" + hash)

	committer.WithClock(commitFixedClock(t, "2026-04-22T10:00:01.000Z"))
	_, err = committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "second commit of identical content must succeed — idempotent per-blob")

	assert.Equal(t, putsBeforeRecommit, blobs.putHits("objects/"+hash),
		"commit Exists-gate: content already at objects/{hash} must NOT be re-written — content-addressed store is write-once per hash")
	assert.Equal(t, firstBlob, blobs.mustGet(t, "objects/"+hash),
		"commit must leave the existing blob byte-identical — re-Put risks a partial write that overwrites a good blob")
}

func TestCommitter_OnlyStoresBlobsForFilesThatChangedAcrossCommits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)

	workdir.put(t, "worlds/level.dat", []byte("LEVEL_V1"))
	workdir.put(t, "worlds/region.mca", []byte("REGION_STABLE"))

	stableHash := commitXXHashHex(t, []byte("REGION_STABLE"))
	changedV2Hash := commitXXHashHex(t, []byte("LEVEL_V2"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "first commit must succeed to populate the blob store for both files")

	workdir.put(t, "worlds/level.dat", []byte("LEVEL_V2"))

	putsBeforeSecondCommit := map[string]int{
		"objects/" + stableHash:    blobs.putHits("objects/" + stableHash),
		"objects/" + changedV2Hash: blobs.putHits("objects/" + changedV2Hash),
	}

	committer.WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err = committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "second commit (with only level.dat changed) must succeed")

	assert.Equal(t,
		putsBeforeSecondCommit["objects/"+changedV2Hash]+1,
		blobs.putHits("objects/"+changedV2Hash),
		"content-addressed incremental commit: the blob for the file whose content CHANGED must be written exactly once — new hash is absent from the blob store")
	assert.Equal(t,
		putsBeforeSecondCommit["objects/"+stableHash],
		blobs.putHits("objects/"+stableHash),
		"content-addressed incremental commit: the blob for the file whose content did NOT change must NOT be re-written — same hash, Exists-gate skips the write")
}

func TestCommitter_PropagatesParentFromOpts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	parent := domain.RefID("2026-04-22T09-00-00.000Z")
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	id, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  parent,
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "commit with explicit parent must succeed — parent is advisory metadata, not gating")

	ref, ok := blobs.decodeRef(t, id)
	require.True(t, ok, "commit step 6 must write refs/{id}.json into the blob store")
	assert.Equal(t, parent, ref.Parent,
		"ref.Parent must equal opts.Parent — Committer records lineage verbatim without reinterpretation")
}

func TestCommitter_AmendReplacesOldDraft(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	grandparent := domain.RefID("2026-04-22T08-00-00.000Z")
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	idA, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  grandparent,
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "first commit must succeed to produce the draft that will be amended")

	committer.WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	idB, err := committer.Commit(ctx, ports.CommitOpts{
		Amend:   idA,
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "amend commit must succeed — draft is local-only and the remote-presence check is Pusher's job")

	refB, ok := blobs.decodeRef(t, idB)
	require.True(t, ok, "commit step 6 must write the new refs/{id}.json for the amend")
	assert.Equal(t, grandparent, refB.Parent,
		"amend must inherit the old draft's parent — no chain lengthening per §Commit Amend step 3")

	existsOld, err := blobs.storage.Exists(ctx, "refs/"+string(idA)+".json")
	require.NoError(t, err, "Exists probe against the on-disk store must not fail")
	assert.False(t, existsOld,
		"amend step 5 must delete the old draft's ref JSON — leaving it behind makes max(timestamp) pick the stale lineage")
}

func TestCommitter_AmendInvokesLocalGCAfterDeletingOldDraft(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	firstID, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fixture: first commit must land the draft that the amend will supersede")

	gcCalls := 0
	oldDraftPresentAtGC := true
	committer.
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z")).
		WithLocalGC(func(gcCtx context.Context) error {
			gcCalls++
			present, existsErr := blobs.storage.Exists(gcCtx, "refs/"+string(firstID)+".json")
			require.NoError(t, existsErr, "localGC closure: Exists probe on the on-disk store must not fail")
			oldDraftPresentAtGC = present
			return nil
		})

	_, err = committer.Commit(ctx, ports.CommitOpts{Amend: firstID, Targets: []string{"worlds/**"}})
	require.NoError(t, err, "amend commit must succeed when the injected local GC returns nil")

	assert.Equal(t, 1, gcCalls,
		"§Retention and GC 'Local GC after amend': localGC must run exactly once per amend so the superseded draft's exclusive blobs get reaped")
	assert.False(t, oldDraftPresentAtGC,
		"§Retention and GC ordering: localGC must run AFTER the old draft delete so the live-set scan no longer holds the superseded draft's hashes")
}

func TestCommitter_FreshCommitDoesNotInvokeLocalGC(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	gcCalls := 0
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z")).
		WithLocalGC(func(context.Context) error {
			gcCalls++
			return nil
		})

	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fresh commit must succeed regardless of whether a local GC closure is wired")

	assert.Equal(t, 0, gcCalls,
		"§Retention and GC triggers: only `amend → localGC` runs the sweep — fresh commits must leave GC to the `push → retention → gc` chain so no spurious work fires per tick")
}

func TestCommitter_AmendPropagatesLocalGCError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	firstID, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fixture: first commit must land the draft that the amend will supersede")

	gcFailure := errors.New("simulated local GC failure mid-sweep")
	committer.
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z")).
		WithLocalGC(func(context.Context) error { return gcFailure })

	_, err = committer.Commit(ctx, ports.CommitOpts{Amend: firstID, Targets: []string{"worlds/**"}})

	require.Error(t, err,
		"amend with a failing local GC must surface the error so the caller learns the orphan sweep did not complete — silent swallow would mask bounded-cache violations")
	assert.ErrorIs(t, err, gcFailure,
		"amend must wrap the injected GC closure's error verbatim — caller loses the cause otherwise and cannot decide retry vs. abort")
}

func TestCommitter_ReturnsErrorWhenNoWorkdirFilesMatchTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "server.jar", []byte("CCCC"))
	workdir.put(t, "logs/latest.log", []byte("DDDD"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.Error(t, err,
		"commit with zero matched files must surface an error — a ref with empty Objects is almost certainly a glob bug and masking it hides data loss")
	assert.Empty(t, blobs.keys(),
		"failed commit must leave the blob store untouched — no partial ref write and no orphaned blobs")
}

func TestCommitter_DoesNotWriteRefWhenBlobPutFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	blobs := newFaultyStorage(newFSBundle(t))
	failingBlobKey := "objects/" + commitXXHashHex(t, []byte("AAAA"))
	blobs.putFail[failingBlobKey] = errors.New("simulated R2 503")

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.Error(t, err,
		"§Commit step 4 barrier: a blob put failure must abort commit — the barrier '∀ hash ∈ M.objects.values : exists(blobs/{hash})' must hold before the ref write gate")
	refKeys := keysWithPrefix(blobs.bundle, "refs/")
	assert.Empty(t, refKeys,
		"§Commit step 4 ordering: no ref must be written when any referenced blob fails to persist — ref write is gated on the barrier")
}

func TestCommitter_LeavesPartialBlobsWhenRefWriteFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))
	workdir.put(t, "worlds/region.mca", []byte("BBBBBBBB"))

	blobs := newFaultyStorage(newFSBundle(t))
	committedID := domain.RefID("2026-04-22T10-00-00.000Z")
	blobs.putFail["refs/"+string(committedID)+".json"] = errors.New("simulated ref put failure mid-write")

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.Error(t, err,
		"§Commit crash-recovery 'Mid walk': ref-write failure must surface as an error so the caller knows no new HEAD was minted")

	levelBlobPresent, err := blobs.Exists(ctx, "objects/"+commitXXHashHex(t, []byte("AAAA")))
	require.NoError(t, err, "Exists probe against faulty store's blob key must not fail — only putFail is injected")
	assert.True(t, levelBlobPresent,
		"§Commit crash-recovery 'Mid walk': successfully stored blobs must remain in the cache — content-addressed blobs are harmless and get reaped by local GC or re-used by the next commit")

	regionBlobPresent, err := blobs.Exists(ctx, "objects/"+commitXXHashHex(t, []byte("BBBBBBBB")))
	require.NoError(t, err, "Exists probe against faulty store's blob key must not fail — only putFail is injected")
	assert.True(t, regionBlobPresent,
		"§Commit crash-recovery 'Mid walk': every blob that landed before the ref-write failure must remain on disk — monotone forward progress, not all-or-nothing")

	refKeys := keysWithPrefix(blobs.bundle, "refs/")
	assert.Empty(t, refKeys,
		"§Commit 'Mid walk': no ref must be present when the ref write itself fails — the failing put never succeeded")
}

func TestCommitter_AmendWritesNewRefBeforeDeletingOldDraft(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	blobs := newFaultyStorage(newFSBundle(t))
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	firstID, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fixture: first commit must land a draft for the amend to replace")

	blobs.deleteFail["refs/"+string(firstID)+".json"] = errors.New("simulated delete failure between write-new and delete-old")
	committer.WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, amendErr := committer.Commit(ctx, ports.CommitOpts{
		Amend:   firstID,
		Targets: []string{"worlds/**"},
	})

	require.Error(t, amendErr,
		"amend with a failing old-draft delete must surface the delete error — the leaked old draft is a known observable crash-recovery state")

	newID := domain.RefID("2026-04-22T10-00-00.000Z")
	_, newPresent := blobs.decodeRef(t, newID)
	assert.True(t, newPresent,
		"§Commit Atomicity 'Ordering: write new manifest BEFORE delete old': the new ref must be on disk even when the old-draft delete fails — proves the ordering, since a delete-before-write order would have no new ref under this failure")

	_, oldPresent := blobs.decodeRef(t, firstID)
	assert.True(t, oldPresent,
		"§Commit crash row 'Between manifest write and old-draft delete': both drafts exist briefly; max(timestamp) picks the new one on recovery")
}

// --- commit test fixtures ---

func commitFixedClock(t *testing.T, rfc3339Milli string) func() time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", rfc3339Milli)
	require.NoError(t, err,
		"test fixture: fixed clock input %q must parse as RFC3339 with millisecond precision", rfc3339Milli)
	return func() time.Time { return parsed }
}

func commitXXHashHex(t *testing.T, data []byte) string {
	t.Helper()
	h := xxhash.New()
	_, err := h.Write(data)
	require.NoError(t, err, "test fixture: xxhash.Digest.Write must not fail on in-memory bytes")
	return fmt.Sprintf("%016x", h.Sum64())
}
