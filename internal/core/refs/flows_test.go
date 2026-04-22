// Package refs_test — §Flows to Check translation:
//
// Integration-level scenarios from §Flows to Check in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md, translated
// into cross-verb tests. Each flow composes the four verbs against the
// same shared-package memStorage fake.
//
// Host model (mirrors §On-disk / on-R2 Layout):
//
//   remote  — remote R2 equivalent; holds refs/{id}.json + objects/{hash}
//   local   — local user-data root's blob cache; holds refs/{id}.json + objects/{hash}
//   workdir — instance tree; holds root-relative paths like `worlds/level.dat`
//
// Wiring per verb:
//
//   Puller:     NewPuller(remote, local)    — download ref + blobs
//   Applier:    NewApplier(local, workdir)  — materialise workdir from local blobs
//   Committer:  NewCommitter(workdir, local) — snapshot workdir into local blobs
//   Pusher:     NewPusher(local, remote)    — upload local ref + blobs
//
// Deferred flows (not translated — require subsystems not yet implemented):
//
//   F-5 Crash mid-push recovery — requires failure-injecting storage; the
//       shape is covered by TestPusher_ResumesAfterBlobsUploadedButRefMissing
//       + TestPusher_IsIdempotentAcrossReruns.
//   F-6 Amend + failed push   — requires failure-injecting storage and a
//       pushed-flag check that lives in the session-lock subsystem.
//   F-7 Two-host race         — requires the session-lock port.
package refs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlow_F1_ColdStartRemotePrePopulated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()
	workdir := newMemStorage()

	originals := map[string][]byte{
		"worlds/level.dat": []byte("LEVEL"),
		"server/server.properties": []byte("motd=spec"),
	}
	originalID := prepareRemoteRef(t, remote,
		domain.RefID("2026-04-22T10-00-00.000Z"),
		[]string{"worlds/**", "server/**"},
		originals,
	)

	puller := refs.NewPuller(remote, local)
	applier := refs.NewApplier(local, workdir)
	require.NoError(t, puller.Pull(ctx, originalID),
		"F-1 step pull: remote pre-populated ref must arrive at local")
	require.NoError(t, applier.Apply(ctx, originalID),
		"F-1 step apply: local ref must materialise into an empty workdir")

	for path, expected := range originals {
		assert.Equal(t, expected, workdir.mustGet(t, path),
			"F-1 post-apply: workdir file %q must match remote source byte-for-byte", path)
	}

	workdir.put("worlds/level.dat", []byte("LEVEL_v2"))

	committer := refs.NewCommitter(workdir, local).
		WithClock(fixedClock(t, "2026-04-22T11-00-00.000Z"))
	pusher := refs.NewPusher(local, remote)

	newID, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  originalID,
		Targets: []string{"worlds/**", "server/**"},
	})
	require.NoError(t, err, "F-1 step commit: a new draft must mint from the mutated workdir")
	require.NoError(t, pusher.Push(ctx, newID),
		"F-1 step push: the new draft must land on remote — establishing the new HEAD")

	secondHostLocal := newMemStorage()
	secondHostWorkdir := newMemStorage()
	secondPuller := refs.NewPuller(remote, secondHostLocal)
	secondApplier := refs.NewApplier(secondHostLocal, secondHostWorkdir)
	require.NoError(t, secondPuller.Pull(ctx, newID),
		"F-1 verify: a second host pulling the new HEAD must succeed against the pushed remote state")
	require.NoError(t, secondApplier.Apply(ctx, newID),
		"F-1 verify: second host apply must succeed against its freshly pulled blobs")
	assert.Equal(t, []byte("LEVEL_v2"), secondHostWorkdir.mustGet(t, "worlds/level.dat"),
		"F-1 verify: second host workdir must reproduce the publisher's bytes exactly — end-to-end round trip consistency")
	assert.Equal(t, []byte("motd=spec"), secondHostWorkdir.mustGet(t, "server/server.properties"),
		"F-1 verify: unchanged files must also reproduce on the second host — full-tree consistency")
}

func TestFlow_F2_LiveTickerWithAmend(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()
	workdir := newMemStorage()

	workdir.put("worlds/level.dat", []byte("tick0"))

	clock := advancingClock(t, "2026-04-22T10-00-00.000Z", time.Minute)
	committer := refs.NewCommitter(workdir, local).WithClock(clock)
	pusher := refs.NewPusher(local, remote)

	tick1, err := committer.Commit(ctx, ports.CommitOpts{Targets: []string{"worlds/**"}})
	require.NoError(t, err, "F-2 tick 1: initial commit of live-ticker session")
	require.NoError(t, pusher.Push(ctx, tick1),
		"F-2 tick 1: first push completes the initial session linearization")

	workdir.put("worlds/level.dat", []byte("tick2"))
	tick2, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  tick1,
		Amend:   tick1,
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "F-2 tick 2: amend of the session draft must mint a new ref")

	assertRefAbsent(t, local, tick1,
		"F-2 §Commit Amend step 5: the superseded local draft must be deleted after the new draft is written — no chain lengthening on disk")

	workdir.put("worlds/level.dat", []byte("tick3"))
	tick3, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  tick2,
		Amend:   tick2,
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "F-2 tick 3: further amend continues the draft chain collapse")
	require.NoError(t, pusher.Push(ctx, tick3),
		"F-2 tick 3: push the amended draft — only the final tick leaves local disk")

	assertRefAbsent(t, local, tick2,
		"F-2 amend-collapse: intermediate draft tick2 must be gone from local after the next amend replaces it")
	assert.True(t, refExists(remote, tick3),
		"F-2 §Push step 5: the final amended ref must be the one that reaches remote")
	assertRefAbsent(t, remote, tick2,
		"F-2 §Commit Amend isolation: an amended draft must never reach remote — intermediate-tick blobs + ref stay local")
}

func TestFlow_F3_RestoreToPastTimestamp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()
	workdir := newMemStorage()

	oldID := prepareRemoteRef(t, remote,
		domain.RefID("2026-04-22T10-00-00.000Z"),
		[]string{"worlds/**"},
		map[string][]byte{"worlds/level.dat": []byte("OLD")},
	)
	newID := prepareRemoteRef(t, remote,
		domain.RefID("2026-04-22T12-00-00.000Z"),
		[]string{"worlds/**"},
		map[string][]byte{"worlds/level.dat": []byte("NEW")},
	)

	puller := refs.NewPuller(remote, local)
	applier := refs.NewApplier(local, workdir)
	require.NoError(t, puller.Pull(ctx, newID),
		"F-3 preparatory pull of current HEAD to seed local cache")
	require.NoError(t, applier.Apply(ctx, newID),
		"F-3 preparatory apply of current HEAD")
	require.Equal(t, []byte("NEW"), workdir.mustGet(t, "worlds/level.dat"),
		"F-3 fixture: workdir begins on the NEW snapshot before the operator restores")

	require.NoError(t, puller.Pull(ctx, oldID),
		"F-3 restore step: pulling a past timestamp must hydrate its blobs independently of HEAD")
	require.NoError(t, applier.Apply(ctx, oldID),
		"F-3 restore step: applying the old ref must rewrite the workdir to that snapshot")
	assert.Equal(t, []byte("OLD"), workdir.mustGet(t, "worlds/level.dat"),
		"F-3 post-restore: workdir's tracked files must byte-match the chosen past ref — §Apply consistency postcondition")
}

func TestFlow_F4_PatchTargetEditWithoutDataChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()

	headID := prepareRemoteRef(t, remote,
		domain.RefID("2026-04-22T10-00-00.000Z"),
		[]string{"worlds/**"},
		map[string][]byte{"worlds/level.dat": []byte("LEVEL")},
	)

	puller := refs.NewPuller(remote, local)
	require.NoError(t, puller.Pull(ctx, headID),
		"F-4 preparatory pull to hydrate the existing HEAD locally")

	patched := cloneAndPatchRef(t, local, headID,
		domain.RefID("2026-04-22T11-00-00.000Z"),
		func(r *domain.Ref) { r.Targets = append(r.Targets, "server/forge/**") },
	)

	pusher := refs.NewPusher(local, remote)
	require.NoError(t, pusher.Push(ctx, patched),
		"F-4 step push: patched ref must upload with no new blobs — only the manifest changed")

	pushed, ok := remote.decodeRef(t, patched)
	require.True(t, ok, "F-4: patched ref must reach remote under its new timestamp")
	assert.Equal(t, []string{"worlds/**", "server/forge/**"}, pushed.Targets,
		"F-4: pushed ref must carry the expanded targets list — scope edits travel with history")
	assert.Equal(t, 1, len(pushed.Objects),
		"F-4: patch must not alter the object set — data content is unchanged across a target-only edit")
}

func TestFlow_F8_InitOnEmptyRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	remote := newMemStorage()
	local := newMemStorage()
	workdir := newMemStorage()

	workdir.put("worlds/level.dat", []byte("fresh"))

	committer := refs.NewCommitter(workdir, local).
		WithClock(fixedClock(t, "2026-04-22T10-00-00.000Z"))
	pusher := refs.NewPusher(local, remote)

	initID, err := committer.Commit(ctx, ports.CommitOpts{
		Parent:  "",
		Targets: []string{"worlds/**"},
	})
	require.NoError(t, err, "F-8 init commit: a ritual with no parent must produce a valid first ref")
	require.NoError(t, pusher.Push(ctx, initID),
		"F-8 init push: first push to an empty remote must succeed")

	initRef, ok := remote.decodeRef(t, initID)
	require.True(t, ok, "F-8: init ref must land on the empty remote")
	assert.Equal(t, domain.RefID(""), initRef.Parent,
		"F-8: init ref's Parent must be empty — there is no prior HEAD on an empty remote")
	assert.Equal(t, []byte("fresh"), remote.mustGet(t, "objects/"+hashHex("fresh")),
		"F-8: init push must upload every referenced blob to the previously empty remote")
}

// --- flow fixtures (prefix-named to avoid collision) ---

func prepareRemoteRef(t *testing.T, remote *memStorage, id domain.RefID, targets []string, files map[string][]byte) domain.RefID {
	t.Helper()
	objects := map[string]domain.Object{}
	for path, data := range files {
		hash := hashHex(string(data))
		objects[path] = domain.Object{Hash: hash, Size: int64(len(data))}
		remote.put("objects/"+hash, data)
	}
	ref := &domain.Ref{
		Timestamp:     id,
		RitualVersion: "2.1.0",
		Targets:       targets,
		Objects:       objects,
	}
	body, err := json.Marshal(ref)
	require.NoError(t, err, "flow fixture: ref must marshal to JSON")
	remote.put("refs/"+string(id)+".json", body)
	return id
}

func cloneAndPatchRef(t *testing.T, local *memStorage, sourceID, targetID domain.RefID, patch func(*domain.Ref)) domain.RefID {
	t.Helper()
	rc, err := local.GetStream(context.Background(), "refs/"+string(sourceID)+".json")
	require.NoError(t, err, "flow fixture: source ref must exist locally before cloning")
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	require.NoError(t, err, "flow fixture: source ref bytes must be readable")
	ref := &domain.Ref{}
	require.NoError(t, json.Unmarshal(raw, ref),
		"flow fixture: source ref must decode as domain.Ref for a patch clone")
	ref.Timestamp = targetID
	patch(ref)
	body, err := json.Marshal(ref)
	require.NoError(t, err, "flow fixture: patched ref must marshal")
	local.put("refs/"+string(targetID)+".json", body)
	return targetID
}

func fixedClock(t *testing.T, iso string) func() time.Time {
	t.Helper()
	parsed, err := time.Parse(domain.RefIDFormat, iso)
	require.NoError(t, err, "flow fixture: fixed clock timestamp %q must parse as RefIDFormat", iso)
	return func() time.Time { return parsed }
}

func advancingClock(t *testing.T, startISO string, step time.Duration) func() time.Time {
	t.Helper()
	current, err := time.Parse(domain.RefIDFormat, startISO)
	require.NoError(t, err, "flow fixture: advancing clock start %q must parse as RefIDFormat", startISO)
	return func() time.Time {
		tick := current
		current = current.Add(step)
		return tick
	}
}

func assertRefAbsent(t *testing.T, store *memStorage, id domain.RefID, msg string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	_, exists := store.items["refs/"+string(id)+".json"]
	assert.False(t, exists, msg)
}

func refExists(store *memStorage, id domain.RefID) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	_, ok := store.items["refs/"+string(id)+".json"]
	return ok
}

// avoid "bytes" unused if no other ref
var _ = bytes.NewReader
