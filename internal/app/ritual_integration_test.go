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
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
	"ritual/internal/subsystems/heartbeat"
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
	local           *adapters.FSRepository
	remote          *adapters.FSRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	ch              <-chan ports.Event
	ctx             context.Context
	cancel          context.CancelFunc
}

func newRitual(t *testing.T) *testRitual {
	t.Helper()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	localRoot, err := os.OpenRoot(localDir)
	require.NoError(t, err, "open local root")
	t.Cleanup(func() { localRoot.Close() })

	remoteRoot, err := os.OpenRoot(remoteDir)
	require.NoError(t, err, "open remote root")
	t.Cleanup(func() { remoteRoot.Close() })

	local, err := adapters.NewFSRepository(localRoot)
	require.NoError(t, err, "create local FSRepository")

	remote, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err, "create remote FSRepository")

	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

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

	local, err := adapters.NewFSRepository(localRoot)
	require.NoError(t, err, "create local FSRepository (shared-remote)")

	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

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
	stdin  io.WriteCloser
}

func (r *testRitual) fakerun() *fakeServer {
	return &fakeServer{binary: fakerunBin, root: r.localDir}
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

func (b *fakeServerCmdBuilder) Build(_ context.Context, _ io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	pr, pw := io.Pipe()
	b.server.stdin = pw

	cmd := exec.Command(b.server.binary, "--root", b.server.root)
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

func (r *testRitual) startRitualWithConditions(t *testing.T, conds ...ports.ConditionService) *fakeServer {
	t.Helper()
	server := r.fakerun()
	return r.startRitualFull(t, conds, server)
}

func (r *testRitual) startRitualFull(t *testing.T, conditions []ports.ConditionService, server *fakeServer) *fakeServer {
	t.Helper()

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0755), "create worlds dir")

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

	cmdBuilder := &fakeServerCmdBuilder{server: server}

	ritual := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		conditions,
		[]ports.UpdaterService{downloader},
		[]ports.UpdaterService{uploader},
		nil,
		scanner,
		cmdBuilder,
		immediateReady{},
	)

	go ritual.Listen(r.ctx)
	time.Sleep(20 * time.Millisecond)

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
	waitForIntegrationStatus(t, r.ch, app.Done, 10*time.Second)
}

func (r *testRitual) waitFailed(t *testing.T) {
	t.Helper()
	waitForIntegrationStatus(t, r.ch, app.Failed, 10*time.Second)
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

// ---------- seed helpers ----------

func seedRemoteWorld(t *testing.T, r *testRitual, files ...testFile) {
	t.Helper()
	seedFiles(t, r.remoteDir, files)
	updateManifestXXHash(t, r.ctx, r.remoteDir, r.remoteManifests, config.WorldsDir,
		func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState })
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
	localManifest.Worlds.SyncState.XXHashMap = xxhashMap
	localManifest.Worlds.SyncState.XXHashSyncAt = time.Now()
	require.NoError(t, r.localManifests.Save(r.ctx, localManifest), "save local manifest for synced seed")

	remoteManifest, err := r.remoteManifests.Get(r.ctx)
	require.NoError(t, err, "get remote manifest for synced seed")
	remoteManifest.Worlds.SyncState.XXHashMap = xxhashMap
	remoteManifest.Worlds.SyncState.XXHashSyncAt = time.Now()
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
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755), "create parent dirs for %s", f.path)
		require.NoError(t, os.WriteFile(fullPath, f.content, 0644), "write seed file %s", f.path)
	}
}

func seedBackups(t *testing.T, r *testRitual, count int) {
	t.Helper()
	for i := range count {
		ts := time.Now().Add(time.Duration(-count+i) * time.Hour).UTC().Format(config.TimestampFormat)

		localBackupDir := filepath.Join(r.localDir, config.BackupsDir, ts)
		require.NoError(t, os.MkdirAll(localBackupDir, 0755), "create local backup dir %s", ts)
		require.NoError(t, os.WriteFile(filepath.Join(localBackupDir, config.ManifestFilename), []byte("{}"), 0644), "write local backup manifest %s", ts)

		remoteBackupDir := filepath.Join(r.remoteDir, config.BackupsDir, ts)
		require.NoError(t, os.MkdirAll(remoteBackupDir, 0755), "create remote backup dir %s", ts)
		require.NoError(t, os.WriteFile(filepath.Join(remoteBackupDir, config.ManifestFilename), []byte("{}"), 0644), "write remote backup manifest %s", ts)
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

// ---------- startRitualWithRetention ----------

func (r *testRitual) startRitualWithRetention(t *testing.T) *fakeServer {
	t.Helper()
	server := r.fakerun()

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	_ = os.MkdirAll(worldsPath, 0755)

	worldsFS := os.DirFS(worldsPath)
	worldScanner := adapters.NewFullScanner(worldsFS)

	localStaging := filepath.Join(t.TempDir(), "staging")
	remoteStaging := fmt.Sprintf("sync/test-%d", time.Now().UnixNano())

	worldSync := services.NewSyncService(
		worldScanner, r.local, r.remote, r.bus,
		services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
		filepath.Join(localStaging, config.WorldsDir),
		remoteStaging+"/"+config.WorldsDir,
	)

	worldDown := services.NewSyncDownloadUpdater(
		worldSync, r.localManifests, r.remoteManifests,
		func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState },
	)
	worldUp := services.NewSyncUploader(
		worldSync, r.localManifests, r.remoteManifests,
		func(m *domain.Manifest) *domain.SyncState { return &m.Worlds.SyncState },
	)

	rules := domain.DefaultRetentionRules()
	localRetention, err := services.NewRetention(r.local, rules, config.BackupsDir, services.ParseTimestampDir)
	require.NoError(t, err)
	remoteRetention, err := services.NewRetention(r.remote, rules, config.BackupsDir, services.ParseTimestampDir)
	require.NoError(t, err)

	cmdBuilder := &fakeServerCmdBuilder{server: server}

	rit := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		nil,
		[]ports.UpdaterService{worldDown},
		[]ports.UpdaterService{worldUp},
		[]ports.RetentionService{localRetention, remoteRetention},
		worldScanner,
		cmdBuilder,
		immediateReady{},
	)

	go rit.Listen(r.ctx)
	time.Sleep(20 * time.Millisecond)
	r.bus.Publish(app.StartRequested{})
	return server
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

func (r *testRitual) retentionLimit() int {
	return domain.DefaultRetentionRules().KeepLast
}

// ---------- condition helpers ----------

type alwaysFailCondition struct {
	reason string
}

func (c alwaysFailCondition) Check(_ context.Context) error {
	return fmt.Errorf("%s", c.reason)
}

func failCondition(reason string) alwaysFailCondition {
	return alwaysFailCondition{reason: reason}
}

func manifestLockCondition(t *testing.T, r *testRitual) ports.ConditionService {
	t.Helper()
	cond, err := services.NewManifestLockCondition(r.remoteManifests)
	require.NoError(t, err, "create manifest lock condition")
	return cond
}

// ---------- failOnceUpdater ----------

type failOnceIntegrationUpdater struct {
	calls int
}

func (f *failOnceIntegrationUpdater) Run(_ context.Context) error {
	f.calls++
	if f.calls == 1 {
		return fmt.Errorf("simulated transient failure")
	}
	return nil
}

// ---------- fakeServer wait helper ----------

func (s *fakeServer) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for s.stdin == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fakerun stdin to be connected")
		default:
			time.Sleep(10 * time.Millisecond)
		}
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

	ritual.startRitualWithConditions(t, failCondition("insufficient disk space"))
	ritual.waitFailed(t)

	ritual.assertLocalFileMissing(t, "world/level.dat",
		"condition failed at checking — no files should be downloaded")
	ritual.assertManifestUnlocked(t,
		"condition failed before lock — both manifests should remain unlocked")
}

func TestIntegration_ManifestLocked_RejectStart(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteManifest(t, ritual, &domain.Manifest{LockedBy: "other-host"})
	ritual.startRitualWithConditions(t, manifestLockCondition(t, ritual))
	ritual.waitFailed(t)

	ritual.assertManifestLockedBy(t, "other-host",
		"remote manifest should still be locked by original host — we should not touch it")
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

func (r *testRitual) startRitualWithFlakyUpdater(t *testing.T, flaky ports.UpdaterService) *fakeServer {
	t.Helper()
	server := r.fakerun()
	cmdBuilder := &fakeServerCmdBuilder{server: server}

	rit := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		nil,
		[]ports.UpdaterService{flaky},
		nil, nil, nil,
		cmdBuilder,
		immediateReady{},
	)

	go rit.Listen(r.ctx)
	time.Sleep(20 * time.Millisecond)
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
	ritual.waitFailed(t)

	ritual.assertManifestUnlocked(t,
		"lock must be released after graceful stop")
}

func TestIntegration_FetchFails_RetrySucceeds(t *testing.T) {
	ritual := newRitual(t)

	flaky := &failOnceIntegrationUpdater{}
	server := ritual.startRitualWithFlakyUpdater(t, flaky)
	ritual.waitFailed(t)

	ritual.sendRetry()
	server.waitReady(t)
	server.stdin.Close()
	ritual.waitDone(t)

	assert.Equal(t, 2, flaky.calls,
		"updater should be called twice — fail on first, succeed on retry")
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

func TestIntegration_BackupCreated_ContainsPreRunState(t *testing.T) {
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
	ritual.assertBackupFileContent(t, backupName, "worlds/world/level.dat", []byte("before run"),
		"backup should contain pre-run snapshot, not post-mutation content")
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

func TestIntegration_RetentionPrunesOldBackups(t *testing.T) {
	ritual := newRitual(t)

	seedBackups(t, ritual, 5)
	seedRemoteWorld(t, ritual,
		file("world/level.dat", []byte("level")),
	)

	server := ritual.startRitualWithRetention(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("changed"))
	server.exit(0)
	server.stdin.Close()
	ritual.waitDone(t)

	ritual.assertBackupCount(t, ritual.retentionLimit(),
		"retention should prune oldest backups, keeping only N most recent")
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

func (b *liveSyncCmdBuilder) Build(_ context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command(b.binary, "--root", b.root)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd, nil
}

// ---------- startRitualWithLiveSync ----------

func (r *testRitual) startRitualWithLiveSync(t *testing.T) {
	t.Helper()

	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0755), "create worlds dir")

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

	cmdBuilder := &liveSyncCmdBuilder{binary: fakerunBin, root: r.localDir}

	_, stopHeartbeat := heartbeat.Attach(r.bus, r.localManifests, r.remoteManifests, syncSvc)
	t.Cleanup(stopHeartbeat)

	rit := app.New(
		r.bus,
		r.local, r.remote,
		r.localManifests, r.remoteManifests,
		nil,
		[]ports.UpdaterService{downloader},
		[]ports.UpdaterService{uploader},
		nil,
		scanner,
		cmdBuilder,
		immediateReady{},
	)

	go rit.Listen(r.ctx)
	time.Sleep(20 * time.Millisecond)
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
			if _, ok := e.(ports.SaveCompleted); ok {
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
				if _, ok := e.(ports.SaveCompleted); ok {
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
	remoteManifest.Lease.HeartbeatInterval = domain.Duration(200 * time.Millisecond)
	remoteManifest.Lease.TTL = domain.Duration(2 * time.Second)

	worldsPath := filepath.Join(r.remoteDir, config.WorldsDir)
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	xxhashMap, err := scanner.Scan(r.ctx)
	require.NoError(t, err, "scan worlds for short heartbeat seed")
	remoteManifest.Worlds.SyncState.XXHashMap = xxhashMap
	remoteManifest.Worlds.SyncState.XXHashSyncAt = time.Now()

	require.NoError(t, r.remoteManifests.Save(r.ctx, remoteManifest), "save remote manifest with short heartbeat")
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

	waitForSaveCompletedThenStatus(t, ch, 2, ritual.sendStop, app.Failed, 10*time.Second)

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

	waitForSaveCompletedThenStatus(t, ch, 2, ritual.sendStop, app.Failed, 10*time.Second)

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
	remoteManifest.Lease.HeartbeatInterval = domain.Duration(200 * time.Millisecond)
	remoteManifest.Lease.TTL = domain.Duration(2 * time.Second)
	remoteManifest.Worlds.SyncState.XXHashSyncAt = time.Now().Add(-time.Hour)
	remoteManifest.Worlds.SyncState.XXHashMap = map[string]string{
		"world/level.dat": "remote-hash-old",
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
	localManifest.Worlds.SyncState.XXHashSyncAt = time.Now()
	localManifest.Worlds.SyncState.XXHashMap = localXXHash
	require.NoError(t, ritual.localManifests.Save(ritual.ctx, localManifest), "save local manifest for crash recovery")

	ch, unsub := ritual.bus.Subscribe()
	defer unsub()

	ritual.startRitualWithLiveSync(t)

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Failed, 10*time.Second)

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

	waitForSaveCompleted(t, ch, 1, 10*time.Second)

	worldsPath := filepath.Join(ritual.localDir, config.WorldsDir)
	newFilePath := filepath.Join(worldsPath, "world", "playerdata", "player1.dat")
	require.NoError(t, os.MkdirAll(filepath.Dir(newFilePath), 0755), "create playerdata dir")
	require.NoError(t, os.WriteFile(newFilePath, []byte("player one"), 0644), "write new player file")

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Failed, 10*time.Second)

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

	waitForSaveCompletedThenStatus(t, ch, 1, ritual.sendStop, app.Failed, 10*time.Second)

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

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			ritual.sendStop()
			t.Fatal("timed out waiting for save-off confirmation — autosaves should be disabled when server becomes ready")
		case e, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed while waiting for save-off confirmation")
			}
			if out, ok := e.(ports.ServerOutputInfo); ok && strings.Contains(out.Line, "Automatic saving is now disabled") {
				ritual.sendStop()
				waitForIntegrationStatus(t, ch, app.Failed, 10*time.Second)
				return
			}
		}
	}
}
