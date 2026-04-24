// Package acquiring_test — Acquiring stage story suite.
//
// Acquiring is the lease-claim boundary. A fresh or expired remote slot
// yields a new sessionId pinned on RunState and a LockAcquiredInfo on
// the bus for the heartbeat supervisor. A live foreign lease routes to
// onFail and surfaces the holder via LockHeldInfo.
//
// Rules (per ritual_integration_test.go):
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
//   - Custom fakes with story-friendly names; never generic mocks.
package acquiring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ritual/internal/adapters"
	"ritual/internal/core/lock"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubStrategy struct{ tag string }

func (s *stubStrategy) Run(context.Context, *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil
}

func drainBus(t *testing.T, ch <-chan ports.Event, quiet time.Duration) []ports.Event {
	t.Helper()
	var events []ports.Event
	timeout := time.NewTimer(quiet)
	defer timeout.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
			timeout.Reset(quiet)
		case <-timeout.C:
			return events
		}
	}
}

func TestAcquiring_FreshSlot_SetsSessionIDAndPublishesLockAcquired(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	onFail := &stubStrategy{tag: "failed"}
	onOK := &stubStrategy{tag: "run"}
	strategy := acquiring.New(locker.Acquire, locker.Inspect, nil, locker.HeartbeatInterval, onOK, onFail)

	rs := &ritual.RunState{RunID: "test-run", Bus: bus}
	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "acquiring Run must not surface transport errors on a fresh-slot happy path")
	assert.Same(t, onOK, next, "fresh-slot acquire must route to onOK so the pipeline continues into Running")
	assert.NotEmpty(t, rs.SessionID, "acquiring must record the fresh sessionId on RunState so downstream stages can fence with it")

	events := drainBus(t, ch, 50*time.Millisecond)
	var acquired *ritual.LockAcquiredInfo
	for i := range events {
		if e, ok := events[i].(ritual.LockAcquiredInfo); ok {
			acquired = &e
			break
		}
	}
	require.NotNil(t, acquired, "acquiring must publish LockAcquiredInfo on success so the heartbeat supervisor can attach to this run")
	assert.Equal(t, "test-run", acquired.RunID, "LockAcquiredInfo.RunID must echo rs.RunID so supervisor maps events to the correct run")
	assert.Equal(t, rs.SessionID, acquired.SessionID, "LockAcquiredInfo.SessionID must equal rs.SessionID — supervisor uses it as the fencing token for Heartbeat calls")
	assert.Equal(t, lock.DefaultHeartbeatInterval, acquired.Interval, "LockAcquiredInfo.Interval must carry the locker's heartbeat cadence so supervisor schedules its beat loop correctly")
}

func TestAcquiring_ActiveLease_PublishesLockHeldInfoWithHolder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	store.seedLive(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	onFail := &stubStrategy{tag: "failed"}
	onOK := &stubStrategy{tag: "run"}
	strategy := acquiring.New(locker.Acquire, locker.Inspect, nil, locker.HeartbeatInterval, onOK, onFail)

	rs := &ritual.RunState{RunID: "test-run", Bus: bus}
	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "acquiring Run must not surface the ErrLocked up the stack — it routes via onFail and records rs.Err instead")
	assert.Same(t, onFail, next, "live-foreign-lease must route to onFail so the pipeline short-circuits to the Failed stage")
	require.Error(t, rs.Err, "acquiring must record an error on RunState describing the lease collision")
	assert.ErrorIs(t, rs.Err, lock.ErrLocked, "recorded RunState error must be the ErrLocked sentinel so callers can dispatch on class, not string match")
	assert.Empty(t, rs.SessionID, "no sessionId must be recorded on a failed acquire — callers must not retain a token for a lease they don't hold")

	events := drainBus(t, ch, 50*time.Millisecond)
	var held *acquiring.LockHeldInfo
	for i := range events {
		if e, ok := events[i].(acquiring.LockHeldInfo); ok {
			held = &e
			break
		}
	}
	require.NotNil(t, held, "acquiring must publish LockHeldInfo when the remote lease is active — the GUI projection depends on this event to render the stage-locked screen with the holder identity")
	assert.Equal(t, "hostB", held.Holder, "LockHeldInfo.Holder must equal the remote owner hostname verbatim so the UI can show the actual host name")
}

func TestAcquiring_StorageError_RoutesFailWithoutLockAcquiredInfo(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	store.existsErr = errors.New("transport down")
	locker := lock.New(store, "hostA")
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	onFail := &stubStrategy{tag: "failed"}
	onOK := &stubStrategy{tag: "run"}
	strategy := acquiring.New(locker.Acquire, locker.Inspect, nil, locker.HeartbeatInterval, onOK, onFail)

	rs := &ritual.RunState{RunID: "test-run", Bus: bus}
	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "acquiring Run must never surface transport errors up the stack — the pipeline routes failures via onFail")
	assert.Same(t, onFail, next, "storage transport errors must route to onFail so the pipeline reports the failure through the normal path")
	require.Error(t, rs.Err, "acquiring must record the storage error on RunState so the Failed stage can report it")
	assert.Empty(t, rs.SessionID, "no sessionId must be recorded when acquire never succeeded")

	events := drainBus(t, ch, 50*time.Millisecond)
	for i := range events {
		if _, ok := events[i].(ritual.LockAcquiredInfo); ok {
			t.Fatal("LockAcquiredInfo must not be published on a failed acquire — the heartbeat supervisor must never attach to a run that never claimed the lease")
		}
	}
}

// --- leaseStore — in-memory ports.StorageRepository + seed helpers ---

type leaseStore struct {
	mu        sync.Mutex
	items     map[string][]byte
	existsErr error
}

func newLeaseStore() *leaseStore {
	return &leaseStore{items: map[string][]byte{}}
}

func (s *leaseStore) String() string { return "mem::lease" }

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
	require.NoError(t, err, "seed payload must marshal — test setup invariant")
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
	return nil
}

func (s *leaseStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.existsErr != nil {
		return false, s.existsErr
	}
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
	return nil, errors.New("leaseStore: Get is deprecated, use GetStream")
}

func (s *leaseStore) Put(_ context.Context, _ string, _ []byte) error {
	return errors.New("leaseStore: Put is deprecated, use PutStream")
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
	return errors.New("leaseStore: Copy not used by Acquiring")
}

func (s *leaseStore) Rename(_ context.Context, _ string, _ string) error {
	return errors.New("leaseStore: Rename not used by Acquiring")
}

type decodedPayload struct {
	Owner       string    `json:"owner"`
	SessionID   string    `json:"sessionId"`
	AcquiredAt  time.Time `json:"acquiredAt"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
