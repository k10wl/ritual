package heartbeat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ritual/internal/adapters"
	"ritual/internal/core/lock"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/heartbeat"
	"sync"
	"testing"
	"time"
)

func acquireFor(t *testing.T, store *leaseStore) (*lock.Locker, string) {
	t.Helper()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(context.Background())
	if err != nil {
		t.Fatalf("setup Acquire: %v", err)
	}
	return locker, sessionID
}

func TestSupervisorBeatsAfterAcquire(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)
	initial := store.heartbeatAt(t)

	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, locker.Heartbeat)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 50 * time.Millisecond})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if store.heartbeatAt(t).After(initial) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("heartbeat never refreshed remote lease payload")
}

func TestSupervisorPublishesLostOnSessionMismatch(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)
	store.seedLive(t, "hostB", "sess-bob", time.Hour)

	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	_, stop := heartbeat.Attach(bus, locker.Heartbeat)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 30 * time.Millisecond})

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("did not observe LockLostInfo")
		case e := <-ch:
			if lost, ok := e.(ritual.LockLostInfo); ok && lost.RunID == "run-1" {
				if lost.Reason != "taken_over" {
					t.Fatalf("reason: %s", lost.Reason)
				}
				return
			}
		}
	}
}

func TestSupervisorStopsOnReleased(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, locker.Heartbeat)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	time.Sleep(80 * time.Millisecond)
	bus.Publish(ritual.LockReleasedInfo{RunID: "run-1"})
	time.Sleep(30 * time.Millisecond)

	before := store.putCount()
	time.Sleep(150 * time.Millisecond)
	after := store.putCount()
	if after != before {
		t.Fatalf("beats continued after release: before=%d after=%d", before, after)
	}
}

func TestSupervisorStopsAllOnServerStopped(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, locker.Heartbeat)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	time.Sleep(60 * time.Millisecond)
	bus.Publish(running.ServerStoppedInfo{})
	time.Sleep(30 * time.Millisecond)

	before := store.putCount()
	time.Sleep(120 * time.Millisecond)
	after := store.putCount()
	if after != before {
		t.Fatalf("beats continued after ServerStoppedInfo: before=%d after=%d", before, after)
	}
}

func TestSupervisorStopsAllOnServerCrashed(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	bus := adapters.NewEventBus(16)
	_, stop := heartbeat.Attach(bus, locker.Heartbeat)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	time.Sleep(60 * time.Millisecond)
	bus.Publish(running.ServerCrashedInfo{})
	time.Sleep(30 * time.Millisecond)

	before := store.putCount()
	time.Sleep(120 * time.Millisecond)
	after := store.putCount()
	if after != before {
		t.Fatalf("beats continued after ServerCrashedInfo: before=%d after=%d", before, after)
	}
}

func TestSupervisorRoutesLeaseLostErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason string
	}{
		{"taken_over", lock.ErrLeaseTakenOver, "taken_over"},
		{"vanished", lock.ErrLeaseVanished, "vanished"},
		{"lease_lost", lock.ErrLeaseLost, "lease_lost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hb := func(_ context.Context, _ string) error { return tc.err }

			bus := adapters.NewEventBus(16)
			ch, cancel := bus.Subscribe()
			defer cancel()

			_, stop := heartbeat.Attach(bus, hb)
			defer stop()

			bus.Publish(ritual.LockAcquiredInfo{RunID: "run-x", SessionID: "sess-x", Interval: 20 * time.Millisecond})

			deadline := time.After(time.Second)
			for {
				select {
				case <-deadline:
					t.Fatalf("%s: did not observe LockLostInfo", tc.name)
				case e := <-ch:
					if lost, ok := e.(ritual.LockLostInfo); ok && lost.RunID == "run-x" {
						if lost.Reason != tc.reason {
							t.Fatalf("reason: got %s, want %s", lost.Reason, tc.reason)
						}
						return
					}
				}
			}
		})
	}
}

func TestSupervisorRoutesUnknownErrorAsErrorInfo(t *testing.T) {
	hb := func(_ context.Context, _ string) error { return errors.New("network down") }

	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	_, stop := heartbeat.Attach(bus, hb)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-x", SessionID: "sess-x", Interval: 20 * time.Millisecond})

	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("did not observe ErrorInfo")
		case e := <-ch:
			if ei, ok := e.(ritual.ErrorInfo); ok && ei.Operation == "heartbeat" {
				return
			}
		}
	}
}

func TestSupervisorAttachCancelStopsConsumer(t *testing.T) {
	bus := adapters.NewEventBus(16)
	hb := func(_ context.Context, _ string) error { return nil }

	_, stop := heartbeat.Attach(bus, hb)
	stop() // must return promptly without deadlock
}

// --- leaseStore — in-memory ports.StorageRepository that also counts puts ---

type leaseStore struct {
	mu    sync.Mutex
	items map[string][]byte
	puts  int
}

func newLeaseStore() *leaseStore {
	return &leaseStore{items: map[string][]byte{}}
}

func (s *leaseStore) String() string { return "mem::lease" }

func (s *leaseStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

func (s *leaseStore) heartbeatAt(t *testing.T) time.Time {
	t.Helper()
	s.mu.Lock()
	data, ok := s.items[lock.Key]
	s.mu.Unlock()
	if !ok {
		return time.Time{}
	}
	var p decodedPayload
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("decode lease payload: %v", err)
	}
	return p.HeartbeatAt
}

func (s *leaseStore) seedLive(t *testing.T, owner, sessionID string, ttl time.Duration) {
	t.Helper()
	now := time.Now()
	p := decodedPayload{
		Owner:       owner,
		SessionID:   sessionID,
		AcquiredAt:  now,
		HeartbeatAt: now,
		ExpiresAt:   now.Add(ttl),
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal seed payload: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[lock.Key] = data
}

func (s *leaseStore) GetStream(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	data, ok := s.items[key]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("leaseStore: key %q not found", key)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), data...))), nil
}

func (s *leaseStore) PutStream(_ context.Context, key string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = data
	s.puts++
	return nil
}

func (s *leaseStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key]
	return ok, nil
}

func (s *leaseStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}

func (s *leaseStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("leaseStore: Get deprecated")
}

func (s *leaseStore) Put(_ context.Context, _ string, _ []byte) error {
	return errors.New("leaseStore: Put deprecated")
}

func (s *leaseStore) DeleteBatch(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := s.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (s *leaseStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for k := range s.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *leaseStore) Copy(_ context.Context, _ string, _ string) error {
	return errors.New("leaseStore: Copy unused")
}

func (s *leaseStore) Rename(_ context.Context, _ string, _ string) error {
	return errors.New("leaseStore: Rename unused")
}

type decodedPayload struct {
	Owner       string    `json:"owner"`
	SessionID   string    `json:"sessionId"`
	AcquiredAt  time.Time `json:"acquiredAt"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
