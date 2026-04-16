package heartbeat_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/subsystems/heartbeat"
)

// safeStore is a concurrency-safe ManifestStore for tests where both
// tick and syncTick access the same store from separate goroutines.
// Unlike mocks.MockManifestStore it does not have unsynchronized counters.
type safeStore struct {
	mu      sync.Mutex
	getFunc  func(context.Context) (*domain.Manifest, error)
	saveFunc func(context.Context, *domain.Manifest) error
}

func (s *safeStore) Get(ctx context.Context) (*domain.Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getFunc != nil {
		return s.getFunc(ctx)
	}
	return nil, nil
}

func (s *safeStore) Save(ctx context.Context, m *domain.Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveFunc != nil {
		return s.saveFunc(ctx, m)
	}
	return nil
}

// mockSyncService is a handwritten mock for ports.SyncService.
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

// noopSyncer returns a SyncService that does nothing.
func noopSyncer() *mockSyncService { return &mockSyncService{} }

// emptyStore returns a ManifestStore that always returns (nil, nil).
func emptyStore() *mocks.MockManifestStore { return &mocks.MockManifestStore{} }

// autoRespondSaveRequested subscribes to bus and auto-responds to
// SaveRequested with SaveCompleted so syncTick can proceed.
func autoRespondSaveRequested(bus ports.EventBus) func() {
	ch, unsub := bus.Subscribe()
	go func() {
		for e := range ch {
			if _, ok := e.(ports.SaveRequested); ok {
				bus.Publish(ports.SaveCompleted{})
			}
		}
	}()
	return unsub
}

func TestSupervisorBeatsAfterAcquire(t *testing.T) {
	var mu sync.Mutex
	current := &domain.Manifest{LockedBy: "run-1"}

	remoteStore := &mocks.MockManifestStore{
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
	_, stop := heartbeat.Attach(bus, emptyStore(), remoteStore, noopSyncer())
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
	remoteStore := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) { return current, nil },
	}
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	_, stop := heartbeat.Attach(bus, emptyStore(), remoteStore, noopSyncer())
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
	remoteStore := &mocks.MockManifestStore{
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
	_, stop := heartbeat.Attach(bus, emptyStore(), remoteStore, noopSyncer())
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

func TestPlayerPlaying_WorldsSyncEveryTick(t *testing.T) {
	manifest := &domain.Manifest{LockedBy: "run-1"}

	remoteStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return cloneForTest(manifest), nil
		},
		saveFunc: func(_ context.Context, m *domain.Manifest) error {
			manifest = cloneForTest(m)
			return nil
		},
	}
	localStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{}, nil
		},
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

	_, stop := heartbeat.Attach(bus, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 50 * time.Millisecond})
	bus.Publish(ports.ServerReadyInfo{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if uploadCount.Load() >= 2 {
			// also verify heartbeat refreshed
			remoteStore.mu.Lock()
			beat := manifest.HeartbeatAt
			remoteStore.mu.Unlock()
			if !beat.IsZero() {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected >=2 uploads, got %d", uploadCount.Load())
}

func TestPreviousSyncStillRunning_NextSyncWaits(t *testing.T) {
	manifest := &domain.Manifest{LockedBy: "run-1"}

	remoteStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return cloneForTest(manifest), nil
		},
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}
	localStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{}, nil
		},
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
			time.Sleep(200 * time.Millisecond)
			concurrent.Add(-1)
			return local, nil
		},
	}

	bus := adapters.NewEventBus(64)
	saveUnsub := autoRespondSaveRequested(bus)
	defer saveUnsub()

	_, stop := heartbeat.Attach(bus, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 50 * time.Millisecond})
	bus.Publish(ports.ServerReadyInfo{})

	// let several ticks fire while upload is slow
	time.Sleep(600 * time.Millisecond)

	if mc := maxConcurrent.Load(); mc > 1 {
		t.Fatalf("concurrent uploads: %d, expected max 1", mc)
	}
}

func TestPlayerStopsServer_SyncStops(t *testing.T) {
	manifest := &domain.Manifest{LockedBy: "run-1"}

	remoteStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return cloneForTest(manifest), nil
		},
		saveFunc: func(_ context.Context, _ *domain.Manifest) error { return nil },
	}
	localStore := &safeStore{
		getFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{}, nil
		},
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

	_, stop := heartbeat.Attach(bus, localStore, remoteStore, syncer)
	defer stop()

	bus.Publish(ports.LockAcquiredInfo{RunID: "run-1", LockID: "run-1", Interval: 50 * time.Millisecond})
	bus.Publish(ports.ServerReadyInfo{})

	// wait for at least one sync
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if uploadCount.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if uploadCount.Load() < 1 {
		t.Fatal("no uploads before stop")
	}

	bus.Publish(ports.ServerStoppedInfo{})
	time.Sleep(50 * time.Millisecond)

	countAtStop := uploadCount.Load()
	time.Sleep(300 * time.Millisecond)
	countAfter := uploadCount.Load()

	if countAfter > countAtStop {
		t.Fatalf("uploads continued after server stop: atStop=%d after=%d", countAtStop, countAfter)
	}
}

func cloneForTest(m *domain.Manifest) *domain.Manifest {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}
