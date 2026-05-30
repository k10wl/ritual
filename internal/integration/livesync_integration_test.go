// Live-sync integration tests (design-log/016 Phase 6). Same rules as
// ritual_integration_test.go: no comments inside bodies, named helpers,
// assertion messages carry the why.
package integration_test

import (
	"context"
	"errors"
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
	"ritual/internal/core/ritual"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/livesync"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/pipeline"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tickAwareCmdBuilder multiplexes running.coordinate's stdin writes
// (save-off, save-all flush, stop) with the test's direct instructions
// onto the same fakerun stdin. The base fakeServerCmdBuilder ignores
// the passed-in stdin reader — that's fine for tests that never publish
// SaveRequested, but live-sync tests REQUIRE the save handshake to
// reach fakerun. This builder forwards both streams concurrently.
type tickAwareCmdBuilder struct {
	server *fakeServer
}

func (b *tickAwareCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	b.server.ready <- pw
	go func() { _, _ = io.Copy(pw, stdin) }()

	cmd := exec.CommandContext(ctx, b.server.binary, "--root", b.server.root)
	cmd.Stdin = pr
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd, nil
}

// startRitualWithLiveSync mirrors startRitualFull but wires the
// design-log/016 live-sync ticker, dispatcher, and drain barrier. Short
// tickInterval keeps tests under a few hundred ms while still exercising
// multi-tick amend chains.
func (r *testRitual) startRitualWithLiveSync(t *testing.T, tickInterval, saveTimeout, drainTimeout time.Duration) *fakeServer {
	t.Helper()

	server := r.fakerun()
	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create worlds dir")

	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	preflightChecks := []checks.Check{
		r.localDivergenceCheck(scanner),
		r.remoteDivergenceCheck(),
	}

	cmdBuilder := &tickAwareCmdBuilder{server: server}

	puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)
	committer, pusher, commitTargets := r.buildCommittingVerbs(t, worldsPath, scanner)

	host, _ := os.Hostname()
	localLocker := observed.NewLocker(lock.New(r.local, host), r.bus)
	remoteLocker := observed.NewLocker(lock.New(r.remote, host), r.bus)
	locker := lock.NewBoth(localLocker, remoteLocker)

	parentFn, stopParent := livesync.ParentFromBus(r.bus)
	t.Cleanup(stopParent)
	ticker, engine, stopTicker := livesync.New(
		r.bus, committer, pusher, commitTargets, parentFn,
		tickInterval, saveTimeout,
	)
	t.Cleanup(stopTicker)
	dispatcher, stopDispatcher := livesync.NewDispatcher(r.bus, nil)
	t.Cleanup(stopDispatcher)
	drainer := livesync.NewDrainer(ticker, engine, dispatcher, drainTimeout)

	entry := pipeline.Build(pipeline.Deps{
		Bus: r.bus, Checks: preflightChecks,
		Puller: puller, Applier: applier, HeadResolver: headResolver,
		Committer: committer, CommitOpts: ritual.NewCommitOptsResolver(commitTargets), Pusher: pusher,
		LocalRetentions: r.localRetentions, RemoteRetentions: r.remoteRetentions,
		CmdBuilder: cmdBuilder, Readiness: immediateReady{},
		AcquireFn: locker.Acquire, InspectFn: locker.Inspect, ReleaseFn: locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
		Drainable:         drainer,
	})

	loggingStop, err := logging.Build(r.bus, r.localRoot)
	require.NoError(t, err, "logging.Build must succeed for livesync integration harness")
	t.Cleanup(loggingStop)

	sessionHook := func(rs *ritual.RunState) {
		dispatcher.SetTarget(func(id domain.RefID) { rs.RefID = id })
	}
	stop := lifecycle.Attach(r.ctx, r.bus, lifecycle.Entries{Session: entry}, sessionHook)
	t.Cleanup(stop)
	r.bus.Publish(ritual.StartRequested{})
	return server
}

func collectLiveDraftEvents(t *testing.T, r *testRitual) func() []livesync.LiveDraftCommitted {
	t.Helper()
	ch, cancel := r.bus.Subscribe()
	var mu sync.Mutex
	var got []livesync.LiveDraftCommitted
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			if ev, ok := e.(livesync.LiveDraftCommitted); ok {
				mu.Lock()
				got = append(got, ev)
				mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return func() []livesync.LiveDraftCommitted {
		mu.Lock()
		defer mu.Unlock()
		return append([]livesync.LiveDraftCommitted(nil), got...)
	}
}

func listRemoteRefs(t *testing.T, r *testRitual) []string {
	t.Helper()
	keys, err := r.remote.List(r.ctx, "refs/")
	require.NoError(t, err, "list remote refs")
	return keys
}

// Story: full session past one tick interval — tick fires, publishes
// LiveDraftCommitted, commits and pushes a draft. Then graceful exit:
// the post-session committing.Strategy amends that draft into a single
// final ref. Verifies the full producer→amend→sweep chain end-to-end.
func TestIntegration_LiveSync_TickFires_AmendsIntoSingleFinalRef(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before live-sync session")),
	)
	beforeKeys := listRemoteRefs(t, r)
	drafts := collectLiveDraftEvents(t, r)

	server := r.startRitualWithLiveSync(t, 100*time.Millisecond, 5*time.Second, 2*time.Second)
	server.waitReady(t)

	server.write("worlds/world/level.dat", []byte("mid tick"))
	require.Eventually(t, func() bool {
		return len(drafts()) >= 1
	}, 3*time.Second, 20*time.Millisecond,
		"a tick must publish LiveDraftCommitted within 3s at 100ms interval — confirms the producer fires end-to-end through running.coordinate's save handshake")

	tickDraftID := drafts()[0].RefID

	server.write("worlds/world/level.dat", []byte("final"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	afterKeys := listRemoteRefs(t, r)
	assert.Len(t, afterKeys, len(beforeKeys)+1,
		"after a tick + graceful exit, remote must have seeded ref + exactly ONE new ref — the tick draft must have been amended into the final ref, not stacked alongside it (sweepSupersededSiblings)")

	for _, key := range afterKeys {
		if key == "refs/"+string(tickDraftID)+".json" {
			t.Fatalf("remote still carries the tick draft %s — sweep on amend did not fire", tickDraftID)
		}
	}
}

// Story: a session that runs less than one tick interval and exits.
// No tick fires; the post-session commit is a fresh commit (not amend),
// remote ends with seeded + final.
func TestIntegration_LiveSync_ShortSession_NoTickFired(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("short session before")),
	)
	beforeKeys := listRemoteRefs(t, r)
	getDrafts := collectLiveDraftEvents(t, r)

	server := r.startRitualWithLiveSync(t, time.Hour, 5*time.Second, 2*time.Second)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("short session after"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	afterKeys := listRemoteRefs(t, r)
	assert.Len(t, afterKeys, len(beforeKeys)+1,
		"sub-interval session must add exactly one ref via the post-session fresh commit path — no tick draft ever existed")

	if drafts := getDrafts(); len(drafts) != 0 {
		t.Fatalf("no tick should fire under interval=1h, got %d LiveDraftCommitted", len(drafts))
	}
}

// Story: server crash (non-zero exit) routes Running → Unlocking,
// bypassing Committing entirely. If a tick had already pushed, that
// draft remains the remote HEAD; if not, the seeded ref stays HEAD.
// Either way, no fresh post-session commit happens.
func TestIntegration_LiveSync_ServerCrash_NoPostSessionCommit(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before crash session")),
	)
	beforeKeys := listRemoteRefs(t, r)

	server := r.startRitualWithLiveSync(t, time.Hour, 5*time.Second, 2*time.Second)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("mid crash"))
	server.exit(1)
	server.stdin.Close()
	r.waitFailed(t)

	afterKeys := listRemoteRefs(t, r)
	assert.Equal(t, len(beforeKeys), len(afterKeys),
		"server crash (exit != 0) must NOT add a new ref to remote — Running routes directly to Unlocking, post-session Committing is skipped; without any tick pushing during this session the seeded ref remains the sole HEAD")
}

// Story: failing pusher (network unreachable surrogate) — ticks commit
// locally but never reach remote. Post-session push also fails. Remote
// keeps the seeded ref as HEAD; the session ends in Failed status.
// Asserts the "honest durability promise" of design §amend-gap-fix —
// tick failures don't crash the server or break shutdown.
func TestIntegration_LiveSync_FailingPush_RemoteUnchangedSessionFails(t *testing.T) {
	gate := newSwitchableRefPutInjector()
	r := newRitualWith(t, func(inner ports.StorageRepository) ports.StorageRepository {
		gate.StorageRepository = inner
		return gate
	})

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before failing-push session")),
	)
	beforeKeys := listRemoteRefs(t, r)
	gate.failAll.Store(true)

	server := r.startRitualWithLiveSync(t, 100*time.Millisecond, 5*time.Second, 2*time.Second)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("written but never reaches remote"))
	time.Sleep(250 * time.Millisecond)
	server.exit(0)
	server.stdin.Close()
	r.waitFailed(t)

	afterKeys := listRemoteRefs(t, r)
	assert.Equal(t, len(beforeKeys), len(afterKeys),
		"every remote ref PUT fails — the remote ref set must be unchanged from the seed; design §Flow 9 (offline session): tick keeps local commits, remote catches up only when push succeeds")
}

// switchableRefPutInjector fails refs/ PutStreams iff failAll is set.
// Seed phase runs with failAll=false (allows the seed ref); the test
// flips failAll=true to simulate offline play. Blob writes always pass
// through.
type switchableRefPutInjector struct {
	ports.StorageRepository
	failAll atomic.Bool
}

func newSwitchableRefPutInjector() *switchableRefPutInjector {
	return &switchableRefPutInjector{}
}

func (f *switchableRefPutInjector) PutStream(ctx context.Context, key string, body io.Reader) error {
	if strings.HasPrefix(key, "refs/") && f.failAll.Load() {
		return errors.New("simulated persistent remote refs/ PutStream failure")
	}
	return f.StorageRepository.PutStream(ctx, key, body)
}
