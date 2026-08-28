package control_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/relocating"
	"ritual/internal/gui/control"
	"ritual/internal/gui/projection"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSnapshotSource struct {
	phase projection.Phase
}

func (f fakeSnapshotSource) Snapshot() projection.ViewModel {
	return projection.ViewModel{Phase: f.phase}
}

func newWorkRootRefs(t *testing.T) relocating.WorkRootRefs {
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

func withScratchRoots(t *testing.T, workRoot string) {
	t.Helper()
	originalRootPath := config.RootPath
	originalWorkRoot := config.WorkRoot
	config.RootPath = t.TempDir()
	config.WorkRoot = workRoot
	t.Cleanup(func() {
		config.RootPath = originalRootPath
		config.WorkRoot = originalWorkRoot
	})
}

func TestControlService_GetWorkRoot_DefaultReportsIsDefaultTrue(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	// withScratchRoots already set config.WorkRoot = the *old* RootPath
	// value before RootPath itself changed — pin WorkRoot to the NEW
	// RootPath explicitly so the two match, exercising the "default" case.
	config.WorkRoot = config.RootPath

	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	info := svc.GetWorkRoot()
	assert.Equal(t, config.RootPath, info.Path)
	assert.True(t, info.IsDefault, "WorkRoot == RootPath must report IsDefault true")
}

func TestControlService_GetWorkRoot_ExplicitPathReportsIsDefaultFalse(t *testing.T) {
	withScratchRoots(t, filepath.Join(t.TempDir(), "content"))

	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	info := svc.GetWorkRoot()
	assert.Equal(t, config.WorkRoot, info.Path)
	assert.False(t, info.IsDefault, "an explicit work root different from RootPath must report IsDefault false")
}

func TestControlService_ChangeWorkRoot_WhileRunning_RejectedNoStateChanged(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	refs := newWorkRootRefs(t)
	originalRoot := refs.Root.Load()

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhasePlaying}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	err := svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content"))
	require.ErrorIs(t, err, control.ErrSessionRunning, "ChangeWorkRoot must reject while a session is RUNNING — a live sync/server could be writing worlds/objects mid-copy")
	assert.Same(t, originalRoot, refs.Root.Load(), "a rejected ChangeWorkRoot must leave the storage refs untouched")
}

func TestControlService_ChangeWorkRoot_Success_UpdatesConfigWorkRootAndSettings(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	require.NoError(t, domain.DefaultSettings().Save(), "seed a settings.json so ChangeWorkRoot's internal domain.LoadSettings succeeds")
	refs := newWorkRootRefs(t)
	// A successful ChangeWorkRoot swaps refs.Root to a NEW os.Root opened on
	// dst (a t.TempDir() subdirectory) — newWorkRootRefs' own t.Cleanup only
	// closes the ORIGINAL root it captured, so the swapped-in root is never
	// closed and Windows' t.TempDir() cleanup fails to remove dst ("used by
	// another process"). A plain defer runs before any t.Cleanup-registered
	// TempDir removal, so close whatever refs.Root currently holds.
	defer func() { _ = refs.Root.Load().Close() }()

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseIdle}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	dst := filepath.Join(t.TempDir(), "content")
	require.NoError(t, svc.ChangeWorkRoot(dst))

	assert.Equal(t, dst, config.WorkRoot, "a successful ChangeWorkRoot must update the in-process config.WorkRoot")
	onDisk, err := domain.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, dst, onDisk.WorkRoot, "a successful ChangeWorkRoot must durably persist the new work_root")
}

func TestControlService_ChangeWorkRoot_WhileDownloading_RejectedNoStateChanged(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	refs := newWorkRootRefs(t)
	originalRoot := refs.Root.Load()

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseDownloading}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	err := svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content"))
	require.ErrorIs(t, err, control.ErrSessionRunning, "the gate must reject any active pipeline phase, not only PhasePlaying — a Pull/Apply can be actively touching CONTENT storage during Downloading too")
	assert.Same(t, originalRoot, refs.Root.Load())
}

func TestControlService_ChangeWorkRoot_WhileFailed_Allowed(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	require.NoError(t, domain.DefaultSettings().Save())
	refs := newWorkRootRefs(t)
	defer func() { _ = refs.Root.Load().Close() }()

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseFailed}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	err := svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content"))
	assert.NoError(t, err, "PhaseFailed means the pipeline has stopped with no ongoing work — a relocate must still be allowed so the user can recover")
}

type blockingGetStream struct {
	ports.StorageRepository
	entered chan struct{}
	once    sync.Once
	release chan struct{}
}

func (b *blockingGetStream) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.StorageRepository.GetStream(ctx, key)
}

func putValidObject(t *testing.T, refs relocating.WorkRootRefs, content []byte) {
	t.Helper()
	compressed, err := adapters.NewCompressingStorage(refs.Local.Current())
	require.NoError(t, err)
	require.NoError(t, compressed.PutStream(context.Background(), fmt.Sprintf("objects/%016x", xxhash.Sum64(content)), bytes.NewReader(content)))
}

func TestControlService_ChangeWorkRoot_ConcurrentCalls_SecondRejected(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	require.NoError(t, domain.DefaultSettings().Save())
	refs := newWorkRootRefs(t)
	defer func() { _ = refs.Root.Load().Close() }()
	putValidObject(t, refs, []byte("blob content"))

	release := make(chan struct{})
	entered := make(chan struct{})
	refs.Local.Store(&blockingGetStream{StorageRepository: refs.Local.Current(), entered: entered, release: release})

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseIdle}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content-a"))
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first ChangeWorkRoot call never reached the blocked GetStream — the test's synchronization point was never hit")
	}

	err := svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content-b"))
	require.ErrorIs(t, err, control.ErrRelocateInProgress, "a second ChangeWorkRoot call while the first is still mid-copy must be rejected immediately, not left to race over the same WorkRootRefs")

	close(release)
	require.NoError(t, <-firstErrCh, "the first call, once unblocked, must still run to completion successfully")
}

func TestControlService_Start_WhileChangeWorkRootInFlight_Rejected(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	require.NoError(t, domain.DefaultSettings().Save())
	refs := newWorkRootRefs(t)
	defer func() { _ = refs.Root.Load().Close() }()
	putValidObject(t, refs, []byte("blob content"))

	release := make(chan struct{})
	entered := make(chan struct{})
	refs.Local.Store(&blockingGetStream{StorageRepository: refs.Local.Current(), entered: entered, release: release})

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseIdle}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	relocateErrCh := make(chan error, 1)
	go func() {
		relocateErrCh <- svc.ChangeWorkRoot(filepath.Join(t.TempDir(), "content-a"))
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the relocate never reached the blocked GetStream")
	}

	err := svc.Start(25565, 4096, false)
	assert.Error(t, err, "Start must refuse to publish StartRequested while a relocate is still copying worlds/objects — starting a session mid-copy would let the game process write into a directory the relocate is concurrently reading")

	close(release)
	require.NoError(t, <-relocateErrCh)
}

func TestControlService_ResetWorkRoot_MovesBackToConfigRootPath(t *testing.T) {
	withScratchRoots(t, config.RootPath)
	require.NoError(t, (&domain.Settings{WorkRoot: filepath.Join(t.TempDir(), "elsewhere"), MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1, Port: 25565, Memory: 4096}).Save())
	refs := newWorkRootRefs(t)
	defer func() { _ = refs.Root.Load().Close() }()

	svc := control.NewControlService(nil, fakeSnapshotSource{phase: projection.PhaseIdle}, nil, nil, nil, nil)
	svc.SetWorkRoot(refs, adapters.NewSwappableCmdBuilder(), nil)

	require.NoError(t, svc.ResetWorkRoot())
	assert.Equal(t, config.RootPath, config.WorkRoot, "ResetWorkRoot must move the content root back to config.RootPath")
}

// OpenRootFolder itself is not covered by an automated test: it shells out
// to the OS file manager (exec.Command("open"/"explorer"/"xdg-open").Start())
// with no seam to intercept the process launch, so exercising it in `go
// test` would actually pop a real Finder/Explorer window as a side effect.
// The design-log/055 wiring fix (reveal config.WorkRoot instead of
// config.RootPath) is a one-line change, verified by reading control.go.

func TestControlService_PickWorkRootFolder_NilPickerDegrades(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	path, ok := svc.PickWorkRootFolder()
	assert.False(t, ok, "an unset directoryPicker must degrade to ok=false, never a panic or error")
	assert.Empty(t, path)
}

func TestControlService_PickWorkRootFolder_CancelDegrades(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	// Mirrors Wails v3's own macOS Cancel contract (design-log/056): the
	// dialog returns ("", nil) on Cancel, not an error.
	svc.SetDirectoryPicker(func(dir string) (string, error) { return "", nil })
	path, ok := svc.PickWorkRootFolder()
	assert.False(t, ok, "a cancelled pick (empty path, nil error) must degrade to ok=false")
	assert.Empty(t, path)
}

func TestControlService_PickWorkRootFolder_ErrorDegrades(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.SetDirectoryPicker(func(dir string) (string, error) { return "", errors.New("dialog failed") })
	path, ok := svc.PickWorkRootFolder()
	assert.False(t, ok, "a real dialog-open failure must degrade to ok=false, not be surfaced as an error the user can't act on")
	assert.Empty(t, path)
}

// TestControlService_PickWorkRootFolder_AppendsAppNameContainer guards the
// 2026-08-11 UX decision: the picker chooses a CONTAINER, not the exact
// content folder, so the returned path is always <picked>/<AppName> — never
// the raw OS selection verbatim. Dumping objects/refs/server/worlds
// straight into whatever folder a user happens to pick would mix Ritual's
// data in with anything else already there.
func TestControlService_PickWorkRootFolder_AppendsAppNameContainer(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	picked := filepath.Join(t.TempDir(), "Games")
	svc.SetDirectoryPicker(func(dir string) (string, error) { return picked, nil })
	path, ok := svc.PickWorkRootFolder()
	require.True(t, ok)
	assert.Equal(t, filepath.Join(picked, config.AppName), path, "the returned path must be the picked folder plus an AppName container subfolder")
}

func TestControlService_PickWorkRootFolder_SeedsDialogAtCurrentWorkRoot(t *testing.T) {
	withScratchRoots(t, filepath.Join(t.TempDir(), "current-content"))
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	var seenDir string
	svc.SetDirectoryPicker(func(dir string) (string, error) {
		seenDir = dir
		return "", nil
	})
	_, _ = svc.PickWorkRootFolder()
	assert.Equal(t, config.WorkRoot, seenDir, "the dialog must be seeded at the current WorkRoot so it opens somewhere relevant")
}
