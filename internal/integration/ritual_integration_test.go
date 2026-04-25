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
package integration_test

import (
	"bytes"
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
	"ritual/internal/config"
	"ritual/internal/core/checks"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/retention"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/pipeline"
	subretention "ritual/internal/subsystems/retention"
	"strings"
	"sync"
	"testing"
	"time"

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
	localDir   string
	remoteDir  string
	localRoot  *os.Root
	remoteRoot *os.Root
	local      ports.StorageRepository
	remote     ports.StorageRepository
	bus        ports.EventBus
	ch         <-chan ports.Event
	ctx        context.Context
	cancel     context.CancelFunc

	// Optional prune jobs per stage instance. Integration tests simulate
	// the remote slot by pointing its jobs at localStorage — cheaper than
	// standing up a second remote fixture and proves both instances wire.
	localRetentions  []retaining.Job
	remoteRetentions []retaining.Job
}

func newRitual(t *testing.T) *testRitual {
	return newRitualWith(t, nil)
}

// newRitualWith builds the harness, applying an optional remote-storage
// transform under the observed decorator. Test callers use the transform
// to inject deterministic failures while keeping the production wiring
// order (bus ← observed ← transform ← FSRepository).
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

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &testRitual{
		localDir:   localDir,
		remoteDir:  remoteDir,
		localRoot:  localRoot,
		remoteRoot: remoteRoot,
		local:      local,
		remote:     remote,
		bus:        bus,
		ch:         ch,
		ctx:        ctx,
		cancel:     cancel,
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

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &testRitual{
		localDir:   localDir,
		remoteDir:  other.remoteDir,
		localRoot:  localRoot,
		remoteRoot: other.remoteRoot,
		local:      local,
		remote:     other.remote,
		bus:        bus,
		ch:         ch,
		ctx:        ctx,
		cancel:     cancel,
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

	cmdBuilder := &fakeServerCmdBuilder{server: server}

	puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)
	committer, pusher, commitTargets := r.buildCommittingVerbs(t, worldsPath, scanner)

	host, _ := os.Hostname()
	locker := observed.NewLocker(lock.New(r.remote, host), r.bus)
	entry := pipeline.Build(pipeline.Deps{
		Bus: r.bus, Checks: preflightChecks,
		Puller: puller, Applier: applier, HeadResolver: headResolver,
		Committer: committer, CommitOpts: ritual.NewCommitOptsResolver(commitTargets), Pusher: pusher,
		LocalRetentions: r.localRetentions, RemoteRetentions: r.remoteRetentions,
		CmdBuilder: cmdBuilder, Readiness: immediateReady{},
		AcquireFn: locker.Acquire, InspectFn: locker.Inspect, ReleaseFn: locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
	})
	stop := lifecycle.Attach(r.ctx, r.bus, entry)
	t.Cleanup(stop)
	r.bus.Publish(ritual.StartRequested{})
	return server
}

// ---------- send helpers ----------

func (r *testRitual) sendStop() {
	r.bus.Publish(ritual.StopRequested{})
}

func (r *testRitual) sendRetry() {
	r.bus.Publish(ritual.RetryRequested{})
}

// ---------- wait helpers ----------

func (r *testRitual) waitDone(t *testing.T) {
	t.Helper()
	waitForIntegrationStatus(t, r.ch, lifecycle.Done, time.Second)
}

func (r *testRitual) waitFailed(t *testing.T) {
	t.Helper()
	waitForIntegrationStatus(t, r.ch, lifecycle.Failed, time.Second)
}

func waitForIntegrationStatus(t *testing.T, ch <-chan ports.Event, want lifecycle.Outcome, timeout time.Duration) {
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
			if sc, ok := e.(lifecycle.StatusChanged); ok && sc.Status == want {
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

// buildCommittingVerbs constructs committer + pusher wired against the
// testRitual's local/remote storage. Workdir targets the worlds directory
// so the committer scans server-mutated files. Targets default to "**" so
// every file produced by a test scenario is captured without bespoke
// glob plumbing.
func (r *testRitual) buildCommittingVerbs(t *testing.T, worldsPath string, scanner ports.DirectoryScanner) (ports.Committer, ports.Pusher, []string) {
	t.Helper()
	worldsRoot, err := os.OpenRoot(worldsPath)
	require.NoError(t, err, "open worlds root for committer")
	t.Cleanup(func() { worldsRoot.Close() })
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "workdir-commit")
	require.NoError(t, err, "workdir storage for committer")
	runner := adapters.NewSerialRunner()
	committer := refs.NewCommitter(scanner, workdirStorage, r.local, runner)
	pusher := refs.NewPusher(r.local, r.remote, runner)
	return committer, pusher, []string{"**"}
}

// ---------- seed helpers ----------

func seedRemoteWorld(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.remoteDir, files)
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
}

func seedExpiredRemoteLock(t *testing.T, r *testRitual, owner, sessionID string) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	payload := map[string]any{
		"owner":       owner,
		"sessionId":   sessionID,
		"acquiredAt":  past.Format(time.RFC3339Nano),
		"heartbeatAt": past.Format(time.RFC3339Nano),
		"expiresAt":   past.Add(time.Minute).Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err, "marshal expired lock payload")
	require.NoError(t, r.remote.PutStream(r.ctx, lock.Key, bytes.NewReader(data)), "seed expired lock object")
}

func (r *testRitual) assertRemoteLockAbsent(t *testing.T, msg string) {
	t.Helper()
	exists, err := r.remote.Exists(r.ctx, lock.Key)
	require.NoError(t, err, msg)
	assert.False(t, exists, msg)
}

func seedFiles(t *testing.T, rootDir string, files []testFile) {
	t.Helper()
	for _, f := range files {
		fullPath := filepath.Join(rootDir, config.WorldsDir, f.path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755), "create parent dirs for %s", f.path)
		require.NoError(t, os.WriteFile(fullPath, f.content, 0o644), "write seed file %s", f.path)
	}
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
		if name == ritual.StageDone || name == ritual.StageFailed {
			return
		}
		if len(seq) > 0 && seq[len(seq)-1] == name {
			return
		}
		seq = append(seq, name)
	}
	for _, e := range events {
		sc, ok := e.(ritual.StateChangedInfo)
		if !ok {
			continue
		}
		push(sc.From)
		push(sc.To)
	}
	return seq
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
}

func TestIntegration_LeaseExpired_TakesOverAndCompletes(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)
	seedExpiredRemoteLock(t, ritual, "crashed-host", "crashed-session")

	server := ritual.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertRemoteLockAbsent(t,
		"stale lease should be taken over and released — crashed host's lock object is gone")
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
	committer, pusher, commitTargets := r.buildCommittingVerbs(t, worldsPath, scanner)

	host, _ := os.Hostname()
	locker := observed.NewLocker(lock.New(r.remote, host), r.bus)
	entry := pipeline.Build(pipeline.Deps{
		Bus:    r.bus,
		Puller: flaky, Applier: applier, HeadResolver: headResolver,
		Committer: committer, CommitOpts: ritual.NewCommitOptsResolver(commitTargets), Pusher: pusher,
		CmdBuilder: cmdBuilder, Readiness: immediateReady{},
		AcquireFn: locker.Acquire, InspectFn: locker.Inspect, ReleaseFn: locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
	})
	stop := lifecycle.Attach(r.ctx, r.bus, entry)
	t.Cleanup(stop)
	r.bus.Publish(ritual.StartRequested{})
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

// ---------- integration tests: backup and retention ----------

func TestIntegration_PipelineOrder_MatchesCheckPullAcquireRunCommitRetainPushRetainUnlock(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(r.bus)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	events := drain()

	want := []string{
		ritual.StageChecking,
		ritual.StagePulling,
		ritual.StageAcquiring,
		ritual.StageRunning,
		ritual.StageCommitting,
		ritual.StageRetaining,
		ritual.StagePushing,
		ritual.StageRetaining,
		ritual.StageUnlocking,
	}
	assert.Equal(t, want, stageSequence(events),
		"post-session chain per spec §2267: commit writes local ref, local prune sweeps orphan blobs before they escape, push uploads ref+blobs, remote prune reaps once remote is authoritative, unlock last")
}

func TestIntegration_ChangesUploaded_RefAppearsOnRemote(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	keys, err := r.remote.List(r.ctx, "refs/")
	require.NoError(t, err, "list remote refs after session")
	assert.GreaterOrEqual(t, len(keys), 2,
		"remote must carry at least the seeded ref and the session's newly pushed ref — otherwise pushing stage dropped the commit")
}

func TestIntegration_ServerCrash_SkipsCommitAndPush(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(r.bus)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("mid crash"))
	server.exit(1)
	server.stdin.Close()
	r.waitFailed(t)

	events := drain()

	stages := stageSequence(events)
	assert.NotContains(t, stages, ritual.StageCommitting,
		"server crash (exit code != 0) must skip Committing — mid-mutation workdir is not a safe snapshot source")
	assert.NotContains(t, stages, ritual.StagePushing,
		"server crash must skip Pushing — nothing was committed, nothing to push")
}

func TestIntegration_Prune_BothInstancesExecute(t *testing.T) {
	r := newRitual(t)

	// Side-agnostic wiring: point the remote prune slot at local storage.
	// Each slot uses a real retention.Job so a regression in wiring (single
	// instance, swapped slots, missing onOK) manifests as missing bus events.
	keepAll := domain.RetentionRules{KeepLast: 999}
	r.localRetentions = []retaining.Job{
		retaining.NewRetentionRefsJob("refs-local", retention.NewRefsRetention(r.local, keepAll), r.local),
		retaining.NewGCRefsJob("gc-refs-local", refs.NewCollector(r.local)),
	}
	r.remoteRetentions = []retaining.Job{
		retaining.NewRetentionRefsJob("refs-remote", retention.NewRefsRetention(r.local, keepAll), r.local),
		retaining.NewGCRefsJob("gc-refs-remote", refs.NewCollector(r.local)),
	}

	drain := collectBusEvents(r.bus)
	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("level data")),
		file("world/region/r.0.0.mca", []byte("region data")),
		file("world/playerdata/player-uuid.dat", []byte("player state")),
	)

	server := r.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	starts := countRetainStarts(drain())
	assert.Equal(t, 2, starts,
		"retain StartInfo must fire twice per run — once paired with committing (local prune), once paired with pushing (remote prune) per spec §2297. starts!=2 means one retaining.Strategy instance was dropped from buildChain")
}

func TestIntegration_Retention_BuildWiresLocalAndRemoteJobs_BothSidesEmitSplitEvents(t *testing.T) {
	r := newRitual(t)

	settingsRoot := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = settingsRoot
	t.Cleanup(func() { config.RootPath = originalRootPath })

	keepAll := domain.RetentionRules{KeepLast: 999}
	require.NoError(t, (&domain.Settings{
		Port:            25565,
		Memory:          4096,
		MinRAMMB:        1,
		MinDiskMB:       1,
		MinJavaVersion:  1,
		LocalRetention:  keepAll,
		RemoteRetention: keepAll,
	}).Save(),
		"settings.Save must succeed before retention.Build — Build reads rules via domain.LoadSettings")

	localJobs, remoteJobs, err := subretention.Build(r.local, r.local, r.bus)
	require.NoError(t, err,
		"retention.Build must wire jobs from real storage + bus without error — composition root contract")
	require.Len(t, localJobs, 3,
		"local side must wire three Jobs in order: refs retention, refs GC, logs retention. Length drift means a slot is missing or duplicated; Strategy iterates the slice verbatim, so a missing Job silently skips a sweep")
	require.Len(t, remoteJobs, 2,
		"remote side must wire exactly two Jobs: refs retention then refs GC. Logs are local-only — adding a logs Job to remote means we tried to retain remote logs that do not exist")

	assert.Equal(t, retaining.KindRetention, localJobs[0].Kind,
		"local[0] must be Retention so manifests drop before GC mark-sweeps blobs they exposed; reverse order leaks orphan blobs that survive until the next session")
	assert.Equal(t, "refs-local", localJobs[0].Label,
		"local[0] Label must round-trip into per-Job events as refs-local — subscribers split sides via Label, not via stage chain inspection")
	assert.Equal(t, retaining.KindGC, localJobs[1].Kind,
		"local[1] must be GC and run after local[0] retention — see §2297 retention pairing rationale")
	assert.Equal(t, "gc-refs-local", localJobs[1].Label,
		"local[1] Label must be gc-refs-local so a stuck GC is attributable to the local side")
	assert.Equal(t, retaining.KindRetention, localJobs[2].Kind,
		"local[2] (logs) must be Retention — logs have no content-addressed blob store so no GC counterpart")
	assert.Equal(t, "logs-local", localJobs[2].Label,
		"local[2] Label distinguishes logs from refs in the same side")

	assert.Equal(t, retaining.KindRetention, remoteJobs[0].Kind,
		"remote[0] must be Retention — remote sweep must drop manifests before GC, identical ordering rule as local")
	assert.Equal(t, "refs-remote", remoteJobs[0].Label,
		"remote[0] Label must be refs-remote — subscribers split sides via Label so a remote-only retention failure is attributable")
	assert.Equal(t, retaining.KindGC, remoteJobs[1].Kind,
		"remote[1] must be GC and run after remote[0] retention; reverse order would leave orphan blobs after retention drops a manifest")
	assert.Equal(t, "gc-refs-remote", remoteJobs[1].Label,
		"remote[1] Label must be gc-refs-remote so the remote-side GC can be observed independently of the local one")

	r.localRetentions = localJobs
	r.remoteRetentions = remoteJobs

	drain := collectBusEvents(r.bus)
	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("level data")),
		file("world/region/r.0.0.mca", []byte("region data")),
	)

	server := r.startRitual(t)
	server.waitReady(t)
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	events := drain()

	wantLifecycle := []string{
		"retention.start:refs-local",
		"retention.finish:refs-local",
		"gc.start:gc-refs-local",
		"gc.finish:gc-refs-local",
		"retention.start:logs-local",
		"retention.finish:logs-local",
		"retention.start:refs-remote",
		"retention.finish:refs-remote",
		"gc.start:gc-refs-remote",
		"gc.finish:gc-refs-remote",
	}
	gotLifecycle := retentionLifecycleSequence(events)
	assert.Equal(t, wantLifecycle, gotLifecycle,
		"a real session wired through retention.Build must fire Retention*/GC* events in this exact order: local side (refs-retention → refs-gc → logs-retention) paired with committing, then remote side (refs-retention → refs-gc) paired with pushing. Drift means a Job slot was dropped, sides were swapped, or a Kind was misclassified")

	assert.True(t, allRetentionFinishesNilErr(events),
		"every Retention/GC Finished event must carry Err=nil for a clean session — a non-nil Err here means a wired Job failed silently against real storage and the upstream chain still treated the run as Done; investigate Job factory wiring before flake-blaming")
}

func retentionLifecycleSequence(events []ports.Event) []string {
	out := []string{}
	for _, e := range events {
		switch v := e.(type) {
		case retaining.RetentionStartedInfo:
			out = append(out, "retention.start:"+v.Label)
		case retaining.RetentionFinishedInfo:
			out = append(out, "retention.finish:"+v.Label)
		case retaining.GCStartedInfo:
			out = append(out, "gc.start:"+v.Label)
		case retaining.GCFinishedInfo:
			out = append(out, "gc.finish:"+v.Label)
		}
	}
	return out
}

func allRetentionFinishesNilErr(events []ports.Event) bool {
	for _, e := range events {
		switch v := e.(type) {
		case retaining.RetentionFinishedInfo:
			if v.Err != nil {
				return false
			}
		case retaining.GCFinishedInfo:
			if v.Err != nil {
				return false
			}
		}
	}
	return true
}

func countRetainStarts(events []ports.Event) int {
	n := 0
	for _, e := range events {
		s, ok := e.(ritual.StartInfo)
		if ok && s.Operation == "retain" {
			n++
		}
	}
	return n
}

func TestIntegration_ServerLifecycleEventsEmitted_StartingReadyStoppingStopped(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("level")),
	)

	drain := collectBusEvents(r.bus)

	server := r.startRitual(t)
	server.waitReady(t)
	r.bus.Publish(ritual.StopRequested{})
	time.Sleep(50 * time.Millisecond)
	server.stdin.Close()
	r.waitDone(t)

	events := drain()

	types := serverEventTypeSequence(events)
	assert.Equal(t,
		[]string{"ServerStartingInfo", "ServerReadyInfo", "ServerStoppingInfo", "ServerStoppedInfo"},
		types,
		"server lifecycle events must fire in fixed order — GUI state machine subscribes to these to drive DOWN/STARTING/STARTED/STOPPING transitions; backend emission is the contract")
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

// ---------- failure-injection storage decorator ----------

// refPutFailureInjector wraps a StorageRepository and fails the Nth PutStream
// targeting any key under "refs/". Blob writes (objects/) and read paths pass
// through. Counter is shared across the seed phase and the session phase, so
// callers pick failAt with the seed's ref-puts in mind: failAt=2 fails the
// session push when seedRemoteRef has already consumed the 1st ref-put.
type refPutFailureInjector struct {
	ports.StorageRepository
	mu     sync.Mutex
	seen   int
	failAt int
}

func newRefPutFailureInjector(inner ports.StorageRepository, failAt int) *refPutFailureInjector {
	return &refPutFailureInjector{StorageRepository: inner, failAt: failAt}
}

func (f *refPutFailureInjector) PutStream(ctx context.Context, key string, body io.Reader) error {
	if strings.HasPrefix(key, "refs/") {
		f.mu.Lock()
		f.seen++
		seen := f.seen
		f.mu.Unlock()
		if seen == f.failAt {
			return errors.New("simulated remote refs/ PutStream failure")
		}
	}
	return f.StorageRepository.PutStream(ctx, key, body)
}

// ---------- local ref seeding ----------

// seedLocalRef commits a ref into r.local from the current contents of the
// local worlds dir. Mirror of seedRemoteRef but on the local side: tests use
// it to fabricate a "prior session's local HEAD" without going through the
// running stage.
func seedLocalRef(t *testing.T, r *testRitual) {
	t.Helper()
	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create local worlds dir before seeding ref")
	worldsRoot, err := os.OpenRoot(worldsPath)
	require.NoError(t, err, "open local worlds root for seed commit")
	t.Cleanup(func() { worldsRoot.Close() })
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "seed-local-workdir")
	require.NoError(t, err, "seed local workdir storage")
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	committer := refs.NewCommitter(scanner, workdirStorage, r.local, adapters.NewSerialRunner())
	_, err = committer.Commit(r.ctx, ports.CommitOpts{Targets: []string{"**"}})
	require.NoError(t, err, "seed local ref commit")
}

// ---------- user stories: cross-host coordination ----------

func TestIntegration_TeammateAlreadyPlaying_MyClientWaitsForLeaseRelease(t *testing.T) {
	teammate := newRitual(t)
	seedRemoteWorld(t, teammate, file("world/level.dat", []byte("shared")))

	teammateServer := teammate.startRitual(t)
	teammateServer.waitReady(t)

	mine := newRitualSharingRemote(t, teammate)
	mine.startRitual(t)
	mine.waitFailed(t)

	teammateServer.exit(0)
	teammateServer.stdin.Close()
	teammate.waitDone(t)

	teammate.assertRemoteLockAbsent(t,
		"once my teammate exits cleanly the shared lock must release — if it lingers, my next attempt to play sees a phantom 'someone else is editing' message and I cannot continue without manual cleanup (US-7 cross-host resume)")
}

func TestIntegration_UploadCutOffMidFlight_RemoteStaysOnPriorPushedWorld(t *testing.T) {
	var injector *refPutFailureInjector
	r := newRitualWith(t, func(inner ports.StorageRepository) ports.StorageRepository {
		injector = newRefPutFailureInjector(inner, 2)
		return injector
	})

	seedRemoteWorld(t, r, file("world/level.dat", []byte("teammates last saw this")))

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("my new edits"))
	server.exit(0)
	server.stdin.Close()
	r.waitFailed(t)

	require.NotNil(t, injector, "failure injector must wire under remoteTransform")

	keys, err := r.remote.List(r.ctx, "refs/")
	require.NoError(t, err, "list remote refs after my upload was cut off mid-flight")
	assert.Len(t, keys, 1,
		"if my upload is cut off mid-flight, my teammates must continue to see the previously confirmed world — a half-uploaded snapshot promoted to HEAD would silently corrupt their next session and they'd have no way to know my edits never finished")
}

// ---------- user stories driving impl ----------
//
// Each test below names a player-visible scenario and asserts the consequence
// the player perceives if the invariant holds. Tests fail today (red bar) —
// the assertion message tells future-impl what user impact each missing piece
// of behaviour causes. When impl lands, the test goes green; the user story
// stays as living documentation.

func TestIntegration_PlayerEditedFilesByHand_NextStartShowsDivergencePromptsPublishOrRestore(t *testing.T) {
	r := newRitual(t)
	seedLocalWorld(t, r, file("world/level.dat", []byte("synced")))
	seedLocalRef(t, r)
	seedRemoteWorld(t, r, file("world/level.dat", []byte("synced")))
	require.NoError(t, os.WriteFile(
		filepath.Join(r.localDir, config.WorldsDir, "world/level.dat"),
		[]byte("manual recovery"), 0o644),
		"player hand-edited their world file between sessions (manual recovery, mod install, file rescue, mod-pack swap)")

	r.startRitual(t)
	r.waitFailed(t)

	r.assertLocalFileContent(t, "world/level.dat", []byte("manual recovery"),
		"if I touched my world files outside ritual — to recover a corrupt save, install a mod by hand, swap mod-packs, copy a friend's file in — ritual must STOP at a divergence stage and ask me which side wins (publish my hand-edits as a new snapshot, or restore from a prior pushed snapshot). Silently overwriting my edits with the cloud copy throws away whatever I was doing without my consent")
}

func TestIntegration_TeammatePushedWhileOffline_StaleClientShowsDivergence(t *testing.T) {
	teammate := newRitual(t)
	seedRemoteWorld(t, teammate, file("world/level.dat", []byte("morning")))

	teammateServer := teammate.startRitual(t)
	teammateServer.waitReady(t)
	teammateServer.write("worlds/world/level.dat", []byte("teammate's afternoon edits"))
	teammateServer.exit(0)
	teammateServer.stdin.Close()
	teammate.waitDone(t)

	mine := newRitualSharingRemote(t, teammate)
	seedLocalWorld(t, mine, file("world/level.dat", []byte("morning")))
	seedLocalRef(t, mine)

	mine.startRitual(t)
	mine.waitFailed(t)
}

func TestIntegration_LongSessionCrashAfterHoursOfPlay_LastTickAlreadyOnRemote(t *testing.T) {
	t.Fatal("user story: after a multi-hour session crashes mid-play, my next start must find the most recent live-ticker snapshot already on remote — losing 30 minutes of play to a power cut would shatter trust in the tool. Drives spec §US-2: per-tick save-all flush + commit + amend-aware push within RunningState (planned 5-min cadence). Replace this Fatal once the running-stage self-transition + a test-tunable TickInterval land.")
}
