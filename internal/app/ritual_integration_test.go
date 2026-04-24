// Package app_test contains integration tests for the full Ritual pipeline.
//
// Rules for writing tests in this file:
//
//   - No comments in test bodies. Code must be self-documenting through
//     function names, variable names, and helper names. If a line needs
//     a comment, the name is wrong.
//
//   - Variable names carry meaning. Use "ritual" not "env", "server" not
//     "srv", "retryServer" not "srv2". Names must be unambiguous when read
//     in isolation — in a stack trace, in a diff, in a grep result.
//
//   - Assertion messages are the sole documentation. They appear in test
//     output on failure — three layers of signal:
//     1. Test function name — what scenario failed.
//     2. Assertion message — what was expected and why.
//     3. Expected vs actual values — what went wrong.
//
//   - Every assertion message must be verbose and meaningful. It should
//     explain the scenario context, not just restate the check. An AI agent
//     reading only the failure output must understand what broke and why.
//
//   - Test function names read as user stories:
//     TestIntegration_PlayedAndExitClean_ChangesUploadedAndBackedUp
//     not TestIntegration_Case8
//
//   - Flat structure. Arrange, act, assert visible in one scroll.
//     Helpers hide plumbing, never intent.
//
//   - No table-driven tests. Each scenario is its own function.
//
//   - Every assertion has a message string. Bare assertions are not allowed.
package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/checks"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/services"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/heartbeat"
	"strings"
	"sync"
	"testing"
	"time"

	stagenames "ritual/internal/core/ritual"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- TestMain: compile fakerun binary once ----------

var fakerunBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ritual-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	bin := filepath.Join(tmp, fmt.Sprintf("fakerun_%d", time.Now().UnixNano()))
	cmd := exec.Command("go", "build", "-o", bin, "ritual/cmd/fakerun")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build fakerun: %v\n", err)
		os.RemoveAll(tmp)
		os.Exit(1)
	}
	fakerunBin = bin

	// Pre-warm: first fakerun spawn pays OS-level cold costs (macOS
	// Gatekeeper, disk cache miss). Amortize into TestMain so no
	// individual test inherits the hit and exceeds the 1s ceiling.
	warmup := exec.Command(fakerunBin, "--root", tmp)
	stdin, _ := warmup.StdinPipe()
	_ = warmup.Start()
	_, _ = stdin.Write([]byte(`{"op":"exit","code":0}` + "\n"))
	_ = stdin.Close()
	_ = warmup.Wait()

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// ---------- testRitual ----------

type testRitual struct {
	localDir        string
	remoteDir       string
	localRoot       *os.Root
	remoteRoot      *os.Root
	local           ports.StorageRepository
	remote          ports.StorageRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	ch              <-chan ports.Event
	ctx             context.Context
	cancel          context.CancelFunc
}

func newRitual(t *testing.T) *testRitual {
	return newRitualWith(t, nil)
}

// newRitualWith builds the harness, applying an optional remote-storage
// transform under the observed decorator. Test callers use the transform
// to inject deterministic failures (e.g., scriptedStorage) while keeping
// the production wiring order (bus ← observed ← transform ← FSRepository).
func newRitualWith(t *testing.T, remoteTransform func(ports.StorageRepository) ports.StorageRepository) *testRitual {
	t.Helper()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	localRoot, err := os.OpenRoot(localDir)
	require.NoError(t, err, "open local root")
	t.Cleanup(func() { localRoot.Close() })

	remoteRoot, err := os.OpenRoot(remoteDir)
	require.NoError(t, err, "open remote root")
	t.Cleanup(func() { remoteRoot.Close() })

	rawLocal, err := adapters.NewFSRepository(localRoot, "local")
	require.NoError(t, err, "create local FSRepository")

	rawRemote, err := adapters.NewFSRepository(remoteRoot, "remote")
	require.NoError(t, err, "create remote FSRepository")

	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	var remoteInner ports.StorageRepository = rawRemote
	if remoteTransform != nil {
		remoteInner = remoteTransform(rawRemote)
	}

	local := observed.NewStorage(rawLocal, bus)
	remote := observed.NewStorage(remoteInner, bus)

	localManifests := adapters.NewManifestStore(local)
	remoteManifests := adapters.NewManifestStore(remote)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	require.NoError(t, localManifests.Save(ctx, &domain.Manifest{}), "seed empty local manifest")
	require.NoError(t, remoteManifests.Save(ctx, &domain.Manifest{}), "seed empty remote manifest")

	return &testRitual{
		localDir:        localDir,
		remoteDir:       remoteDir,
		localRoot:       localRoot,
		remoteRoot:      remoteRoot,
		local:           local,
		remote:          remote,
		localManifests:  localManifests,
		remoteManifests: remoteManifests,
		bus:             bus,
		ch:              ch,
		ctx:             ctx,
		cancel:          cancel,
	}
}

func newRitualSharingRemote(t *testing.T, other *testRitual) *testRitual {
	t.Helper()

	localDir := t.TempDir()

	localRoot, err := os.OpenRoot(localDir)
	require.NoError(t, err, "open local root (shared-remote)")
	t.Cleanup(func() { localRoot.Close() })

	rawLocal, err := adapters.NewFSRepository(localRoot, "local")
	require.NoError(t, err, "create local FSRepository (shared-remote)")

	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	local := observed.NewStorage(rawLocal, bus)

	localManifests := adapters.NewManifestStore(local)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	require.NoError(t, localManifests.Save(ctx, &domain.Manifest{}), "seed empty local manifest (shared-remote)")

	return &testRitual{
		localDir:        localDir,
		remoteDir:       other.remoteDir,
		localRoot:       localRoot,
		remoteRoot:      other.remoteRoot,
		local:           local,
		remote:          other.remote,
		localManifests:  localManifests,
		remoteManifests: other.remoteManifests,
		bus:             bus,
		ch:              ch,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// ---------- fakeServer ----------

type fakeServer struct {
	binary string
	root   string
	ready  chan io.WriteCloser
	stdin  io.WriteCloser
}

func (r *testRitual) fakerun() *fakeServer {
	return &fakeServer{
		binary: fakerunBin,
		root:   r.localDir,
		ready:  make(chan io.WriteCloser, 1),
	}
}

func (s *fakeServer) write(path string, content []byte) {
	data := base64.StdEncoding.EncodeToString(content)
	line, _ := json.Marshal(map[string]any{"op": "write", "path": path, "data": data})
	_, _ = fmt.Fprintf(s.stdin, "%s\n", line)
}

func (s *fakeServer) delete(path string) {
	line, _ := json.Marshal(map[string]any{"op": "delete", "path": path})
	_, _ = fmt.Fprintf(s.stdin, "%s\n", line)
}

func (s *fakeServer) exit(code int) {
	line, _ := json.Marshal(map[string]any{"op": "exit", "code": code})
	_, _ = fmt.Fprintf(s.stdin, "%s\n", line)
}

// ---------- fakeServerCmdBuilder ----------

type fakeServerCmdBuilder struct {
	server *fakeServer
}

func (b *fakeServerCmdBuilder) Build(ctx context.Context, _ io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	// os.Pipe (not io.Pipe): exec dups the read fd directly into the
	// subprocess, avoiding a copier goroutine. The test's writes to pw
	// land in the kernel buffer immediately, even before exec.Cmd.Start
	// returns, so a quick exit() right after waitReady never deadlocks.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	b.server.ready <- pw

	// exec.CommandContext (not exec.Command): the running stage sets
	// cmd.Cancel, which Go only accepts on a ctx-bound Cmd. Same binding
	// as the production ServerCmdBuilder.
	cmd := exec.CommandContext(ctx, b.server.binary, "--root", b.server.root)
	cmd.Stdin = pr
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd, nil
}

type immediateReady struct{}

func (immediateReady) Wait(_ context.Context) error { return nil }

// ---------- startRitual ----------

func (r *testRitual) startRitual(t *testing.T) *fakeServer {
	t.Helper()
	return r.startRitualWithConditions(t)
}

func (r *testRitual) startRitualWithConditions(t *testing.T, cs ...checks.Check) *fakeServer {
	t.Helper()
	server := r.fakerun()
	return r.startRitualFull(t, cs, server)
}

func (r *testRitual) startRitualFull(t *testing.T, preflightChecks []checks.Check, server *fakeServer) *fakeServer {
	t.Helper()

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create worlds dir")

	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	staging := t.TempDir()

	syncSvc := services.NewSyncService(
		scanner, r.local, r.remote, r.bus,
		services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
		filepath.Join(staging, "local"),
		"sync/integration/worlds",
	)

	getState := func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState }

	downloader := services.NewSyncDownloadUpdater(syncSvc, r.localManifests, r.remoteManifests, getState)
	uploader := services.NewSyncUploader(syncSvc, r.localManifests, r.remoteManifests, getState)
	_ = downloader

	cmdBuilder := &fakeServerCmdBuilder{server: server}

	puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)

	ritual := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		preflightChecks,
		puller, applier, headResolver,
		[]ports.UpdaterService{uploader},
		nil,
		cmdBuilder,
		immediateReady{},
	)

	ritual.Listen(r.ctx)
	r.bus.Publish(app.StartRequested{})
	return server
}

// ---------- send helpers ----------

func (r *testRitual) sendStop() {
	r.bus.Publish(app.StopRequested{})
}

func (r *testRitual) sendRetry() {
	r.bus.Publish(app.RetryRequested{})
}

// ---------- wait helpers ----------

func (r *testRitual) waitDone(t *testing.T) {
	t.Helper()
	waitForIntegrationStatus(t, r.ch, app.Done, time.Second)
}

func (r *testRitual) waitFailed(t *testing.T) {
	t.Helper()
	waitForIntegrationStatus(t, r.ch, app.Failed, time.Second)
}

func waitForIntegrationStatus(t *testing.T, ch <-chan ports.Event, want app.Outcome, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status %s", want)
		case e, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed while waiting for status")
			}
			if sc, ok := e.(app.StatusChanged); ok && sc.Status == want {
				return
			}
		}
	}
}

// ---------- testFile ----------

type testFile struct {
	path    string
	content []byte
}

func file(path string, content []byte) testFile {
	return testFile{path: path, content: content}
}

// ---------- pulling verbs (V2 refs pipeline) ----------

// buildPullingVerbs constructs puller, applier, and head resolver wired
// against the testRitual's local/remote storage. Workdir targets the
// worlds directory; the applier materialises refs into it. The head
// resolver returns an explicit "no refs" error when the remote has none,
// matching production semantics — tests that don't seed a ref will see
// the pulling stage route to onFail.
func (r *testRitual) buildPullingVerbs(worldsPath string, scanner ports.DirectoryScanner) (ports.Puller, ports.Applier, pulling.HeadResolver) {
	worldsRoot, err := os.OpenRoot(worldsPath)
	if err != nil {
		panic(fmt.Sprintf("open worlds root: %v", err))
	}
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "workdir")
	if err != nil {
		panic(fmt.Sprintf("workdir storage: %v", err))
	}
	runner := adapters.NewSerialRunner()
	puller := refs.NewPuller(r.remote, r.local, runner)
	applier := refs.NewApplier(r.local, workdirStorage, scanner, runner)
	resolver := func(ctx context.Context) (domain.RefID, error) {
		keys, err := r.remote.List(ctx, "refs/")
		if err != nil {
			return "", fmt.Errorf("list refs: %w", err)
		}
		var head string
		for _, key := range keys {
			name := strings.TrimPrefix(key, "refs/")
			name = strings.TrimSuffix(name, ".json")
			if name == "" {
				continue
			}
			if name > head {
				head = name
			}
		}
		return domain.RefID(head), nil
	}
	return puller, applier, resolver
}

// ---------- seed helpers ----------

func seedRemoteWorld(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.remoteDir, files)
	updateManifestXXHash(t, r.ctx, r.remoteDir, r.remoteManifests, config.WorldsDir,
		func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState })
	seedRemoteRef(t, r)
}

// seedRemoteRef commits a ref on remote storage reflecting the current
// contents of remoteDir/worlds. Pulling stage resolves this as HEAD and
// materialises it into the local workdir. Targets: "**" so the walk
// captures every seeded file without a caller-specified glob.
func seedRemoteRef(t *testing.T, r *testRitual) {
	t.Helper()
	remoteWorldsDir := filepath.Join(r.remoteDir, config.WorldsDir)
	if _, err := os.Stat(remoteWorldsDir); os.IsNotExist(err) {
		return
	}
	worldsRoot, err := os.OpenRoot(remoteWorldsDir)
	require.NoError(t, err, "open remote worlds root for seed commit")
	t.Cleanup(func() { worldsRoot.Close() })
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "seed-workdir")
	require.NoError(t, err, "seed workdir storage")
	scanner := adapters.NewFullScanner(os.DirFS(remoteWorldsDir))
	committer := refs.NewCommitter(scanner, workdirStorage, r.remote, adapters.NewSerialRunner())
	_, err = committer.Commit(r.ctx, ports.CommitOpts{Targets: []string{"**"}})
	require.NoError(t, err, "seed remote ref commit")
}

func seedLocalWorld(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.localDir, files)
	updateManifestXXHash(t, r.ctx, r.localDir, r.localManifests, config.WorldsDir,
		func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState })
}

func seedSyncedWorld(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.localDir, files)
	seedFiles(t, r.remoteDir, files)

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	xxhashMap, err := scanner.Scan(r.ctx)
	require.NoError(t, err, "scan worlds for synced seed")

	localManifest, err := r.localManifests.Get(r.ctx)
	require.NoError(t, err, "get local manifest for synced seed")
	localManifest.Worlds.XXHashMap = xxhashMap
	localManifest.Worlds.XXHashSyncAt = time.Now()
	require.NoError(t, r.localManifests.Save(r.ctx, localManifest), "save local manifest for synced seed")

	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, "get remote manifest for synced seed")
	remoteManifest.Worlds.XXHashMap = xxhashMap
	remoteManifest.Worlds.XXHashSyncAt = time.Now()
	require.NoError(t, r.remoteManifests.Save(r.ctx, remoteManifest), "save remote manifest for synced seed")
}

func seedRemoteManifest(t *testing.T, r *testRitual, manifest *domain.Manifest) {
	t.Helper()
	require.NoError(t, r.remoteManifests.Save(r.ctx, manifest), "seed remote manifest")
}

func seedLocalManifest(t *testing.T, r *testRitual, manifest *domain.Manifest) {
	t.Helper()
	require.NoError(t, r.localManifests.Save(r.ctx, manifest), "seed local manifest")
}

func seedFiles(t *testing.T, rootDir string, files []testFile) {
	t.Helper()
	for _, f := range files {
		fullPath := filepath.Join(rootDir, config.WorldsDir, f.path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755), "create parent dirs for %s", f.path)
		require.NoError(t, os.WriteFile(fullPath, f.content, 0o644), "write seed file %s", f.path)
	}
}

func updateManifestXXHash(
	t *testing.T,
	ctx context.Context,
	rootDir string,
	store ports.ManifestStore,
	prefix string,
	getState func(*domain.Manifest) *domain.SyncState,
) {
	t.Helper()
	prefixDir := filepath.Join(rootDir, prefix)
	if _, err := os.Stat(prefixDir); os.IsNotExist(err) {
		return
	}
	scanner := adapters.NewFullScanner(os.DirFS(prefixDir))
	xxhashMap, err := scanner.Scan(ctx)
	require.NoError(t, err, "scan %s for xxhash update", prefix)

	manifest, err := store.Get(ctx)
	require.NoError(t, err, "get manifest for xxhash update")

	state := getState(manifest)
	state.XXHashMap = xxhashMap
	state.XXHashSyncAt = time.Now()
	require.NoError(t, store.Save(ctx, manifest), "save manifest after xxhash update")
}

// ---------- latestBackupName ----------

func (r *testRitual) latestBackupName(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(r.localDir, config.BackupsDir))
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	return entries[len(entries)-1].Name()
}

// ---------- assert helpers ----------

func (r *testRitual) assertLocalHasFile(t *testing.T, path string, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.localDir, config.WorldsDir, path)
	_, err := os.Stat(fullPath)
	assert.NoError(t, err, msg)
}

func (r *testRitual) assertLocalFileContent(t *testing.T, path string, want []byte, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.localDir, config.WorldsDir, path)
	got, err := os.ReadFile(fullPath)
	require.NoError(t, err, msg)
	assert.Equal(t, want, got, msg)
}

func (r *testRitual) assertLocalFileMissing(t *testing.T, path string, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.localDir, config.WorldsDir, path)
	_, err := os.Stat(fullPath)
	assert.True(t, os.IsNotExist(err), msg)
}

func (r *testRitual) assertRemoteHasFile(t *testing.T, path string, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.remoteDir, config.WorldsDir, path)
	_, err := os.Stat(fullPath)
	assert.NoError(t, err, msg)
}

func (r *testRitual) assertRemoteFileContent(t *testing.T, path string, want []byte, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.remoteDir, config.WorldsDir, path)
	got, err := os.ReadFile(fullPath)
	require.NoError(t, err, msg)
	assert.Equal(t, want, got, msg)
}

func (r *testRitual) assertRemoteFileMissing(t *testing.T, path string, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.remoteDir, config.WorldsDir, path)
	_, err := os.Stat(fullPath)
	assert.True(t, os.IsNotExist(err), msg)
}

func (r *testRitual) assertManifestUnlocked(t *testing.T, msg string) {
	t.Helper()
	localManifest, err := r.localManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.Empty(t, localManifest.LockedBy, msg+" (local manifest should be unlocked)")

	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.Empty(t, remoteManifest.LockedBy, msg+" (remote manifest should be unlocked)")
}

func (r *testRitual) assertManifestLockedBy(t *testing.T, wantSubstr string, msg string) {
	t.Helper()
	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.Contains(t, remoteManifest.LockedBy, wantSubstr, msg)
}

func (r *testRitual) assertManifestXXHashCount(t *testing.T, want int, msg string) {
	t.Helper()
	localManifest, err := r.localManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.Len(t, localManifest.Worlds.XXHashMap, want, msg)
}

func (r *testRitual) assertManifestXXHashNotEmpty(t *testing.T, msg string) {
	t.Helper()
	localManifest, err := r.localManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.NotEmpty(t, localManifest.Worlds.XXHashMap, msg)
}

func (r *testRitual) assertBackupExists(t *testing.T, msg string) {
	t.Helper()
	localBackups := filepath.Join(r.localDir, config.BackupsDir)
	entries, err := os.ReadDir(localBackups)
	if os.IsNotExist(err) {
		t.Fatal(msg + " (local backups dir does not exist)")
	}
	require.NoError(t, err, msg)
	assert.NotEmpty(t, entries, msg+" (local backups dir is empty)")
}

func (r *testRitual) assertNoBackup(t *testing.T, msg string) {
	t.Helper()
	localBackups := filepath.Join(r.localDir, config.BackupsDir)
	entries, err := os.ReadDir(localBackups)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err, msg)
	assert.Empty(t, entries, msg)
}

func (r *testRitual) assertBackupFileContent(t *testing.T, backupName, filePath string, want []byte, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.localDir, config.BackupsDir, backupName, filePath)
	got, err := os.ReadFile(fullPath)
	require.NoError(t, err, msg)
	assert.Equal(t, want, got, msg)
}

func (r *testRitual) assertBackupHasManifest(t *testing.T, backupName string, msg string) {
	t.Helper()
	fullPath := filepath.Join(r.localDir, config.BackupsDir, backupName, config.ManifestFilename)
	_, err := os.Stat(fullPath)
	assert.NoError(t, err, msg)
}

func (r *testRitual) assertBackupCount(t *testing.T, want int, msg string) {
	t.Helper()
	localBackups := filepath.Join(r.localDir, config.BackupsDir)
	entries, err := os.ReadDir(localBackups)
	if os.IsNotExist(err) {
		assert.Equal(t, 0, want, msg)
		return
	}
	require.NoError(t, err, msg)

	count := 0
	for _, e := range entries {
		if _, parseErr := time.Parse(config.TimestampFormat, e.Name()); parseErr == nil {
			count++
		}
	}
	assert.Equal(t, want, count, msg)
}

func (r *testRitual) assertNoStagingFiles(t *testing.T, msg string) {
	t.Helper()
	keys, err := r.remote.List(r.ctx, "sync")
	if err != nil {
		return
	}
	assert.Empty(t, keys, msg)
}

// ---------- condition helpers ----------

func failCheck(reason string) checks.Check {
	return func(_ context.Context) error {
		return fmt.Errorf("%s", reason)
	}
}

// ---------- failOncePuller ----------

type failOnceIntegrationPuller struct {
	inner ports.Puller
	calls int
}

func (f *failOnceIntegrationPuller) Pull(ctx context.Context, id domain.RefID) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("simulated transient failure")
	}
	return f.inner.Pull(ctx, id)
}

// ---------- bus event collection + filters ----------
// Used by backup stories to assert on stage transitions, storage decorator
// emissions, and error events without coupling to the production subscriber.
// A separate Subscribe per test avoids racing testRitual.ch (owned by
// waitForIntegrationStatus).

// collectBusEvents starts a fresh subscription and returns a drain function.
// Callers invoke drain AFTER the session reaches a terminal status; it
// unsubscribes, waits for the collector goroutine to exit, and returns a
// safe snapshot. This pattern avoids data races with the running collector.
func collectBusEvents(bus ports.EventBus) func() []ports.Event {
	ch, unsub := bus.Subscribe()
	var mu sync.Mutex
	events := []ports.Event{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
	}()
	return func() []ports.Event {
		unsub()
		<-done
		mu.Lock()
		defer mu.Unlock()
		snapshot := make([]ports.Event, len(events))
		copy(snapshot, events)
		return snapshot
	}
}

func stageSequence(events []ports.Event) []string {
	seq := []string{}
	push := func(name string) {
		if name == stagenames.StageDone || name == stagenames.StageFailed {
			return
		}
		if len(seq) > 0 && seq[len(seq)-1] == name {
			return
		}
		seq = append(seq, name)
	}
	for _, e := range events {
		sc, ok := e.(stagenames.StateChangedInfo)
		if !ok {
			continue
		}
		push(sc.From)
		push(sc.To)
	}
	return seq
}

func countRemoteGetsBetween(events []ports.Event, operation string) int {
	inWindow := false
	count := 0
	for _, e := range events {
		switch v := e.(type) {
		case stagenames.StartInfo:
			if v.Operation == operation {
				inWindow = true
			}
		case stagenames.FinishInfo:
			if v.Operation == operation {
				inWindow = false
			}
		case observed.StorageGetInfo:
			if inWindow && v.Store == "fs::remote" {
				count++
			}
		}
	}
	return count
}

func filterCopyInfoWithDstPrefix(events []ports.Event, prefix string) []observed.StorageCopyInfo {
	out := []observed.StorageCopyInfo{}
	for _, e := range events {
		c, ok := e.(observed.StorageCopyInfo)
		if ok && strings.HasPrefix(c.DstKey, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// serverEventTypeSequence picks the ordered lifecycle events the GUI
// subscribes to. Skips intermediate stdout/stop-requested noise; asserts
// only the transitions that drive UI state.
func serverEventTypeSequence(events []ports.Event) []string {
	seq := []string{}
	for _, e := range events {
		switch e.(type) {
		case running.ServerStartingInfo:
			seq = append(seq, "ServerStartingInfo")
		case running.ServerReadyInfo:
			seq = append(seq, "ServerReadyInfo")
		case running.ServerStoppingInfo:
			seq = append(seq, "ServerStoppingInfo")
		case running.ServerStoppedInfo:
			seq = append(seq, "ServerStoppedInfo")
		case running.ServerCrashedInfo:
			seq = append(seq, "ServerCrashedInfo")
		}
	}
	return seq
}

func hasBackupErrorInfo(events []ports.Event) bool {
	for _, e := range events {
		ei, ok := e.(stagenames.ErrorInfo)
		if ok && ei.Operation == "backup" {
			return true
		}
	}
	return false
}

// ---------- scriptedStorage ----------
// Decorator that injects deterministic failures by delegating to a rule
// function. Currently only Copy is scripted — add more per story need.
// Sits under the observed decorator so failures surface as StorageCopyInfo
// with Err set, mirroring production error reporting.
type scriptedStorage struct {
	ports.StorageRepository
	copyFail func(src, dst string) error
}

func (s *scriptedStorage) Copy(ctx context.Context, src, dst string) error {
	if s.copyFail != nil {
		if err := s.copyFail(src, dst); err != nil {
			return err
		}
	}
	return s.StorageRepository.Copy(ctx, src, dst)
}

// ---------- fakeServer wait helper ----------

func (s *fakeServer) waitReady(t *testing.T) {
	t.Helper()
	select {
	case s.stdin = <-s.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for fakerun stdin to be connected")
	}
}

// ---------- integration tests: happy paths ----------

func TestIntegration_FirstLaunch_NoLocalFiles_DownloadsEverything(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level data")),
		file("world/region/r.0.0.mca", []byte("region data")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertLocalHasFile(t, "world/level.dat",
		"first launch — world files should be downloaded from remote")
	ritual.assertLocalHasFile(t, "world/region/r.0.0.mca",
		"first launch — region files should be downloaded from remote")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after successful first run")
}

func TestIntegration_PlayedAndExitClean_ChangesUploadedAndBackedUp(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("original level")),
		file("world/region/r.0.0.mca", []byte("region data")),
		file("world/playerdata/old.dat", []byte("old player")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("modified level"))
	server.write("worlds/world/playerdata/new.dat", []byte("new player"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertRemoteFileContent(t, "world/level.dat", []byte("modified level"),
		"server modified level.dat — remote should reflect the change after publish")
	ritual.assertRemoteHasFile(t, "world/playerdata/new.dat",
		"server created new player file — should exist on remote after publish")
	ritual.assertRemoteHasFile(t, "world/region/r.0.0.mca",
		"untouched region file should still exist on remote")
	ritual.assertManifestXXHashCount(t, 4,
		"3 original + 1 new file = 4 entries in xxhash map")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after successful pipeline completion")
}

func TestIntegration_AlreadySynced_NoTransfersNoBackup(t *testing.T) {
	ritual := newRitual(t)

	seedSyncedWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertNoBackup(t,
		"no file changes during run — backup should not be created")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared even when no work was done")
}

func TestIntegration_ServerMutatesFiles_AllChangesReflectedOnRemote(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/a.dat", []byte("original a")),
		file("world/b.dat", []byte("original b")),
		file("world/c.dat", []byte("original c")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/a.dat", []byte("modified a"))
	server.write("worlds/world/d.dat", []byte("brand new"))
	server.delete("worlds/world/c.dat")
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertRemoteFileContent(t, "world/a.dat", []byte("modified a"),
		"modified file should have new content on remote")
	ritual.assertRemoteFileContent(t, "world/b.dat", []byte("original b"),
		"untouched file should remain unchanged")
	ritual.assertRemoteHasFile(t, "world/d.dat",
		"new file created by server should appear on remote")
	ritual.assertRemoteFileMissing(t, "world/c.dat",
		"deleted file should be removed from remote")
	ritual.assertManifestXXHashCount(t, 3,
		"3 original - 1 deleted + 1 added = 3 entries in manifest")
}

// ---------- integration tests: failure paths ----------

func TestIntegration_ConditionFails_NothingTouched(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)

	ritual.startRitualWithConditions(t, failCheck("insufficient disk space"))
	ritual.waitFailed(t)

	ritual.assertLocalFileMissing(t, "world/level.dat",
		"condition failed at checking — no files should be downloaded")
	ritual.assertManifestUnlocked(t,
		"condition failed before lock — both manifests should remain unlocked")
}

func TestIntegration_LeaseExpired_TakesOverAndCompletes(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)
	seedRemoteManifest(t, ritual, &domain.Manifest{
		LockedBy:    "crashed-host",
		HeartbeatAt: time.Now().Add(-time.Hour),
	})

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertManifestUnlocked(t,
		"stale lock should be taken over and released — crashed host is gone")
}

func TestIntegration_ServerCrash_NoUploadLockReleased(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("original")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(1)
	server.stdin.Close()
	ritual.waitFailed(t)

	ritual.assertRemoteFileContent(t, "world/level.dat", []byte("original"),
		"server crashed — remote should have pre-run content, no upload should happen")
	ritual.assertManifestUnlocked(t,
		"lock must be released even after server crash")
}

// ---------- integration tests: stop, retry, multi-host ----------

func (r *testRitual) startRitualWithFlakyPuller(t *testing.T, flaky *failOnceIntegrationPuller) *fakeServer {
	t.Helper()
	server := r.fakerun()
	cmdBuilder := &fakeServerCmdBuilder{server: server}

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	_ = os.MkdirAll(worldsPath, 0o755)
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	realPuller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)
	flaky.inner = realPuller

	rit := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		nil,
		flaky, applier, headResolver,
		nil, nil,
		cmdBuilder,
		immediateReady{},
	)

	rit.Listen(r.ctx)
	r.bus.Publish(app.StartRequested{})
	return server
}

func TestIntegration_StopMidGame_LockReleased(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("original")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("mid-game state"))
	time.Sleep(50 * time.Millisecond)

	ritual.sendStop()
	time.Sleep(50 * time.Millisecond)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertManifestUnlocked(t,
		"lock must be released after graceful stop")
}

func TestIntegration_FetchFails_RetrySucceeds(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level data")),
	)

	flaky := &failOnceIntegrationPuller{}
	server := ritual.startRitualWithFlakyPuller(t, flaky)
	ritual.waitFailed(t)

	ritual.sendRetry()
	server.waitReady(t)
	server.stdin.Close()
	ritual.waitDone(t)

	assert.Equal(t, 2, flaky.calls,
		"puller should be called twice — fail on first, succeed on retry")
}

func TestIntegration_MultiHost_AUploads_BDownloadsPlaysUploads(t *testing.T) {
	ritualA := newRitual(t)

	seedLocalWorld(t, ritualA,
		file("world/level.dat", []byte("host A level")),
	)

	serverA := ritualA.startRitual(t)
	serverA.waitReady(t)
	serverA.write("worlds/world/level.dat", []byte("host A modified"))
	serverA.exit(0)
	serverA.stdin.Close()
	ritualA.waitDone(t)

	ritualB := newRitualSharingRemote(t, ritualA)

	serverB := ritualB.startRitual(t)
	serverB.waitReady(t)
	serverB.write("worlds/world/level.dat", []byte("host B modified"))
	serverB.exit(0)
	serverB.stdin.Close()
	ritualB.waitDone(t)

	ritualA.assertRemoteFileContent(t, "world/level.dat", []byte("host B modified"),
		"remote should have Host B's changes — B was the last to upload")
}

// ---------- integration tests: backup and retention ----------

func TestIntegration_BackupCreated_ContainsPostRunState(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertBackupExists(t,
		"files changed during run — backup should be created")
	backupName := ritual.latestBackupName(t)
	ritual.assertBackupFileContent(t, backupName, "worlds/world/level.dat", []byte("after run"),
		"backup should contain post-run snapshot (runs after publishing)")
	ritual.assertBackupHasManifest(t, backupName,
		"backup should contain manifest.json snapshot")
}

func TestIntegration_NothingChanged_NoBackupCreated(t *testing.T) {
	ritual := newRitual(t)

	seedSyncedWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertNoBackup(t,
		"server ran but changed nothing — no backup should be created")
}

func TestIntegration_BackupUsesSameStorageOnly_NoRemoteReadDuringBackup(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	events := drain()

	assert.Zero(t, countRemoteGetsBetween(events, "backup"),
		"backup stage must not issue any Get on remote — same-storage CopyObject is server-side; any remote Get implies the cross-storage download path we removed")
}

func TestIntegration_BackupEmitsStorageCopyEventsWithBackupsPrefix(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	events := drain()

	copies := filterCopyInfoWithDstPrefix(events, config.BackupsDir+"/")
	assert.GreaterOrEqual(t, len(copies), 2,
		"backup must emit StorageCopyInfo per file per side — at least one local + one remote — so logs and GUI show the snapshot fanning out")
}

func TestIntegration_ServerCrash_SkipsPublishAndBackup(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("mid crash"))
	server.exit(1)
	server.stdin.Close()
	ritual.waitFailed(t)

	events := drain()

	stages := stageSequence(events)
	assert.NotContains(t, stages, stagenames.StagePublishing,
		"server crash (exit code != 0) must route straight Running → Unlocking — Publishing is unsafe when local is mid-mutation")
	assert.NotContains(t, stages, stagenames.StageBackup,
		"server crash must also skip Backup — no canonical state exists when the crash happened mid-mutation")
	ritual.assertManifestUnlocked(t,
		"crash path still must release the lock via Unlocking")
}

func TestIntegration_PipelineOrder_MatchesCheckFetchAcquireRunPublishBackupUnlockRetain(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	events := drain()

	want := []string{
		stagenames.StageChecking,
		stagenames.StagePulling,
		stagenames.StageAcquiring,
		stagenames.StageRunning,
		stagenames.StagePublishing,
		stagenames.StageBackup,
		stagenames.StageUnlocking,
		stagenames.StageRetaining,
	}
	assert.Equal(t, want, stageSequence(events),
		"pipeline order is load-bearing — Publish writes remote, Backup snapshots post-publish canonical state; swapping them means backing up pre-run content instead")
}

func TestIntegration_ServerLifecycleEventsEmitted_StartingReadyStoppingStopped(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	ritual.bus.Publish(app.StopRequested{})
	time.Sleep(50 * time.Millisecond)
	server.stdin.Close()
	ritual.waitDone(t)

	events := drain()

	types := serverEventTypeSequence(events)
	assert.Equal(t,
		[]string{"ServerStartingInfo", "ServerReadyInfo", "ServerStoppingInfo", "ServerStoppedInfo"},
		types,
		"server lifecycle events must fire in fixed order — GUI state machine subscribes to these to drive DOWN/STARTING/STARTED/STOPPING transitions; backend emission is the contract")
}

func TestIntegration_BackupCopyError_EmitsErrorInfo_LockStillReleased(t *testing.T) {
	failingRemote := func(raw ports.StorageRepository) ports.StorageRepository {
		return &scriptedStorage{
			StorageRepository: raw,
			copyFail: func(_, dst string) error {
				if strings.HasPrefix(dst, config.BackupsDir+"/") {
					return errors.New("simulated remote backup failure")
				}
				return nil
			},
		}
	}

	ritual := newRitualWith(t, failingRemote)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(ritual.bus)

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	events := drain()

	assert.True(t, hasBackupErrorInfo(events),
		"backup Copy failure must surface as ritual.ErrorInfo{Operation:backup} so operators see it in logs and the GUI")
	ritual.assertManifestUnlocked(t,
		"backup failure is non-fatal by design — Unlocking must still release the lease so the next session can acquire it")
}

func TestIntegration_OutdatedManifest_NoXXHash_FullSyncPopulatesMaps(t *testing.T) {
	ritual := newRitual(t)

	seedFiles(t, ritual.localDir, []testFile{
		file("world/level.dat", []byte("level")),
	})
	seedLocalManifest(t, ritual, &domain.Manifest{ManifestVersion: "1.0.0"})

	seedFiles(t, ritual.remoteDir, []testFile{
		file("world/level.dat", []byte("level")),
	})
	seedRemoteManifest(t, ritual, &domain.Manifest{ManifestVersion: "1.0.0"})

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertManifestXXHashNotEmpty(t,
		"outdated manifest with no xxhash — pipeline should populate maps from actual files")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after migration sync")
}

// ---------- liveSyncCmdBuilder ----------

type liveSyncCmdBuilder struct {
	binary string
	root   string
}

func (b *liveSyncCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, b.binary, "--root", b.root)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd, nil
}

// ---------- startRitualWithLiveSync ----------

func (r *testRitual) startRitualWithLiveSync(t *testing.T) {
	t.Helper()

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create worlds dir")

	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	staging := t.TempDir()

	syncSvc := services.NewSyncService(
		scanner, r.local, r.remote, r.bus,
		services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
		filepath.Join(staging, "local"),
		"sync/integration/worlds",
	)

	getState := func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState }

	downloader := services.NewSyncDownloadUpdater(syncSvc, r.localManifests, r.remoteManifests, getState)
	uploader := services.NewSyncUploader(syncSvc, r.localManifests, r.remoteManifests, getState)
	_ = downloader

	cmdBuilder := &liveSyncCmdBuilder{binary: fakerunBin, root: r.localDir}

	_, stopHeartbeat := heartbeat.Attach(r.bus, r.localManifests, r.remoteManifests, syncSvc)
	t.Cleanup(stopHeartbeat)

	puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)

	rit := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		nil,
		puller, applier, headResolver,
		[]ports.UpdaterService{uploader},
		nil,
		cmdBuilder,
		immediateReady{},
	)

	rit.Listen(r.ctx)
	r.bus.Publish(app.StartRequested{})
}

// ---------- live sync assert helpers ----------

func (r *testRitual) assertRemoteManifestSyncAt(t *testing.T, msg string) time.Time {
	t.Helper()
	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, msg)
	assert.False(t, remoteManifest.Worlds.XXHashSyncAt.IsZero(), msg)
	return remoteManifest.Worlds.XXHashSyncAt
}

func waitForSaveCompleted(t *testing.T, ch <-chan ports.Event, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	seen := 0
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d SaveCompleted events (got %d)", count, seen)
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed while waiting for SaveCompleted (got %d/%d)", seen, count)
			}
			if _, ok := e.(running.SaveCompleted); ok {
				seen++
				if seen >= count {
					return
				}
			}
		}
	}
}

func waitForSaveCompletedThenStatus(t *testing.T, ch <-chan ports.Event, saveCount int, stop func(), want app.Outcome, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	seen := 0
	stopped := false
	for {
		select {
		case <-deadline:
			if !stopped {
				t.Fatalf("timed out waiting for %d SaveCompleted events (got %d)", saveCount, seen)
			}
			t.Fatalf("timed out waiting for %s status after stop", want)
		case e, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed unexpectedly")
			}
			if !stopped {
				if _, ok := e.(running.SaveCompleted); ok {
					seen++
					if seen >= saveCount {
						stop()
						stopped = true
					}
				}
			}
			if stopped {
				if sc, ok := e.(app.StatusChanged); ok && sc.Status == want {
					return
				}
			}
		}
	}
}

func seedRemoteWorldWithShortHeartbeat(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.remoteDir, files)

	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, "get remote manifest for short heartbeat seed")
	remoteManifest.Lease.HeartbeatInterval = domain.Duration(20 * time.Millisecond)
	remoteManifest.Lease.TTL = domain.Duration(500 * time.Millisecond)

	worldsPath := filepath.Join(r.remoteDir, config.WorldsDir)
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	xxhashMap, err := scanner.Scan(r.ctx)
	require.NoError(t, err, "scan worlds for short heartbeat seed")
	remoteManifest.Worlds.XXHashMap = xxhashMap
	remoteManifest.Worlds.XXHashSyncAt = time.Now()

	require.NoError(t, r.remoteManifests.Save(r.ctx, remoteManifest), "save remote manifest with short heartbeat")
	seedRemoteRef(t, r)
}

// ---------- integration tests: live sync ----------

func TestIntegration_PlayingWithLiveSync_WorldsSyncEveryTick(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorldWithShortHeartbeat(t, ritual,
		file("world/level.dat", []byte("level data")),
		file("world/region/r.0.0.mca", []byte("region data")),
	)

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompletedThenStatus(t, ch, 2, ritual.sendStop, app.Done, time.Second)

	ritual.assertRemoteHasFile(t, "world/level.dat",
		"live sync — world files should remain on remote after sync ticks")
	ritual.assertRemoteHasFile(t, "world/region/r.0.0.mca",
		"live sync — region files should remain on remote after sync ticks")
	ritual.assertRemoteManifestSyncAt(t,
		"live sync — remote manifest SyncState.XXHashSyncAt should be updated after sync ticks")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after live sync run completes")
}

func TestIntegration_NothingChangedDuringPlay_NothingUploaded(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorldWithShortHeartbeat(t, ritual,
		file("world/level.dat", []byte("level data")),
	)

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompletedThenStatus(t, ch, 2, ritual.sendStop, app.Done, time.Second)

	ritual.assertRemoteFileContent(t, "world/level.dat", []byte("level data"),
		"nothing changed during play — remote file content should be identical to seed")
	ritual.assertNoStagingFiles(t,
		"nothing changed during play — no staging artifacts should remain")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after no-op sync run")
}

func TestIntegration_CrashRecovery_LocalNewerKept(t *testing.T) {
	ritual := newRitual(t)

	seedFiles(t, ritual.remoteDir, []testFile{
		file("world/level.dat", []byte("remote old data")),
	})

	remoteManifest, err := ritual.remoteManifests.Get(ritual.ctx)
	require.NoError(t, err, "get remote manifest for crash recovery test")
	remoteManifest.Lease.HeartbeatInterval = domain.Duration(20 * time.Millisecond)
	remoteManifest.Lease.TTL = domain.Duration(500 * time.Millisecond)
	remoteManifest.Worlds.XXHashSyncAt = time.Now().Add(-time.Hour)
	remoteManifest.Worlds.XXHashMap = map[string]domain.FileEntry{
		"world/level.dat": {Hash: "remote-hash-old", Size: 16},
	}
	require.NoError(t, ritual.remoteManifests.Save(ritual.ctx, remoteManifest), "save remote manifest for crash recovery")

	seedFiles(t, ritual.localDir, []testFile{
		file("world/level.dat", []byte("local newer data")),
	})

	worldsPath := filepath.Join(ritual.localDir, config.WorldsDir)
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	localXXHash, err := scanner.Scan(ritual.ctx)
	require.NoError(t, err, "scan local worlds for crash recovery")

	localManifest, err := ritual.localManifests.Get(ritual.ctx)
	require.NoError(t, err, "get local manifest for crash recovery test")
	localManifest.Worlds.XXHashSyncAt = time.Now()
	localManifest.Worlds.XXHashMap = localXXHash
	require.NoError(t, ritual.localManifests.Save(ritual.ctx, localManifest), "save local manifest for crash recovery")

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Done, time.Second)

	ritual.assertLocalFileContent(t, "world/level.dat", []byte("local newer data"),
		"crash recovery — local is newer than remote, download should be skipped and local data preserved")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after crash recovery run")
}

func TestIntegration_LiveSyncUploadsNewFiles_RemoteReflectsChanges(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorldWithShortHeartbeat(t, ritual,
		file("world/level.dat", []byte("original level")),
	)

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompleted(t, ch, 1, time.Second)

	worldsPath := filepath.Join(ritual.localDir, config.WorldsDir)
	newFilePath := filepath.Join(worldsPath, "world", "playerdata", "player1.dat")
	require.NoError(t, os.MkdirAll(filepath.Dir(newFilePath), 0o755), "create playerdata dir")
	require.NoError(t, os.WriteFile(newFilePath, []byte("player one"), 0o644), "write new player file")

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Done, time.Second)

	ritual.assertRemoteHasFile(t, "world/level.dat",
		"live sync — original file should still exist on remote")
	ritual.assertRemoteHasFile(t, "world/playerdata/player1.dat",
		"live sync — new file written during play should be uploaded to remote by sync tick")
	ritual.assertRemoteFileContent(t, "world/playerdata/player1.dat", []byte("player one"),
		"live sync — uploaded file content should match what was written locally")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after live sync with file mutations")
}

func TestIntegration_LiveSyncAfterCleanStop_RemoteHasSyncedState(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorldWithShortHeartbeat(t, ritual,
		file("world/level.dat", []byte("level data")),
		file("world/region/r.0.0.mca", []byte("region data")),
	)

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Done, time.Second)

	remoteManifest, err := ritual.remoteManifests.Get(ritual.ctx)
	require.NoError(t, err, "get remote manifest after clean stop")
	assert.NotEmpty(t, remoteManifest.Worlds.XXHashMap,
		"clean stop after live sync — remote manifest should have xxhash map populated from sync")
	assert.False(t, remoteManifest.Worlds.XXHashSyncAt.IsZero(),
		"clean stop after live sync — remote manifest XXHashSyncAt should be set")

	localManifest, err := ritual.localManifests.Get(ritual.ctx)
	require.NoError(t, err, "get local manifest after clean stop")
	assert.NotEmpty(t, localManifest.Worlds.XXHashMap,
		"clean stop after live sync — local manifest should have xxhash map populated from sync")

	ritual.assertRemoteHasFile(t, "world/level.dat",
		"clean stop — world files should persist on remote")
	ritual.assertRemoteHasFile(t, "world/region/r.0.0.mca",
		"clean stop — region files should persist on remote")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after clean stop with live sync")
}

func TestIntegration_ServerBecomesReady_AutosavesDisabled(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorldWithShortHeartbeat(t, ritual,
		file("world/level.dat", []byte("level data")),
	)

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			ritual.sendStop()
			t.Fatal("timed out waiting for save-off — autosaves should be disabled when server becomes ready")
		case e, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed while waiting for save-off")
			}
			if out, ok := e.(running.ServerOutputInfo); ok && strings.Contains(out.Line, "Automatic saving is now disabled") {
				ritual.sendStop()
				waitForIntegrationStatus(t, ch, app.Done, time.Second)
				return
			}
		}
	}
}
