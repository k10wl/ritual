// Package refs_test — Collector story:
//
// Collect implements §Retention and GC's GC algorithm. It scans every
// `refs/*.json` present in the store, collects the set of hashes those
// refs reference, then deletes every `objects/{hash}` whose hash is not
// live. Refs themselves are untouched — retention (which refs survive)
// is a separate concern the caller handles by deleting refs before
// invoking Collect.
//
// Not in scope for Collector (delegated elsewhere):
//   - Retention policy (last-N / daily / weekly / monthly) — pure function
//     that returns the set of refs to delete; lives outside this verb.
//   - Grace period — §Retention and GC says "no grace period" because
//     session lock + mark-sweep makes it unnecessary; Collector trusts
//     the lock invariant.
//   - Blob integrity verification — CompressingStorage decorator.
package refs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/refs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollector_DeletesBlobsNotReferencedByAnyRef(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, store, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	orphanKey := "objects/" + hashHex("ORPHAN")
	store.put(t, orphanKey, []byte("ORPHAN"))

	collector := refs.NewCollector(store.storage)
	err := collector.Collect(ctx)

	require.NoError(t, err,
		"GC over a well-formed store must succeed — §Retention and GC defines this as the happy path")
	orphanPresent, err := store.storage.Exists(ctx, orphanKey)
	require.NoError(t, err, "Exists probe against the on-disk store must not fail")
	assert.False(t, orphanPresent,
		"GC algorithm: objects/{hash} not referenced by any surviving ref must be deleted — unreferenced blobs are orphans, nothing else will ever reach them")
	livePresent, err := store.storage.Exists(ctx, "objects/"+hashHex("AAAA"))
	require.NoError(t, err, "Exists probe against the on-disk store must not fail")
	assert.True(t, livePresent,
		"GC algorithm: blobs referenced by a surviving ref must remain — the live set gates deletion")
}

func TestCollector_KeepsBlobsReferencedByAnyRef(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	files := map[string][]byte{
		"worlds/level.dat":  []byte("AAAA"),
		"worlds/region.mca": []byte("BBBBBBBB"),
	}
	ref := sampleRef("2026-04-22T10-00-00.000Z", files)
	seedRemote(t, store, ref, files)

	collector := refs.NewCollector(store.storage)
	require.NoError(t, collector.Collect(ctx),
		"GC over a store where every blob is referenced must succeed")

	assert.Equal(t, []byte("AAAA"), store.mustGet(t, "objects/"+hashHex("AAAA")),
		"GC live-set preservation: referenced blob must still contain original bytes")
	assert.Equal(t, []byte("BBBBBBBB"), store.mustGet(t, "objects/"+hashHex("BBBBBBBB")),
		"GC live-set preservation: referenced blob must still contain original bytes")
}

func TestCollector_RetainsBlobStillReferencedAfterAnotherRefDeleted(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	shared := []byte("SHARED")
	filesA := map[string][]byte{
		"worlds/level.dat": shared,
		"worlds/a.dat":     []byte("UNIQUE_A"),
	}
	filesB := map[string][]byte{
		"worlds/level.dat": shared,
		"worlds/b.dat":     []byte("UNIQUE_B"),
	}
	refA := sampleRef("2026-04-22T10-00-00.000Z", filesA)
	refB := sampleRef("2026-04-22T11-00-00.000Z", filesB)
	seedRemote(t, store, refA, filesA)
	seedRemote(t, store, refB, filesB)
	require.NoError(t, store.storage.Delete(ctx, "refs/"+string(refA.Timestamp)+".json"),
		"fixture: deleting refA before GC mimics the retention stage picking to drop refA")

	collector := refs.NewCollector(store.storage)
	require.NoError(t, collector.Collect(ctx),
		"GC after retention drop must succeed")

	sharedPresent, err := store.storage.Exists(ctx, "objects/"+hashHex("SHARED"))
	require.NoError(t, err, "Exists probe must not fail")
	assert.True(t, sharedPresent,
		"GC live-set: a blob referenced by ANY surviving ref must survive — shared content stays while exclusive content of the dropped ref is swept")
	uniqueAPresent, err := store.storage.Exists(ctx, "objects/"+hashHex("UNIQUE_A"))
	require.NoError(t, err, "Exists probe must not fail")
	assert.False(t, uniqueAPresent,
		"GC sweep: a blob exclusively referenced by the deleted ref must be swept — no ref reaches it, so it is an orphan")
	uniqueBPresent, err := store.storage.Exists(ctx, "objects/"+hashHex("UNIQUE_B"))
	require.NoError(t, err, "Exists probe must not fail")
	assert.True(t, uniqueBPresent,
		"GC live-set: a blob exclusively referenced by a surviving ref must survive")
}

func TestCollector_IsIdempotentAcrossReruns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, store, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	store.put(t, "objects/"+hashHex("ORPHAN"), []byte("ORPHAN"))

	collector := refs.NewCollector(store.storage)
	require.NoError(t, collector.Collect(ctx),
		"first GC run must succeed and sweep the orphan")

	snapshotKeys := store.keys()
	require.NoError(t, collector.Collect(ctx),
		"second GC run must succeed on a store with no remaining orphans")
	assert.ElementsMatch(t, snapshotKeys, store.keys(),
		"GC is idempotent: running Collect a second time must not change the store — mark-sweep converges in one pass")
}

func TestCollector_IsNoOpOnEmptyStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)

	collector := refs.NewCollector(store.storage)
	err := collector.Collect(ctx)

	require.NoError(t, err,
		"GC over an empty store must succeed — live set is empty, blob list is empty, nothing to sweep; boundary of the mark-sweep algorithm")
	assert.Empty(t, store.keys(),
		"GC on empty store must leave empty store — no phantom writes")
}

func TestCollector_SweepsAllBlobsWhenOnlyRefReferencesNoObjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	ref := &domain.Ref{
		Timestamp:     "2026-04-22T10-00-00.000Z",
		RitualVersion: "2.1.0",
		Targets:       []string{"worlds/**"},
		Objects:       map[string]domain.Object{},
	}
	body, err := json.Marshal(ref)
	require.NoError(t, err, "test fixture: empty-objects ref must marshal")
	store.put(t, "refs/"+string(ref.Timestamp)+".json", body)
	orphanKey := "objects/" + hashHex("ORPHAN")
	store.put(t, orphanKey, []byte("ORPHAN"))

	collector := refs.NewCollector(store.storage)
	require.NoError(t, collector.Collect(ctx),
		"GC with a ref that references no objects must succeed — empty Objects is a valid shape even if rare")

	orphanPresent, err := store.storage.Exists(ctx, orphanKey)
	require.NoError(t, err, "Exists probe must not fail")
	assert.False(t, orphanPresent,
		"GC live-set union over a ref with empty Objects is empty — every existing blob is therefore an orphan and must be swept")
}

func TestCollector_SkipsMalformedRefsAndSweepsFromSurvivingRefsLiveSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newFSBundle(t)
	store.put(t, "refs/2026-04-22T09-00-00.000Z.json", []byte("}{ not json"))
	survivor := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, store, survivor, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	orphanKey := "objects/" + hashHex("ORPHAN")
	store.put(t, orphanKey, []byte("ORPHAN"))

	collector := refs.NewCollector(store.storage)
	require.NoError(t, collector.Collect(ctx),
		"§Retention and GC fail-continue: a malformed ref must not abort the sweep — the surviving refs' live set still defines reachable blobs")

	livePresent, err := store.storage.Exists(ctx, "objects/"+hashHex("AAAA"))
	require.NoError(t, err, "Exists probe must not fail")
	assert.True(t, livePresent,
		"fail-continue sweep: a blob referenced by any surviving (parseable) ref must be preserved even when another ref is malformed")

	orphanPresent, err := store.storage.Exists(ctx, orphanKey)
	require.NoError(t, err, "Exists probe must not fail")
	assert.False(t, orphanPresent,
		"fail-continue sweep: an orphan blob must still be swept when some refs are malformed — the sweep proceeds with the parseable refs' live set")
}

func TestCollector_ContinuesSweepWhenAnIndividualDeleteFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	inner := newFSBundle(t)
	store := newFaultyStorage(inner)
	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": []byte("AAAA"),
	})
	seedRemote(t, inner, ref, map[string][]byte{"worlds/level.dat": []byte("AAAA")})
	stuckOrphanKey := "objects/" + hashHex("STUCK")
	store.put(t, stuckOrphanKey, []byte("STUCK"))
	reapableOrphanKey := "objects/" + hashHex("REAPABLE")
	store.put(t, reapableOrphanKey, []byte("REAPABLE"))
	store.deleteFail[stuckOrphanKey] = errors.New("simulated R2 503 on DELETE")

	collector := refs.NewCollector(store)
	require.NoError(t, collector.Collect(ctx),
		"§Retention and GC line 2842 'Delete failures → fail-continue': one delete error must not surface as Collect's error or halt the sweep")

	stuckPresent, err := store.Exists(ctx, stuckOrphanKey)
	require.NoError(t, err, "Exists probe must not fail")
	assert.True(t, stuckPresent,
		"fail-continue delete: the blob whose delete failed must remain — it survives as an orphan and retries on the next GC cycle; blobs are content-addressed and harmless")

	reapablePresent, err := store.Exists(ctx, reapableOrphanKey)
	require.NoError(t, err, "Exists probe must not fail")
	assert.False(t, reapablePresent,
		"fail-continue delete: orphans after the failing key must still be swept — one flaky delete cannot block the rest of the sweep")
}
