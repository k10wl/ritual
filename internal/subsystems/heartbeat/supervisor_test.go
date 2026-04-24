package heartbeat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/heartbeat"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// safeStore is a concurrency-safe ManifestStore for tests where both
// tick and syncTick access the same store from separate goroutines.
type safeStore struct {
	mu       sync.Mutex
	getFunc  func(context.Context) (*domain.Manifest, error)
	saveFunc func(context.Context, *domain.Manifest) error
}

func (s *safeStore) Get(ctx context.Context) (*domain.Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getFunc != nil {
		return s.getFunc(ctx)
	}
	return nil, nil //nolint:nilnil // test stub default
}

func (s *safeStore) Save(ctx context.Context, m *domain.Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveFunc != nil {
		return s.saveFunc(ctx, m)
	}
	return nil
}

type mockSyncService struct {
	uploadFunc func(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
}

func (m *mockSyncService) Download(_ context.Context, local, _ domain.SyncState) (domain.SyncState, error) {
	return local, nil
}

func (m *mockSyncService) Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, local, remote)
	}
	return local, nil
}

func noopSyncer() *mockSyncService { return &mockSyncService{} }

func emptyStore() *mocks.MockManifestStore { return &mocks.MockManifestStore{} }

func autoRespondSaveRequested(bus ports.EventBus) func() {
	ch, unsub := bus.Subscribe()
	go func() {
		for e := range ch {
			if _, ok := e.(running.SaveRequested); ok {
				bus.Publish(running.SaveCompleted{})
			}
		}
	}()
	return unsub
}

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
	_, stop := heartbeat.Attach(bus, locker.Heartbeat, emptyStore(), emptyStore(), noopSyncer())
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

	_, stop := heartbeat.Attach(bus, locker.Heartbeat, emptyStore(), emptyStore(), noopSyncer())
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
	_, stop := heartbeat.Attach(bus, locker.Heartbeat, emptyStore(), emptyStore(), noopSyncer())
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

func TestPlayerPlaying_WorldsSyncEveryTick(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	remoteStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}
	localStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}

	var uploadCount atomic.Int32
	syncer := &mockSyncService{
		uploadFunc: func(_ context.Context, local, _ domain.SyncState) (domain.SyncState, error) {
			uploadCount.Add(1)
			return local, nil
		},
	}

	bus := adapters.NewEventBus(64)
	saveUnsub := autoRespondSaveRequested(bus)
	defer saveUnsub()

	_, stop := heartbeat.Attach(bus, locker.Heartbeat, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	bus.Publish(running.ServerReadyInfo{})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if uploadCount.Load() >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected >=2 uploads, got %d", uploadCount.Load())
}

func TestPreviousSyncStillRunning_NextSyncWaits(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	remoteStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}
	localStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	syncer := &mockSyncService{
		uploadFunc: func(_ context.Context, local, _ domain.SyncState) (domain.SyncState, error) {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(40 * time.Millisecond)
			concurrent.Add(-1)
			return local, nil
		},
	}

	bus := adapters.NewEventBus(64)
	saveUnsub := autoRespondSaveRequested(bus)
	defer saveUnsub()

	_, stop := heartbeat.Attach(bus, locker.Heartbeat, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	bus.Publish(running.ServerReadyInfo{})

	time.Sleep(120 * time.Millisecond)

	if mc := maxConcurrent.Load(); mc > 1 {
		t.Fatalf("concurrent uploads: %d, expected max 1", mc)
	}
}

func TestPlayerStopsServer_SyncStops(t *testing.T) {
	store := newLeaseStore()
	locker, sessionID := acquireFor(t, store)

	remoteStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}
	localStore := &safeStore{
		getFunc:  func(context.Context) (*domain.Manifest, error) { return &domain.Manifest{}, nil },
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}

	var uploadCount atomic.Int32
	syncer := &mockSyncService{
		uploadFunc: func(_ context.Context, local, _ domain.SyncState) (domain.SyncState, error) {
			uploadCount.Add(1)
			return local, nil
		},
	}

	bus := adapters.NewEventBus(64)
	saveUnsub := autoRespondSaveRequested(bus)
	defer saveUnsub()

	_, stop := heartbeat.Attach(bus, locker.Heartbeat, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ritual.LockAcquiredInfo{RunID: "run-1", SessionID: sessionID, Interval: 20 * time.Millisecond})
	bus.Publish(running.ServerReadyInfo{})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if uploadCount.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if uploadCount.Load() < 1 {
		t.Fatal("no uploads before stop")
	}

	bus.Publish(running.ServerStoppedInfo{})
	time.Sleep(20 * time.Millisecond)

	countAtStop := uploadCount.Load()
	time.Sleep(80 * time.Millisecond)
	countAfter := uploadCount.Load()

	if countAfter > countAtStop {
		t.Fatalf("uploads continued after server stop: atStop=%d after=%d", countAtStop, countAfter)
	}
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
