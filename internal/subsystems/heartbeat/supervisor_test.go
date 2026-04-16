package heartbeat_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/subsystems/heartbeat"
)

func TestSupervisorBeatsAfterAcquire(t *testing.T) {
	var mu sync.Mutex
	current := &domain.Manifest{LockedBy: "run-1"}

	store := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			mu.Lock()
			defer mu.Unlock()
			return cloneForTest(current), nil
		},
		SaveFunc: func(_ context.Context, m *domain.Manifest) error {
			mu.Lock()
			defer mu.Unlock()
			current = cloneForTest(m)
			return nil
		},
	}
	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, store)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 50 * time.Millisecond})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		beat := current.HeartbeatAt
		mu.Unlock()
		if !beat.IsZero() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("heartbeat never beat")
}

func TestSupervisorPublishesLostOnOwnerMismatch(t *testing.T) {
	current := &domain.Manifest{LockedBy: "someone-else"}
	store := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) { return current, nil },
	}
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	_, stop := heartbeat.Attach(bus, store)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 30 * time.Millisecond})

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("did not observe LockLostInfo")
		case e := <-ch:
			if lost, ok := e.(ports.LockLostInfo); ok && lost.RunID == "run-1" {
				if lost.Reason != "owner_mismatch" {
					t.Fatalf("reason: %s", lost.Reason)
				}
				return
			}
		}
	}
}

func TestSupervisorStopsOnReleased(t *testing.T) {
	var saveCount int
	var mu sync.Mutex
	store := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "run-1"}, nil
		},
		SaveFunc: func(_ context.Context, _ *domain.Manifest) error {
			mu.Lock()
			saveCount++
			mu.Unlock()
			return nil
		},
	}
	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, store)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 20 * time.Millisecond})
	time.Sleep(80 * time.Millisecond)
	bus.Publish(ports.LockReleasedInfo{RunID: "run-1"})
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	before := saveCount
	mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	after := saveCount
	mu.Unlock()
	if after != before {
		t.Fatalf("beats continued after release: before=%d after=%d", before, after)
	}
}

func cloneForTest(m *domain.Manifest) *domain.Manifest {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}
