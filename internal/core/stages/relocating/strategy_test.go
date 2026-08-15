package relocating_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/relocating"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sentinelStrategy struct {
	name   string
	called bool
}

func (s *sentinelStrategy) Name() string { return s.name }
func (s *sentinelStrategy) Run(_ context.Context, _ *relocating.State) (machine.Strategy[relocating.State], error) {
	s.called = true
	return nil, nil
}

func newSourceRefs(t *testing.T) relocating.WorkRootRefs {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	local, err := adapters.NewFSRepository(root, "local")
	require.NoError(t, err)
	workdir, err := adapters.NewFSRepository(root, "workdir")
	require.NoError(t, err)

	rootRef := new(atomic.Pointer[os.Root])
	rootRef.Store(root)
	localRef := adapters.NewSwappableStorage()
	localRef.Store(local)
	workdirRef := adapters.NewSwappableStorage()
	workdirRef.Store(workdir)

	return relocating.WorkRootRefs{Root: rootRef, Local: localRef, Workdir: workdirRef}
}

// writeObject writes content under objects/<hash> through a real
// CompressingStorage encoder so the on-disk bytes are genuinely
// zstd-compressed and keyed by the correct xxhash — matching production
// exactly (internal/adapters/compressing.go), so Strategy.Run's verify step
// (which re-decodes+re-hashes at the destination) accepts it.
func writeObject(t *testing.T, refs relocating.WorkRootRefs, content []byte) string {
	t.Helper()
	key := fmt.Sprintf("objects/%016x", xxhash.Sum64(content))
	compressed, err := adapters.NewCompressingStorage(refs.Local.Current())
	require.NoError(t, err)
	require.NoError(t, compressed.PutStream(context.Background(), key, bytes.NewReader(content)))
	return key
}

func writeRef(t *testing.T, refs relocating.WorkRootRefs, id string) string {
	t.Helper()
	ref := domain.Ref{Timestamp: domain.RefID(id), RitualVersion: "test", Objects: map[string]domain.Object{}}
	raw, err := json.Marshal(ref)
	require.NoError(t, err)
	key := "refs/" + id + ".json"
	require.NoError(t, refs.Local.Current().PutStream(context.Background(), key, bytes.NewReader(raw)))
	return key
}

func writeWorkdirFile(t *testing.T, refs relocating.WorkRootRefs, key string, content []byte) string {
	t.Helper()
	require.NoError(t, refs.Workdir.Current().PutStream(context.Background(), key, bytes.NewReader(content)))
	return key
}

func withScratchRootPath(t *testing.T) {
	t.Helper()
	original := config.RootPath
	config.RootPath = t.TempDir()
	t.Cleanup(func() { config.RootPath = original })
}

func withScratchWorkRoot(t *testing.T) {
	t.Helper()
	original := config.WorkRoot
	t.Cleanup(func() { config.WorkRoot = original })
}

func newState(dst string, refs relocating.WorkRootRefs, bus ports.EventBus) *relocating.State {
	return &relocating.State{
		Dst:      dst,
		Refs:     refs,
		Settings: &domain.Settings{Port: 25565, Memory: 4096, MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1},
		Bus:      bus,
	}
}

func TestRelocating_SuccessfulMove_ContentLandsAtDestinationAndOldRootIsRemoved(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	oldDir := refs.Root.Load().Name()
	objKey := writeObject(t, refs, []byte("blob content"))
	refKey := writeRef(t, refs, "2026-08-10T00-00-00.000Z")
	writeWorkdirFile(t, refs, "server/server.properties", []byte("port=25565"))
	writeWorkdirFile(t, refs, "worlds/level.dat", []byte("world data"))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	next, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.NoError(t, err, "a successful relocate over valid, verifiable content must not return an error")
	assert.Nil(t, next, "with onOK=nil the strategy must return nil next on success, per machine.Drive's termination contract")

	assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(objKey)), "the object blob must land at the destination byte-for-byte")
	assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(refKey)), "the ref JSON must land at the destination")
	assert.FileExists(t, filepath.Join(dst, "server", "server.properties"))
	assert.FileExists(t, filepath.Join(dst, "worlds", "level.dat"))

	assert.Equal(t, dst, state.Settings.WorkRoot, "commit must set settings.WorkRoot to the destination")
	assert.Equal(t, dst, config.WorkRoot, "commit must update the in-process config.WorkRoot so live-rederiving readers observe the change without a restart")

	onDisk, err := os.ReadFile(domain.SettingsPath())
	require.NoError(t, err, "commit must durably persist settings.json via the now-atomic Settings.Save()")
	var persisted domain.Settings
	require.NoError(t, json.Unmarshal(onDisk, &persisted), "settings.json must stay valid JSON")
	assert.Equal(t, dst, persisted.WorkRoot, "the persisted settings.json must contain the new work_root")

	for _, dir := range []string{"objects", "refs", "server", "worlds"} {
		_, statErr := os.Stat(filepath.Join(oldDir, dir))
		assert.True(t, os.IsNotExist(statErr), "cleanup must remove the old root's %s subdirectory after a successful commit — stale content files are explicitly not left behind on the success path", dir)
	}
}

// TestRelocating_FirstRelocateFromDefaultRoot_PreservesControlFiles
// regression-guards the most severe live repro of the session (2026-08-11):
// on a never-relocated install, config.WorkRoot defaults to config.RootPath
// — the CONTENT root and the CONTROL root (settings.json/lock/logs/) are the
// SAME directory. cleanup() used to os.RemoveAll(oldDir) wholesale, which on
// a real machine deleted the settings.json commit() had just durably
// written moments earlier (plus the lock file and older logs) — the
// relocated data landed safely at the new destination, but the pointer to
// it, and the whole control root, was wiped. Fixed by scoping cleanup to
// exactly the CONTENT subdirectories (contentDirs) instead of the directory
// itself; this test sets up the exact old-root-equals-RootPath topology the
// existing newSourceRefs-based tests structurally cannot reach (they always
// use a source root distinct from config.RootPath) and asserts settings.json
// and lock survive with the NEW work_root, not a regenerated default.
func TestRelocating_FirstRelocateFromDefaultRoot_PreservesControlFiles(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	// The old content root IS config.RootPath — exactly the default,
	// never-relocated topology every real install starts from.
	root, err := os.OpenRoot(config.RootPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	local, err := adapters.NewFSRepository(root, "local")
	require.NoError(t, err)
	workdir, err := adapters.NewFSRepository(root, "workdir")
	require.NoError(t, err)
	rootRef := new(atomic.Pointer[os.Root])
	rootRef.Store(root)
	localRef := adapters.NewSwappableStorage()
	localRef.Store(local)
	workdirRef := adapters.NewSwappableStorage()
	workdirRef.Store(workdir)
	refs := relocating.WorkRootRefs{Root: rootRef, Local: localRef, Workdir: workdirRef}

	require.NoError(t, domain.DefaultSettings().Save(), "a real install always has settings.json at RootPath before any relocate")
	lockPath := filepath.Join(config.RootPath, "lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("held"), 0o600))
	writeObject(t, refs, []byte("blob content"))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err = relocating.New(nil, nil).Run(context.Background(), state)
	require.NoError(t, err)

	assert.FileExists(t, lockPath, "the lock file at RootPath must survive a relocate whose old content root happened to be RootPath itself")
	onDisk, err := os.ReadFile(domain.SettingsPath())
	require.NoError(t, err, "settings.json at RootPath must survive — cleanup must never delete the CONTROL root's own files")
	var persisted domain.Settings
	require.NoError(t, json.Unmarshal(onDisk, &persisted))
	assert.Equal(t, dst, persisted.WorkRoot, "settings.json must reflect the successful relocate's new work_root, not fall back to a regenerated default")

	_, statErr := os.Stat(filepath.Join(config.RootPath, "objects"))
	assert.True(t, os.IsNotExist(statErr), "the old objects/ subdirectory must still be cleaned up, same as any other relocate")
}

// TestRelocating_EmptyObjectBlob_PassesVerify regression-guards a real dev
// repro (2026-08-11): a genuine zero-byte content-addressed object
// (xxhash64("") == "ef46db3751d8e999", a real, correctly-hashed key — not
// corruption) failed relocate's verify step because it reused the
// non-content-addressed server/worlds/ "reject 0 bytes" check on objects/
// too. objects/ integrity is already guaranteed by the zstd-decode +
// xxhash-vs-filename check on Close (see the corrupted-blob test below); an
// empty result is a legitimate outcome for objects/, not a failure signal.
func TestRelocating_EmptyObjectBlob_PassesVerify(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	objKey := writeObject(t, refs, []byte{})

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.NoError(t, err, "a genuinely empty, correctly-hashed object must pass verify, not be mistaken for a failed copy")
	assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(objKey)), "the empty object blob must still land at the destination")
}

// TestRelocating_EmptyWorldsFile_PassesVerify regression-guards a second
// live repro on the same session (2026-08-11), right after the objects/ fix
// above: a real Paper world save contains dozens of genuinely 0-byte `.mca`
// region files (lazily-allocated POI/entity/region files for
// sparsely-generated areas, e.g. the Nether) — verify's server/worlds/ check
// rejected the first one it saw (worlds/world/DIM-1/poi/r.-2.-2.mca) as
// "empty file", even though the copy itself succeeded byte-for-byte. Fixed
// by dropping the server/worlds/ verify pass entirely (redundant with
// copyContent's own error surfacing — see verify's doc comment), not by
// softening its empty-check, so this now guards the whole class of case.
func TestRelocating_EmptyWorldsFile_PassesVerify(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	key := writeWorkdirFile(t, refs, "worlds/world/DIM-1/poi/r.-2.-2.mca", []byte{})

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.NoError(t, err, "a genuinely empty, lazily-allocated .mca region file must pass verify, not be mistaken for a failed copy")
	assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(key)), "the empty worlds file must still land at the destination")
}

func TestRelocating_CorruptedBlobDuringCopy_AbortsBeforeStoreSourceIntact(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	oldRoot := refs.Root.Load()
	oldLocal := refs.Local.Current()

	// Write a blob whose key does NOT match its content's real hash —
	// copyContent streams raw bytes verbatim (no validation), so this
	// corruption is only caught by verify()'s zstd-decode + xxhash check at
	// the destination, exactly like design-log/025's existing corrupted-
	// write detection for a normal push.
	compressed, err := adapters.NewCompressingStorage(refs.Local.Current())
	require.NoError(t, err)
	badKey := "objects/0000000000000000"
	require.NoError(t, compressed.PutStream(context.Background(), badKey, bytes.NewReader([]byte("mismatched content"))))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err = relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "a corrupted blob must fail verification and abort the relocate before any swap")

	assert.Same(t, oldRoot, refs.Root.Load(), "Refs.Root must still point at the original root — verify failed before the Store moment")
	assert.Same(t, oldLocal, refs.Local.Current(), "Refs.Local must still point at the original facade")
	assert.Empty(t, state.Settings.WorkRoot, "commit must never run when verify fails, so settings.WorkRoot must remain untouched")
	assert.Equal(t, "", config.WorkRoot, "config.WorkRoot must remain untouched when verify fails before the commit step")

	rc, getErr := oldLocal.GetStream(context.Background(), badKey)
	require.NoError(t, getErr, "the source object must remain present and readable — a failed relocate must never mutate the source")
	_ = rc.Close()
}

func TestRelocating_VerifyFailure_AbortsBeforeStoreSourceIntact(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	oldRoot := refs.Root.Load()

	// An empty ref JSON body fails verifyRefParses' json.Unmarshal, a
	// distinct verify() failure mode from the corrupted-blob case above.
	require.NoError(t, refs.Local.Current().PutStream(context.Background(), "refs/broken.json", bytes.NewReader([]byte("not json"))))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "an unparseable ref must fail verify and abort before any swap")
	assert.Same(t, oldRoot, refs.Root.Load(), "Refs.Root must be untouched when verify fails")
}

func TestRelocating_CommitFailure_RollsBackRefsToOldTargets(t *testing.T) {
	withScratchWorkRoot(t)
	// Point config.RootPath at a location whose settings dir cannot be
	// created, so domain.Settings.Save() (called from commit(), after
	// copy+verify already succeeded and Refs.store(new) already ran) fails
	// deterministically. Nothing else in Run touches config.RootPath.
	original := config.RootPath
	config.RootPath = filepath.Join(t.TempDir(), "does-not-exist")
	t.Cleanup(func() { config.RootPath = original })

	refs := newSourceRefs(t)
	oldRoot := refs.Root.Load()
	oldLocal := refs.Local.Current()
	oldWorkdir := refs.Workdir.Current()
	writeObject(t, refs, []byte("blob content"))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "a failing settings.Save() inside commit must surface as an error")

	assert.Same(t, oldRoot, refs.Root.Load(), "a commit failure must roll Refs.Root back to the pre-swap target — the one rollback window design-log/055 calls out")
	assert.Same(t, oldLocal, refs.Local.Current(), "a commit failure must roll Refs.Local back")
	assert.Same(t, oldWorkdir, refs.Workdir.Current(), "a commit failure must roll Refs.Workdir back")
}

func TestRelocating_DestinationNotWritable_RejectedByValidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not reliably restrict directory write access on Windows")
	}
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	readonlyParent := t.TempDir()
	require.NoError(t, os.Chmod(readonlyParent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonlyParent, 0o755) })

	dst := filepath.Join(readonlyParent, "content")
	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "a destination under a read-only parent must be rejected by validate before anything is copied")
	assert.ErrorIs(t, err, relocating.ErrDestNotWritable)
}

func TestRelocating_PermissionDeniedOnDestination_SurfacedAsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not reliably restrict directory write access on Windows")
	}
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	dst := t.TempDir()
	require.NoError(t, os.Chmod(dst, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dst, 0o755) })

	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "an existing, non-writable destination must be surfaced as an error, not silently ignored")
}

func TestRelocating_DestinationEqualsCurrentRoot_RejectedByValidate(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	dst := refs.Root.Load().Name()

	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err)
	assert.ErrorIs(t, err, relocating.ErrDestIsCurrentRoot)
}

func TestRelocating_DestinationInsideCurrentRoot_RejectedByValidate(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	dst := filepath.Join(refs.Root.Load().Name(), "nested")

	bus := adapters.NewEventBus(64)
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err)
	assert.ErrorIs(t, err, relocating.ErrDestInsideCurrent)
}

func TestRelocating_DestinationIsSymlinkToCurrentRoot_RejectedByValidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows by default")
	}
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	linkPath := filepath.Join(t.TempDir(), "alias-for-current-root")
	require.NoError(t, os.Symlink(refs.Root.Load().Name(), linkPath), "seed a symlink that resolves to the same physical directory as the current root")

	bus := adapters.NewEventBus(64)
	state := newState(linkPath, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "a destination that resolves to the same physical directory as the current root via a symlink must be rejected — otherwise copyKey's GetStream(src)-then-PutStream(dst, O_TRUNC) on what turns out to be the same underlying file would truncate the source mid-read")
	assert.ErrorIs(t, err, relocating.ErrDestIsCurrentRoot)
}

func TestRelocating_PublishesStartPlanUpdateFinishEventsOnBus(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	writeObject(t, refs, []byte("blob content"))

	dst := filepath.Join(t.TempDir(), "content")
	bus := adapters.NewEventBus(64)
	sub, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	state := newState(dst, refs, bus)
	onOK := &sentinelStrategy{name: "ok"}
	next, err := relocating.New(onOK, nil).Run(context.Background(), state)
	require.NoError(t, err)
	assert.Same(t, machine.Strategy[relocating.State](onOK), next, "Run must route to onOK on success")

	var sawStart, sawPlan, sawFinish bool
	deadline := time.After(time.Second)
collect:
	for {
		select {
		case e := <-sub:
			// relocate publishes its own dedicated event vocabulary
			// (events.go), not the generic ritual.StartInfo/PlanInfo/
			// UpdateInfo/FinishInfo pull/push use — internal/gui/projection's
			// fold() switches ritual.PlanInfo by concrete type alone with no
			// Operation check, so a shared PlanInfo would land straight on
			// the session ViewModel (design-log/055 addendum). Dedicated
			// types can't collide with the session's, by construction.
			switch e.(type) {
			case relocating.RelocateStarted:
				sawStart = true
			case relocating.RelocatePlanned:
				sawPlan = true
			case relocating.RelocateFinished:
				sawFinish = true
			}
			if sawStart && sawPlan && sawFinish {
				break collect
			}
		case <-deadline:
			break collect
		}
	}

	assert.True(t, sawStart, "Run must publish RelocateStarted")
	assert.True(t, sawPlan, "Run must publish RelocatePlanned before copying")
	assert.True(t, sawFinish, "Run must publish RelocateFinished on success")
}

func TestRelocating_CancelMidCopy_LeavesOldRootActiveNewRootDiscarded(t *testing.T) {
	withScratchRootPath(t)
	withScratchWorkRoot(t)

	refs := newSourceRefs(t)
	oldRoot := refs.Root.Load()
	writeObject(t, refs, []byte("first blob"))
	writeObject(t, refs, []byte("second blob, different content"))

	bus := adapters.NewBlockingEventBus(4)
	stopper := &stopAfterNGets{StorageRepository: refs.Local.Current(), bus: bus, n: 1}
	refs.Local.Store(stopper)

	dst := filepath.Join(t.TempDir(), "content")
	state := newState(dst, refs, bus)

	_, err := relocating.New(nil, nil).Run(context.Background(), state)
	require.Error(t, err, "a StopRequested event mid-copy must abort the relocate")

	assert.Same(t, oldRoot, refs.Root.Load(), "a cancelled copy must never reach the Store moment — the old root stays active")
	assert.Empty(t, state.Settings.WorkRoot, "a cancelled copy must never reach commit")
}

// stopAfterNGets publishes a ritual.StopRequested event after the Nth
// GetStream call, then yields briefly so watchStop's subscriber goroutine
// has a chance to observe it and cancel stopCtx before the next file's
// ctx.Err() check runs.
type stopAfterNGets struct {
	ports.StorageRepository
	bus   ports.EventBus
	n     int32
	count atomic.Int32
}

func (s *stopAfterNGets) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := s.StorageRepository.GetStream(ctx, key)
	if s.count.Add(1) == s.n {
		s.bus.Publish(ritual.StopRequested{})
		time.Sleep(50 * time.Millisecond)
	}
	return rc, err
}
