// Package refs_test — Applier story:
//
// Apply is ACID per §Apply — ACID in docs/superpowers/specs/2026-04-19-fast-
// sync-v2.1-design.md. It consumes a ref that Puller already hydrated into
// the blob store and materialises each referenced object into the workdir,
// skipping files already present and pruning in-scope files the ref no
// longer references. Each test below exercises one ACID invariant from
// that section. Paths in the ref are root-relative; workdir IS the root.
//
// Not in scope for Applier (delegated elsewhere):
//   - Blob decompression + xxhash verify — CompressingStorage decorator.
//   - Stale `.ritualapply.tmp` sweep, Windows rename guards — post-MVP.
//   - Parallel worker pool — post-MVP; Apply is serial.
//   - Session lock / Minecraft quiesce — orchestrator concern.
//
// Rules for writing tests in this file (per ritual_integration_test.go):
//
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
//   - Reuse memStorage + sampleRef + hashKeyFor + seedRemote from
//     pull_test.go; do not redefine.
package refs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ritual/internal/core/refs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplier_MaterialisesEveryRefObjectIntoWorkdir(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, blobs, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})

	applier := refs.NewApplier(blobs, workdir)
	err := applier.Apply(ctx, ref.Timestamp)

	require.NoError(t, err,
		"apply with fully-hydrated blobs and empty workdir must succeed — ACID consistency postcondition")
	assert.Equal(t, []byte("AAAA"), workdir.mustGet(t, "worlds/level.dat"),
		"apply step 3: every ref.Objects entry must land at workdir/<path> with blob bytes — placement invariant")
	assert.Equal(t, []byte("BBBBBBBB"), workdir.mustGet(t, "worlds/region.mca"),
		"apply step 3: every ref.Objects entry must land at workdir/<path> with blob bytes — placement invariant")
}

func TestApplier_SkipsFilesAlreadyPresentInWorkdir(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	seedRemote(t, blobs, ref, map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	})
	workdir.put("worlds/level.dat", []byte("AAAA"))

	applier := refs.NewApplier(blobs, workdir)
	err := applier.Apply(ctx, ref.Timestamp)

	require.NoError(t, err,
		"apply with one workdir file already present must still succeed — idempotent per-file")
	assert.Equal(t, 0, blobs.getHits("objects/"+hashHex("AAAA")),
		"apply skip gate: blob backing a workdir file that already exists must not be re-read — wastes IO and violates MVP skip contract")
	assert.Equal(t, 1, blobs.getHits("objects/"+hashHex("BBBBBBBB")),
		"apply step 3: blob backing a missing workdir file must be fetched exactly once")
	assert.Equal(t, []byte("BBBBBBBB"), workdir.mustGet(t, "worlds/region.mca"),
		"apply step 3: previously-missing workdir file must land after success")
}

func TestApplier_PrunesWorkdirPathsNotInRef(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, blobs, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	workdir.put("worlds/stale.dat", []byte("STALE"))

	applier := refs.NewApplier(blobs, workdir)
	err := applier.Apply(ctx, ref.Timestamp)

	require.NoError(t, err,
		"apply with an in-scope stale file must succeed and prune it — ACID consistency postcondition")
	present, existsErr := workdir.Exists(ctx, "worlds/stale.dat")
	require.NoError(t, existsErr, "workdir Exists must not error on a pruned path")
	assert.False(t, present,
		"apply step 4 prune: workdir paths matching ref.Targets but absent from ref.Objects must be deleted — scope invariant")
	assert.Equal(t, []byte("AAAA"), workdir.mustGet(t, "worlds/level.dat"),
		"apply step 3: pruning must not disturb files the ref still references")
}

func TestApplier_LeavesOutOfScopePathsUntouched(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, blobs, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	workdir.put("server/mods.cfg", []byte("OUTSIDE"))

	applier := refs.NewApplier(blobs, workdir)
	err := applier.Apply(ctx, ref.Timestamp)

	require.NoError(t, err,
		"apply with an out-of-scope pre-existing file must succeed — presence outside Targets is orthogonal to apply")
	assert.Equal(t, []byte("OUTSIDE"), workdir.mustGet(t, "server/mods.cfg"),
		"apply scope guard: paths outside ref.Targets must be untouched — the second consistency invariant forbids Apply from exceeding its jurisdiction")
}

func TestApplier_ReturnsErrorWhenRefMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	applier := refs.NewApplier(blobs, workdir)
	err := applier.Apply(ctx, "2026-04-22T10-00-00.000Z")

	require.Error(t, err,
		"apply of a ref id absent from the blob store must surface an error — silent success would materialise nothing and masquerade as done")
	assert.Empty(t, workdir.keys(),
		"failed apply with no ref must leave workdir untouched — ref load is the first read and must gate all mutation")
}

func TestApplier_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, blobs, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})

	applier := refs.NewApplier(blobs, workdir)
	require.NoError(t, applier.Apply(ctx, ref.Timestamp),
		"first apply must succeed on a fully-hydrated blob store")

	hitsBeforeRerun := blobs.getHits("objects/"+hashHex("AAAA"))
	require.NoError(t, applier.Apply(ctx, ref.Timestamp),
		"second apply on an already-materialised workdir must succeed — Apply is idempotent across replays")

	assert.Equal(t, hitsBeforeRerun, blobs.getHits("objects/"+hashHex("AAAA")),
		"atomicity via idempotent stage replay: a second apply must not re-read blobs whose workdir files already exist")
	assert.Equal(t, []byte("AAAA"), workdir.mustGet(t, "worlds/level.dat"),
		"apply step 3: the referenced file must remain present and intact after a second apply")
}

func TestApplier_ReturnsErrorWhenReferencedBlobMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	blobs := newMemStorage()
	workdir := newMemStorage()

	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	refBody, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: ref must marshal to JSON")
	blobs.put("refs/"+string(ref.Timestamp)+".json", refBody)

	applier := refs.NewApplier(blobs, workdir)
	err = applier.Apply(ctx, ref.Timestamp)

	require.Error(t, err,
		"§Apply step 3: a referenced blob absent from the blob store must surface an error — silent skip would leave the workdir structurally incomplete")
	assert.Empty(t, workdir.keys(),
		"apply must abort before mutating workdir when the first referenced blob it reaches is absent — spec requires monotone forward progress, and pre-any-write abort is the cheap base case; a retry after the blob is restored is what completes the ref (covered by IsIdempotentAcrossReruns)")
}
