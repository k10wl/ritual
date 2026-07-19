package loadedref_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/stages/committing"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/loadedref"
	"sync"
	"testing"
	"time"
)

// inMemSettings is a Loader/Saver pair sharing one Settings value through a
// mutex. Substitutes for domain.LoadSettings + Settings.Save so the test
// stays off disk.
type inMemSettings struct {
	mu sync.Mutex
	s  domain.Settings
}

func (m *inMemSettings) Load() (*domain.Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := m.s
	return &cp, nil
}

func (m *inMemSettings) Save(s *domain.Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s = *s
	return nil
}

func (m *inMemSettings) LoadedRefID() domain.RefID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.s.LoadedRefID
}

func waitFor(t *testing.T, deadline time.Time, want domain.RefID, get func() domain.RefID) {
	t.Helper()
	for time.Now().Before(deadline) {
		if get() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("LoadedRefID did not reach %q within deadline (got %q)", want, get())
}

func TestAttach_TracksHeadResolved(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{}
	stop := loadedref.Attach(t.Context(), bus, store.Load, store.Save)
	defer stop()

	bus.Publish(pulling.HeadResolvedInfo{RefID: domain.RefID("R-PULL")})
	waitFor(t, time.Now().Add(time.Second), "R-PULL", store.LoadedRefID)
}

func TestAttach_TracksCommitted(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{}
	stop := loadedref.Attach(t.Context(), bus, store.Load, store.Save)
	defer stop()

	bus.Publish(committing.CommittedInfo{RefID: domain.RefID("R-COMMIT")})
	waitFor(t, time.Now().Add(time.Second), "R-COMMIT", store.LoadedRefID)
}

func TestAttach_LatestEventWins(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{}
	stop := loadedref.Attach(t.Context(), bus, store.Load, store.Save)
	defer stop()

	// Restore lands first (workdir ← target), then a Publish lands later
	// (workdir captured into a new ref). LoadedRefID must follow the latest
	// workdir-shaping event without operator intervention.
	bus.Publish(pulling.HeadResolvedInfo{RefID: domain.RefID("R-RESTORE")})
	waitFor(t, time.Now().Add(time.Second), "R-RESTORE", store.LoadedRefID)
	bus.Publish(committing.CommittedInfo{RefID: domain.RefID("R-PUBLISH")})
	waitFor(t, time.Now().Add(time.Second), "R-PUBLISH", store.LoadedRefID)
}

func TestAttach_EmptyRefIDIgnored(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{s: domain.Settings{LoadedRefID: "PREV"}}
	stop := loadedref.Attach(t.Context(), bus, store.Load, store.Save)
	defer stop()

	// A defensive event with an empty RefID must not blank the field — the
	// field is a hint for the badge; preserving the last known value beats
	// going silent.
	bus.Publish(pulling.HeadResolvedInfo{RefID: ""})
	bus.Publish(committing.CommittedInfo{RefID: ""})
	// Give the consumer a moment to drain.
	time.Sleep(20 * time.Millisecond)
	if got := store.LoadedRefID(); got != "PREV" {
		t.Fatalf("empty RefID changed LoadedRefID: got %q, want %q", got, "PREV")
	}
}

func TestAttach_LoadErrorSwallowed(t *testing.T) {
	bus := adapters.NewEventBus(8)
	loadErr := errors.New("read settings: boom")
	called := make(chan struct{}, 1)
	stop := loadedref.Attach(t.Context(), bus,
		func() (*domain.Settings, error) { return nil, loadErr },
		func(*domain.Settings) error { called <- struct{}{}; return nil },
	)
	defer stop()
	bus.Publish(pulling.HeadResolvedInfo{RefID: "R"})
	// Should NOT crash, should NOT call Save (no Settings to write into).
	select {
	case <-called:
		t.Fatal("Save called despite a failed Load")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAttach_StopIsIdempotent(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{}
	stop := loadedref.Attach(t.Context(), bus, store.Load, store.Save)
	stop()
	stop() // must not panic on second call
}

func TestAttach_StopOnCtxCancel(t *testing.T) {
	bus := adapters.NewEventBus(8)
	store := &inMemSettings{}
	ctx, cancel := context.WithCancel(t.Context())
	stop := loadedref.Attach(ctx, bus, store.Load, store.Save)
	cancel()
	// Stop should return promptly after the goroutine sees ctx.Done().
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not return after ctx cancel")
	}
}
