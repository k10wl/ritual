package app_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/stretchr/testify/assert"
)

// --- Fakes ---

type fakeStorage struct{}

func (fakeStorage) Get(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (fakeStorage) Put(_ context.Context, _ string, _ []byte) error    { return nil }
func (fakeStorage) Delete(_ context.Context, _ string) error           { return nil }
func (fakeStorage) DeleteBatch(_ context.Context, _ []string) error    { return nil }
func (fakeStorage) List(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (fakeStorage) Copy(_ context.Context, _, _ string) error          { return nil }

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

func (fakeCmdBuilder) Build(_ context.Context) (*exec.Cmd, error) {
	return exec.Command("echo", "ok"), nil
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
		fakeCmdBuilder{},
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
