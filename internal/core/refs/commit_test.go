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
	"encoding/json"
	"errors"
	"fmt"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"testing"
	"time"

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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).WithClock(firstClock)
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
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
	blobs.putFail[failingBlobKey] = errors.New("simulated blob put failure")

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs, serialRunner).
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

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs, serialRunner).
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
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs, serialRunner).
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

func TestCommitter_AmendRetryAfterInterruptedFinalizeSweepsOrphanSiblingRef(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	blobs := newFaultyStorage(newFSBundle(t))
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs, serialRunner).
		WithClock(commitFixedClock(t, "2026-04-22T09:00:00.000Z"))
	draft1, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fixture: initial commit must land the first draft so an amend has something to replace")

	blobs.deleteFail["refs/"+string(draft1)+".json"] = errors.New("simulated transient delete failure between write-new and delete-old")
	committer.WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	draft2 := domain.RefID("2026-04-22T10-00-00.000Z")
	_, firstAmendErr := committer.Commit(ctx, ports.CommitOpts{Amend: draft1, Targets: []string{"worlds/**"}})
	require.Error(t, firstAmendErr, "fixture: first amend must fail at old-draft delete so draft2 is written but draft1 is not deleted — interrupted finalize")
	_, draft2Present := blobs.decodeRef(t, draft2)
	require.True(t, draft2Present, "fixture precondition: draft2 must be on disk after the interrupted finalize so the retry scenario is meaningful")

	delete(blobs.deleteFail, "refs/"+string(draft1)+".json")
	committer.WithClock(commitFixedClock(t, "2026-04-22T11:00:00.000Z"))
	draft3, err := committer.Commit(ctx, ports.CommitOpts{Amend: draft1, Targets: []string{"worlds/**"}})
	require.NoError(t, err, "retry amend with the same Amend target (draft1 still present) must succeed once the transient delete fault clears")

	refKeys := keysWithPrefix(blobs.bundle, "refs/")
	assert.Equal(t, []string{"refs/" + string(draft3) + ".json"}, refKeys,
		"amend-retry hanging-ref invariant: after the retry lands only the newest draft must remain under refs/ — draft1 is the explicit amend target (deleted), draft2 is the orphan sibling left behind by the interrupted first attempt; user accepts orphan OBJECTS (localGC sweeps them) but orphan REFS accumulate into a hanging-ref graveyard across retries and must be swept. A same-Parent sibling with a strictly smaller Timestamp than the just-written ref is the signature of a superseded amend attempt and is safe to delete.")
}

func TestCommitter_ErrorsOnAmendOfNonexistentDraft(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	missingDraft := domain.RefID("2026-04-22T08-00-00.000Z")
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err := committer.Commit(ctx, ports.CommitOpts{
		Amend:   missingDraft,
		Targets: []string{"worlds/**"},
	})

	require.Error(t, err,
		"amend of a RefID that is absent from the blob store MUST surface an error — silently converting the amend into a fresh commit discards the caller's intent and may produce a chain-lengthening ref the caller expected to be inherited-parent")
	refKeys := keysWithPrefix(blobs, "refs/")
	assert.Empty(t, refKeys,
		"amend-missing-draft ref barrier: no new ref must land when the amend target cannot be resolved — the caller's lineage contract cannot be honoured; content-addressed blobs written before the failure are harmless (reaped by the next GC) but a ref pointing at the wrong parent is not")
}

func TestCommitter_SurfacesContextCanceledBeforeAnyWrite(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	cancelledCtx, cancelNow := context.WithCancel(parentCtx)
	cancelNow()

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	_, err := committer.Commit(cancelledCtx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.Error(t, err,
		"commit under a cancelled context MUST surface an error — silently producing a ref from a cancelled caller would commit work the caller explicitly abandoned")
	assert.ErrorIs(t, err, context.Canceled,
		"cancellation error chain must preserve context.Canceled — callers that retry vs. abort branch on this sentinel")
	assert.Empty(t, blobs.keys(),
		"commit atomicity under cancellation: no refs, no blobs — the store must be byte-identical to the pre-call state so a retry starts fresh with no orphan writes")
}

func TestCommitter_RejectsClockCollisionWithExistingRef(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	stuckClock := commitFixedClock(t, "2026-04-22T10:00:00.000Z")
	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).WithClock(stuckClock)
	firstID, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "fixture: first commit at the fixed clock tick must succeed to plant the ref that a same-ms retry would overwrite")

	firstRef, ok := blobs.decodeRef(t, firstID)
	require.True(t, ok, "fixture: first ref must be readable before the collision attempt so the 'byte-identical after reject' assertion is meaningful")
	firstBody, err := json.Marshal(firstRef)
	require.NoError(t, err, "fixture: first ref must remarshal deterministically so the post-attempt comparison is exact")

	workdir.put(t, "worlds/level.dat", []byte("ZZZZ"))
	_, secondErr := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})

	require.Error(t, secondErr,
		"single-writer invariant: two commits that mint the SAME RefID (clock stuck at the same millisecond) MUST be refused — silent overwrite replaces refs/{id}.json with a ref whose Objects differ from the one retention already observed, destroying lineage. User classified this as a critical-bug class, not a future-feature gap")

	survivingRef, stillOk := blobs.decodeRef(t, firstID)
	require.True(t, stillOk, "post-collision: the original refs/{id}.json must remain readable — rejection means the second commit performed no writes at the ref key")
	survivingBody, err := json.Marshal(survivingRef)
	require.NoError(t, err, "post-collision: surviving ref must remarshal deterministically for the byte-for-byte comparison")
	assert.Equal(t, firstBody, survivingBody,
		"collision rejection must be byte-preserving: the ref on disk is still the first commit's ref, not a mutated hybrid — the collision path must not write ANYTHING at refs/{id}.json")
}

func TestCommitter_RefFileIsHumanReadableJSON_NotMinified(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)
	workdir.put(t, "worlds/level.dat", []byte("AAAA"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	id, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "commit must succeed before the on-disk format can be inspected")

	body := blobs.mustGet(t, "refs/"+string(id)+".json")

	assert.Contains(t, string(body), "\n",
		"POC fix #7 regression: refs/{id}.json on disk must be MarshalIndent-formatted (newline + 2-space indent) so an operator can `cat` the file and read it without reflowing — the v1 single-line `json.Marshal` form left users staring at an opaque one-liner")
	assert.Contains(t, string(body), "\n  \"",
		"POC fix #7 regression: indented refs JSON must use a 2-space indent for nested fields — matches the agreed wire format and prevents accidental reversion to single-line marshal")
}

// Audit fix #8 regression (docs/dev-session-2026-04-25-poc-setup.md).
// Pre-fix: workdir was rooted at <root>/worlds and Targets was ["**"], so
// nothing under server/ was tracked and a fresh host could not pull-and-
// run. Fix: workdir = project root, Targets = config.DefaultCommitTargets
// allowlist. The allowlist deliberately omits operational dirs (refs/,
// objects/, logs/, remote-mock/), the user-local settings file, and the
// server's regenerated caches (server/logs, server/usercache.json,
// server/.cache) so they never enter a ref or get pruned by a downstream
// Apply.
//
// This test passes the production allowlist as Targets over a workdir
// that mirrors the post-#8 layout. Reverting the allowlist to ["**"] or
// dropping a server/ subtree will fail this test loudly.
func TestCommitter_DefaultCommitTargets_CapturePlaySurface_ExcludeOperationalDirs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workdir := newFSBundle(t)
	blobs := newFSBundle(t)

	workdir.put(t, "worlds/world/level.dat", []byte("level"))
	workdir.put(t, "worlds/world/region/r.0.0.mca", []byte("region"))
	workdir.put(t, "server/server.jar", []byte("jar"))
	workdir.put(t, "server/server.properties", []byte("props"))
	workdir.put(t, "server/eula.txt", []byte("eula"))
	workdir.put(t, "server/start.bat", []byte("bat"))
	workdir.put(t, "server/user_jvm_args.txt", []byte("jvm"))
	workdir.put(t, "server/libraries/net/neoforged/neoforge/win_args.txt", []byte("args"))
	workdir.put(t, "server/mods/cool-mod.jar", []byte("mod"))
	workdir.put(t, "server/config/cool-mod.toml", []byte("cfg"))
	workdir.put(t, "server/defaultconfigs/cool-mod.toml", []byte("dcfg"))
	workdir.put(t, "server/ops.json", []byte("[]"))
	workdir.put(t, "server/whitelist.json", []byte("[]"))
	workdir.put(t, "server/banned-ips.json", []byte("[]"))
	workdir.put(t, "server/banned-players.json", []byte("[]"))

	workdir.put(t, "server/logs/latest.log", []byte("log"))
	workdir.put(t, "server/usercache.json", []byte("uc"))
	workdir.put(t, "server/usernamecache.json", []byte("unc"))
	workdir.put(t, "server/.cache/forge_versioning.json", []byte("cache"))
	workdir.put(t, "refs/2026-04-22T10-00-00.000Z.json", []byte("{}"))
	workdir.put(t, "objects/abcdef0123456789", []byte("blob"))
	workdir.put(t, "remote-mock/refs/2026-04-22T10-00-00.000Z.json", []byte("{}"))
	workdir.put(t, "logs/20260422100000.log", []byte("session-log"))
	workdir.put(t, "settings.json", []byte("{}"))

	committer := refs.NewCommitter(workdir.scanner(), workdir.storage, blobs.storage, serialRunner).
		WithClock(commitFixedClock(t, "2026-04-22T10:00:00.000Z"))
	id, err := committer.Commit(ctx, ports.CommitOpts{Targets: config.DefaultCommitTargets})

	require.NoError(t, err,
		"production allowlist over the real layout must succeed — at least one matched file (worlds/world/level.dat) is present so the empty-match guard does not trip")

	ref, ok := blobs.decodeRef(t, id)
	require.True(t, ok, "commit must write refs/{id}.json after a successful match")

	mustInclude := []string{
		"worlds/world/level.dat",
		"worlds/world/region/r.0.0.mca",
		"server/server.jar",
		"server/server.properties",
		"server/eula.txt",
		"server/start.bat",
		"server/user_jvm_args.txt",
		"server/libraries/net/neoforged/neoforge/win_args.txt",
		"server/mods/cool-mod.jar",
		"server/config/cool-mod.toml",
		"server/defaultconfigs/cool-mod.toml",
		"server/ops.json",
		"server/whitelist.json",
		"server/banned-ips.json",
		"server/banned-players.json",
	}
	for _, path := range mustInclude {
		_, present := ref.Objects[path]
		assert.Truef(t, present,
			"audit fix #8: production allowlist must capture %q — fresh host needs the full play surface (worlds/+server runtime+config+identity files) to pull-and-run", path)
	}

	mustExclude := []string{
		"server/logs/latest.log",
		"server/usercache.json",
		"server/usernamecache.json",
		"server/.cache/forge_versioning.json",
		"refs/2026-04-22T10-00-00.000Z.json",
		"objects/abcdef0123456789",
		"remote-mock/refs/2026-04-22T10-00-00.000Z.json",
		"logs/20260422100000.log",
		"settings.json",
	}
	for _, path := range mustExclude {
		_, present := ref.Objects[path]
		assert.Falsef(t, present,
			"audit fix #8: production allowlist must NOT capture %q — operational dirs and host-local caches must never enter a ref or get destroyed by a downstream Apply prune", path)
	}
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
