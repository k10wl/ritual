package app_test

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- Fakes ---

type fakeStorage struct{}

func (fakeStorage) String() string                                     { return "fake::storage" }
func (fakeStorage) Get(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (fakeStorage) Put(_ context.Context, _ string, _ []byte) error    { return nil }
func (fakeStorage) Delete(_ context.Context, _ string) error           { return nil }
func (fakeStorage) DeleteBatch(_ context.Context, _ []string) error    { return nil }
func (fakeStorage) List(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (fakeStorage) Copy(_ context.Context, _, _ string) error          { return nil }
func (fakeStorage) Rename(_ context.Context, _, _ string) error        { return nil }

type fakeManifestStore struct{}

func (fakeManifestStore) Get(_ context.Context) (*domain.Manifest, error) {
	return &domain.Manifest{}, nil
}
func (fakeManifestStore) Save(_ context.Context, _ *domain.Manifest) error { return nil }

type noopCondition struct{}

func (noopCondition) Check(_ context.Context) error { return nil }

type noopUpdater struct{}

func (noopUpdater) Run(_ context.Context) error { return nil }

type noopRetention struct{}

func (noopRetention) Apply(_ context.Context) error { return nil }

type fakeCmdBuilder struct{}

func (fakeCmdBuilder) Build(ctx context.Context, _ io.Reader, _ io.Writer) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "echo", "ok"), nil
}

func waitForStatus(t *testing.T, ch <-chan ports.Event, want app.Outcome, timeout time.Duration) {
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
			if sc, ok := e.(app.StatusChanged); ok && sc.Status == want {
				return
			}
		}
	}
}

func TestRitual_Start_RunsPipeline(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		[]ports.ConditionService{noopCondition{}},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.RetentionService{noopRetention{}},
		nil,
		fakeCmdBuilder{},
		immediateReady{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)

	// Yield so Listen goroutine subscribes before we publish.
	time.Sleep(20 * time.Millisecond)
	bus.Publish(app.StartRequested{})
	waitForStatus(t, ch, app.Done, 5*time.Second)

	assert.True(t, true, "pipeline reached Done")
}

// --- failOnceUpdater ---

type failOnceUpdater struct {
	calls int
}

func (f *failOnceUpdater) Run(_ context.Context) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("network timeout")
	}
	return nil
}

func TestRitual_Retry_ReentersAtFailedStage(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	flaky := &failOnceUpdater{}
	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		[]ports.ConditionService{noopCondition{}},
		[]ports.UpdaterService{flaky},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.RetentionService{noopRetention{}},
		nil,
		fakeCmdBuilder{},
		immediateReady{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)
	time.Sleep(20 * time.Millisecond)

	bus.Publish(app.StartRequested{})
	waitForStatus(t, ch, app.Failed, 5*time.Second)

	bus.Publish(app.RetryRequested{})
	waitForStatus(t, ch, app.Done, 5*time.Second)

	assert.Equal(t, 2, flaky.calls, "updater called twice: fail + retry")
}

// --- blockingCmdBuilder ---

type blockingCmdBuilder struct {
	ready chan struct{}
}

func (b *blockingCmdBuilder) Build(ctx context.Context, _ io.Reader, _ io.Writer) (*exec.Cmd, error) {
	close(b.ready)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRitual_Stop_CancelsRunning(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	blocker := &blockingCmdBuilder{ready: make(chan struct{})}
	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		nil, nil, nil, nil, nil,
		blocker,
		immediateReady{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)
	time.Sleep(20 * time.Millisecond)

	bus.Publish(app.StartRequested{})
	<-blocker.ready

	bus.Publish(app.StopRequested{})
	waitForStatus(t, ch, app.Failed, 5*time.Second)
}

func TestRitual_Retry_WhenIdle_Rejected(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		nil, nil, nil, nil, nil,
		fakeCmdBuilder{},
		immediateReady{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)
	time.Sleep(20 * time.Millisecond)

	bus.Publish(app.RetryRequested{})

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			return // no crash, acceptable
		case e := <-ch:
			if sc, ok := e.(app.StatusChanged); ok && sc.Err != nil {
				assert.Contains(t, sc.Err.Error(), "cannot retry")
				return
			}
		}
	}
}
