// Integration coverage for the CONTROL/CONTENT work-root split
// (design-log/055). Same rules as ritual_integration_test.go: no body
// comments, meaningful names, verbose assertion messages, no table-driven
// tests.
package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/stages/relocating"
	"ritual/internal/gui/control"
	"ritual/internal/gui/projection"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workrootSnapshotSource struct {
	phase projection.Phase
}

func (s workrootSnapshotSource) Snapshot() projection.ViewModel {
	return projection.ViewModel{Phase: s.phase}
}

func workrootScratchRoots(t *testing.T) {
	t.Helper()
	originalRootPath := config.RootPath
	originalWorkRoot := config.WorkRoot
	config.RootPath = t.TempDir()
	config.WorkRoot = config.RootPath
	t.Cleanup(func() {
		config.RootPath = originalRootPath
		config.WorkRoot = originalWorkRoot
	})
}

func workrootNewRefs(t *testing.T, dir string) relocating.WorkRootRefs {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, config.DirPermission))
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

func workrootWriteObject(t *testing.T, refs relocating.WorkRootRefs, content []byte) string {
	t.Helper()
	key := fmt.Sprintf("objects/%016x", xxhash.Sum64(content))
	compressed, err := adapters.NewCompressingStorage(refs.Local.Current())
	require.NoError(t, err)
	require.NoError(t, compressed.PutStream(context.Background(), key, bytes.NewReader(content)))
	return key
}

func workrootWriteWorkdirFile(t *testing.T, refs relocating.WorkRootRefs, key string, content []byte) {
	t.Helper()
	require.NoError(t, refs.Workdir.Current().PutStream(context.Background(), key, bytes.NewReader(content)))
}

func workrootNewControlService(refs relocating.WorkRootRefs, phase projection.Phase) (*control.ControlService, *adapters.SwappableCmdBuilder) {
	svc := control.NewControlService(nil, workrootSnapshotSource{phase: phase}, nil, nil, nil, nil)
	cmdBuilderRef := adapters.NewSwappableCmdBuilder()
	svc.SetWorkRoot(refs, cmdBuilderRef, nil)
	return svc, cmdBuilderRef
}

func TestIntegration_ChangeWorkRoot_NonDefaultDestination_ContentSetMoves_ControlStaysPut(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	// A successful ChangeWorkRoot swaps refs.Root to a NEW os.Root opened on
	// dst — workrootNewRefs' own t.Cleanup only closes the ORIGINAL root it
	// captured, so the swapped-in root is never closed and Windows' t.TempDir()
	// cleanup fails to remove dst ("used by another process"). A plain defer
	// runs before any t.Cleanup-registered TempDir removal, so close whatever
	// refs.Root currently holds.
	defer func() { _ = refs.Root.Load().Close() }()
	config.WorkRoot = oldWorkDir
	require.NoError(t, (&domain.Settings{Port: 25565, Memory: 4096, MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1, WorkRoot: oldWorkDir}).Save())

	controlStorage, err := adapters.NewFSRepository(mustOpenRoot(t, config.RootPath))
	require.NoError(t, err)
	require.NoError(t, controlStorage.PutStream(context.Background(), "lock", bytes.NewReader([]byte(`{"owner":"host"}`))))

	objKey := workrootWriteObject(t, refs, []byte("world blob"))
	workrootWriteWorkdirFile(t, refs, "server/server.properties", []byte("port=25565"))
	workrootWriteWorkdirFile(t, refs, "worlds/level.dat", []byte("save data"))

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	dst := filepath.Join(t.TempDir(), "new-content")
	require.NoError(t, svc.ChangeWorkRoot(dst), "moving to a fresh non-default destination must succeed")

	assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(objKey)), "objects/ must land at the new content root")
	assert.FileExists(t, filepath.Join(dst, "server", "server.properties"), "server/ must land at the new content root")
	assert.FileExists(t, filepath.Join(dst, "worlds", "level.dat"), "worlds/ must land at the new content root")

	assert.FileExists(t, filepath.Join(config.RootPath, domain.SettingsFilename), "settings.json is CONTROL — it must stay at config.RootPath, never move with the content")
	assert.FileExists(t, filepath.Join(config.RootPath, "lock"), "lock is CONTROL — it must stay at config.RootPath")
	assert.NoDirExists(t, filepath.Join(dst, "lock"), "lock must never be copied into the content root")

	onDisk, err := domain.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, dst, onDisk.WorkRoot, "the persisted settings.json must record the new work_root")
}

func mustOpenRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, config.DirPermission))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestIntegration_ChangeWorkRoot_EndToEnd_ServerCmdBuilderFollowsTheSwap(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	defer func() { _ = refs.Root.Load().Close() }()
	config.WorkRoot = oldWorkDir
	workrootWriteWorkdirFile(t, refs, "server/start.bat", []byte("java -jar server.jar %1"))
	require.NoError(t, (&domain.Settings{Port: 25565, Memory: 4096, MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1, StartScript: "start.bat", WorkRoot: oldWorkDir}).Save())

	svc, cmdBuilderRef := workrootNewControlService(refs, projection.PhaseIdle)
	// The NEW builder ChangeWorkRoot swaps in below lazily caches an open
	// os.Root on the new server/ directory the first time Build() is called
	// (below) — SwappableCmdBuilder.Close() releases it, same reason as the
	// refs.Root defer above (Windows won't let t.TempDir() remove a directory
	// that still has an open handle).
	defer func() { _ = cmdBuilderRef.Close() }()
	oldBuilder, err := adapters.NewServerCmdBuilder(filepath.Join(oldWorkDir, config.ServerDir), "start.bat", func() (*domain.ServerRuntime, error) {
		return domain.NewServerRuntime(25565, 4096)
	})
	require.NoError(t, err)
	cmdBuilderRef.Store(oldBuilder)

	dst := filepath.Join(t.TempDir(), "new-content")
	require.NoError(t, svc.ChangeWorkRoot(dst))

	cmd, err := cmdBuilderRef.Build(context.Background(), nil, io.Discard)
	require.NoError(t, err, "the rebuilt cmd builder must find start.bat under the NEW server/ directory")
	assert.Equal(t, filepath.Join(dst, config.ServerDir), cmd.Dir, "cmd.Dir must point at the new server/ path — proves the cmdBuilder facade actually followed the relocate, not just the storage facades (design-log/055 Phase D)")
}

func TestIntegration_ChangeWorkRoot_WhileSessionRunning_RejectedNoStateChanged(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	config.WorkRoot = oldWorkDir
	originalRoot := refs.Root.Load()
	workrootWriteObject(t, refs, []byte("world blob"))

	svc, _ := workrootNewControlService(refs, projection.PhasePlaying)

	dst := filepath.Join(t.TempDir(), "new-content")
	err := svc.ChangeWorkRoot(dst)
	require.ErrorIs(t, err, control.ErrSessionRunning, "a RUNNING session must reject the relocate outright")

	assert.NoDirExists(t, dst, "no destination content may exist when the relocate is rejected before it starts")
	assert.Same(t, originalRoot, refs.Root.Load(), "the storage refs must be completely untouched by a rejected relocate")
	assert.Equal(t, oldWorkDir, config.WorkRoot, "config.WorkRoot must not change when the relocate is rejected")
}

func TestIntegration_ChangeWorkRoot_CrashBeforeSettingsWrite_OldRootStillActiveOnRestart(t *testing.T) {
	workrootScratchRoots(t)
	realRootPath := config.RootPath

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	config.WorkRoot = oldWorkDir
	objKey := workrootWriteObject(t, refs, []byte("world blob"))
	require.NoError(t, (&domain.Settings{Port: 25565, Memory: 4096, MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1, WorkRoot: oldWorkDir}).Save(),
		"seed settings.json at the real RootPath recording the pre-relocate WorkRoot, mirroring a non-default install")

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	config.RootPath = filepath.Join(t.TempDir(), "unwritable-settings-dir-parent", "missing")
	dst := filepath.Join(t.TempDir(), "new-content")
	err := svc.ChangeWorkRoot(dst)
	require.Error(t, err, "a settings.Save() failure inside commit must surface as an error, simulating a crash before the durable write lands")

	config.RootPath = realRootPath
	reloaded, err := domain.LoadSettings()
	require.NoError(t, err, "restart must still be able to load settings from the untouched, real RootPath")
	assert.Equal(t, oldWorkDir, config.ResolveWorkRoot(reloaded.WorkRoot), "a crash before the settings write must leave the OLD root as what a fresh boot resolves to")

	rc, err := refs.Local.Current().GetStream(context.Background(), objKey)
	require.NoError(t, err, "the old root's content must remain fully intact after a crash-before-write")
	_ = rc.Close()
}

func TestIntegration_ChangeWorkRoot_CrashAfterSettingsWrite_NewRootActiveOldOrphanedButHarmless(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	defer func() { _ = refs.Root.Load().Close() }()
	config.WorkRoot = oldWorkDir
	objKey := workrootWriteObject(t, refs, []byte("world blob"))

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	dst := filepath.Join(t.TempDir(), "new-content")
	require.NoError(t, svc.ChangeWorkRoot(dst), "this relocate must fully succeed, including the durable settings write")

	reloaded, err := domain.LoadSettings()
	require.NoError(t, err, "restart must load settings from the untouched, real RootPath")
	assert.Equal(t, dst, config.ResolveWorkRoot(reloaded.WorkRoot), "once the settings write has landed, a fresh boot must resolve to the NEW root")

	rc, err := refs.Local.Current().GetStream(context.Background(), objKey)
	require.NoError(t, err, "the new root (now the active facade target) must serve the relocated content correctly")
	_ = rc.Close()
}

func TestIntegration_ChangeWorkRoot_CorruptedBlobDuringCopy_AbortsSourceIntactNoPartialSwap(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	config.WorkRoot = oldWorkDir
	originalRoot := refs.Root.Load()

	compressed, err := adapters.NewCompressingStorage(refs.Local.Current())
	require.NoError(t, err)
	badKey := "objects/0000000000000000"
	require.NoError(t, compressed.PutStream(context.Background(), badKey, bytes.NewReader([]byte("mismatched content"))))

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	dst := filepath.Join(t.TempDir(), "new-content")
	err = svc.ChangeWorkRoot(dst)
	require.Error(t, err, "a corrupted blob must fail verification and abort before any swap")

	assert.Same(t, originalRoot, refs.Root.Load(), "the storage refs must be untouched after an aborted relocate")
	assert.Equal(t, oldWorkDir, config.WorkRoot, "config.WorkRoot must be untouched — commit must never run when verify fails")

	reloaded, loadErr := domain.LoadSettings()
	require.NoError(t, loadErr)
	assert.Empty(t, reloaded.WorkRoot, "settings.json must never be written when the relocate aborts before commit")
}

func TestIntegration_ChangeWorkRoot_ConcurrentReadDuringSwap_NeverObservesHalfOldHalfNew(t *testing.T) {
	workrootScratchRoots(t)

	oldWorkDir := filepath.Join(t.TempDir(), "old-content")
	refs := workrootNewRefs(t, oldWorkDir)
	defer func() { _ = refs.Root.Load().Close() }()
	config.WorkRoot = oldWorkDir
	content := []byte("stable server.properties content")
	workrootWriteWorkdirFile(t, refs, "server/server.properties", content)
	for i := range 20 {
		workrootWriteObject(t, refs, fmt.Appendf(nil, "padding blob %d", i))
	}

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	var wg sync.WaitGroup
	var sawCorruption atomic.Bool
	var reads atomic.Int64
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			rc, err := refs.Workdir.GetStream(context.Background(), "server/server.properties")
			if err != nil {
				continue
			}
			got, readErr := io.ReadAll(rc)
			_ = rc.Close()
			reads.Add(1)
			if readErr != nil || !bytes.Equal(got, content) {
				sawCorruption.Store(true)
			}
		}
	})

	dst := filepath.Join(t.TempDir(), "new-content")
	require.NoError(t, svc.ChangeWorkRoot(dst))
	close(stop)
	wg.Wait()

	assert.False(t, sawCorruption.Load(), "every concurrent read through the swappable facade during the swap must return either the complete old content or the complete new content — never a torn or corrupted read")
	assert.Positive(t, reads.Load(), "the concurrent reader must have actually raced against the swap at least once for this test to be meaningful")
}

func TestIntegration_ResetWorkRoot_MovesBackToDefaultRootPath(t *testing.T) {
	workrootScratchRoots(t)

	customWorkDir := filepath.Join(t.TempDir(), "custom-content")
	refs := workrootNewRefs(t, customWorkDir)
	defer func() { _ = refs.Root.Load().Close() }()
	config.WorkRoot = customWorkDir
	require.NoError(t, (&domain.Settings{Port: 25565, Memory: 4096, MinRAMMB: 1, MinDiskMB: 1, MinJavaVersion: 1, WorkRoot: customWorkDir}).Save())
	objKey := workrootWriteObject(t, refs, []byte("world blob"))

	svc, _ := workrootNewControlService(refs, projection.PhaseIdle)

	require.NoError(t, svc.ResetWorkRoot(), "resetting must succeed even though config.RootPath is a fresh, mostly-empty directory")

	assert.Equal(t, config.RootPath, config.WorkRoot, "ResetWorkRoot must move the content root back to config.RootPath, the zero-config default")
	assert.FileExists(t, filepath.Join(config.RootPath, filepath.FromSlash(objKey)), "content must land directly under RootPath once reset to the default — coexisting with settings.json/lock, exactly like a fresh install's layout")
}
