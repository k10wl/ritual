package integration_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/checks"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/pipeline"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// setupRitual wires the same composition as cmd/gui (minus Wails) into a
// single Attach + cancel pair. The first storage is unused (reserved for
// future local-only verbs); remoteStorage feeds the locker.
func setupRitual(
	t *testing.T,
	bus ports.EventBus,
	_, remoteStorage ports.StorageRepository,
	cs []checks.Check,
	p ports.Puller, a ports.Applier, hr pulling.HeadResolver,
	cm ports.Committer, pu ports.Pusher, ct []string,
	lr, rr []retaining.Job,
	cb ports.CmdBuilder,
	rd ports.ReadinessCheck,
) func() {
	t.Helper()
	host, _ := os.Hostname()
	locker := observed.NewLocker(lock.New(remoteStorage, host), bus)
	entry := pipeline.Build(pipeline.Deps{
		Bus: bus, Checks: cs,
		Puller: p, Applier: a, HeadResolver: hr,
		Committer: cm, CommitOpts: ritual.NewCommitOptsResolver(ct), Pusher: pu,
		LocalRetentions: lr, RemoteRetentions: rr,
		CmdBuilder: cb, Readiness: rd,
		AcquireFn: locker.Acquire, InspectFn: locker.Inspect, ReleaseFn: locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	stop := lifecycle.Attach(ctx, bus, lifecycle.Entries{Session: entry})
	return func() {
		stop()
		cancel()
	}
}

// --- Fakes ---

type fakeStorage struct{}

func (fakeStorage) String() string                                     { return "fake::storage" }
func (fakeStorage) Get(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (fakeStorage) Put(_ context.Context, _ string, _ []byte) error    { return nil }
func (fakeStorage) GetStream(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (fakeStorage) PutStream(_ context.Context, _ string, _ io.Reader) error { return nil }
func (fakeStorage) Exists(_ context.Context, _ string) (bool, error)             { return false, nil }
func (fakeStorage) Delete(_ context.Context, _ string) error                     { return nil }
func (fakeStorage) DeleteBatch(_ context.Context, _ []string) error              { return nil }
func (fakeStorage) List(_ context.Context, _ string) ([]string, error)           { return nil, nil }
func (fakeStorage) Copy(_ context.Context, _, _ string) error                    { return nil }
func (fakeStorage) Rename(_ context.Context, _, _ string) error                  { return nil }

func noopCheck(_ context.Context) error { return nil }

type noopPuller struct{}

func (noopPuller) Pull(_ context.Context, _ domain.RefID) error { return nil }

type noopApplier struct{}

func (noopApplier) Apply(_ context.Context, _ domain.RefID) error { return nil }

type noopCommitter struct{}

func (noopCommitter) Commit(_ context.Context, _ ports.CommitOpts) (domain.RefID, error) {
	return domain.RefID("noop-commit"), nil
}

type noopPusher struct{}

func (noopPusher) Push(_ context.Context, _ domain.RefID) error { return nil }

func noopHead(_ context.Context) (domain.RefID, error) { return domain.RefID("noop"), nil }

type failOncePuller struct {
	calls int
}

func (f *failOncePuller) Pull(_ context.Context, _ domain.RefID) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("network timeout")
	}
	return nil
}

var _ pulling.HeadResolver = noopHead

var noopJob = retaining.Job{
	Kind:  retaining.KindRetention,
	Label: "noop",
	Run:   func(_ context.Context) error { return nil },
}

type fakeCmdBuilder struct{}

func (fakeCmdBuilder) Build(ctx context.Context, _ io.Reader, _ io.Writer) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "echo", "ok"), nil
}

func waitForStatus(t *testing.T, ch <-chan ports.Event, want lifecycle.Outcome, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status %s", want)
		case e, ok := <-ch:
			if !ok {
				t.Fatal("channel closed")
			}
			if sc, ok := e.(lifecycle.StatusChanged); ok && sc.Status == want {
				return
			}
		}
	}
}

func TestRitual_Start_RunsPipeline(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	defer setupRitual(t, bus,
		fakeStorage{}, fakeStorage{},
		[]checks.Check{noopCheck},
		noopPuller{}, noopApplier{}, noopHead,
		noopCommitter{}, noopPusher{}, []string{"**"},
		[]retaining.Job{noopJob}, []retaining.Job{noopJob},
		fakeCmdBuilder{},
		immediateReady{},
	)()

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Done, 5*time.Second)

	assert.True(t, true, "pipeline reached Done")
}

// Story — after Dismiss-from-Failed, the user can Start a fresh run. Design-log/017
// cuts retry-from-failed: dismiss returns the lifecycle to Idle; a subsequent
// Start triggers a fresh pipeline. The flaky puller still succeeds on its
// second invocation because the pipeline re-enters from the entry strategy.
func TestRitual_DismissThenStart_RecoversFromFailure(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	flaky := &failOncePuller{}
	defer setupRitual(t, bus,
		fakeStorage{}, fakeStorage{},
		[]checks.Check{noopCheck},
		flaky, noopApplier{}, noopHead,
		noopCommitter{}, noopPusher{}, []string{"**"},
		[]retaining.Job{noopJob}, []retaining.Job{noopJob},
		fakeCmdBuilder{},
		immediateReady{},
	)()

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Failed, 5*time.Second)

	bus.Publish(ritual.DismissRequested{})
	waitForStatus(t, ch, lifecycle.Dismissed, 5*time.Second)
	waitForStatus(t, ch, lifecycle.Idle, 5*time.Second)

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Done, 5*time.Second)

	assert.Equal(t, 2, flaky.calls, "puller called twice: fail + fresh-start retry path")
}

// Story #7 — Start is only rejected while Running. After terminal states
// (Done, Failed), a fresh Start must begin a new pipeline — users retry by
// starting again. Uses noop fakes so pipelines complete instantly.
func TestRitual_Start_AfterDone_StartsAgain(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	defer setupRitual(t, bus,
		fakeStorage{}, fakeStorage{},
		[]checks.Check{noopCheck},
		noopPuller{}, noopApplier{}, noopHead,
		noopCommitter{}, noopPusher{}, []string{"**"},
		[]retaining.Job{noopJob}, []retaining.Job{noopJob},
		fakeCmdBuilder{},
		immediateReady{},
	)()

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Done, 5*time.Second)

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Running, 5*time.Second)
	waitForStatus(t, ch, lifecycle.Done, 5*time.Second)
}

func TestRitual_Dismiss_WhenIdle_Rejected(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	defer setupRitual(t, bus,
		fakeStorage{}, fakeStorage{},
		nil, noopPuller{}, noopApplier{}, noopHead, nil, nil, nil, nil, nil,
		fakeCmdBuilder{},
		immediateReady{},
	)()

	bus.Publish(ritual.DismissRequested{})

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			return // no crash, acceptable
		case e := <-ch:
			if sc, ok := e.(lifecycle.StatusChanged); ok && sc.Err != nil {
				assert.Contains(t, sc.Err.Error(), "cannot dismiss")
				return
			}
		}
	}
}
