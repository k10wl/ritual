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
	assert.Contains(t, string(onDisk), dst, "the persisted settings.json must contain the new work_root")

	_, statErr := os.Stat(oldDir)
	assert.True(t, os.IsNotExist(statErr), "cleanup must remove the old root directory after a successful commit — stale files are explicitly not left behind on the success path")
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
