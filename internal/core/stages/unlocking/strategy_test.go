// Package unlocking_test — Unlocking stage story suite.
//
// Unlocking is the session's release boundary. It calls lock.Locker.Release
// with rs.SessionID; Release is silent-no-op on foreign/absent leases, so
// Unlocking is always safe to run. A zero SessionID means this run never
// acquired — skip the Release call, skip the LockReleasedInfo event.
package unlocking_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ritual/internal/adapters"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/unlocking"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnlocking_WhenSessionOwnsLease_DeletesRemoteObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed before Unlocking can exercise the owning-release path")

	strategy := unlocking.New(locker.Release, nil)
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), SessionID: sessionID}

	_, err = strategy.Run(ctx, rs)

	require.NoError(t, err, "Unlocking must never surface errors — release is best-effort by design")
	assert.False(t, store.exists(lock.Key), "owning Release must delete the remote lease object — presence IS the held signal per §Remote lock object")
}

func TestUnlocking_WhenSessionIDEmpty_SkipsReleaseAndSkipsEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	store.seedLive(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	bus := adapters.NewEventBus(8)
	ch, unsub := bus.Subscribe()
	defer unsub()

	strategy := unlocking.New(locker.Release, nil)
	rs := &ritual.RunState{RunID: "r", Bus: bus, SessionID: ""}

	_, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "Unlocking with zero SessionID must not error — this is the 'never acquired' fast-path")
	assert.True(t, store.exists(lock.Key), "zero SessionID means we never acquired — foreign lease must remain untouched")

	events := drainBus(t, ch, 50*time.Millisecond)
	for i := range events {
		if _, ok := events[i].(ritual.LockReleasedInfo); ok {
			t.Fatal("LockReleasedInfo must not fire when SessionID is empty — heartbeat supervisor never attached, so there is no subscription to tear down")
		}
	}
}

func TestUnlocking_WhenForeignLeaseHeld_SilentNoOpPreservesHolder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	store := newLeaseStore()
	store.seedLive(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	strategy := unlocking.New(locker.Release, nil)
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), SessionID: "stale-session"}

	_, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "Unlocking with a foreign lease on the remote must not error — Release silently no-ops on session mismatch")
	assert.True(t, store.exists(lock.Key), "foreign lease must remain intact — footgun guard against 'we crashed, woke up, deleted new holder's lock'")
}

func TestUnlocking_RunsUnderWithoutCancelSoShutdownCannotAbortRelease(t *testing.T) {
	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(context.Background())
	require.NoError(t, err, "setup Acquire must succeed")

	strategy := unlocking.New(locker.Release, nil)
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), SessionID: sessionID}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = strategy.Run(ctx, rs)

	require.NoError(t, err, "cancelled parent context must not prevent release — Unlocking wraps in WithoutCancel precisely so shutdown cannot skip cleanup")
	assert.False(t, store.exists(lock.Key), "release must still land despite cancelled parent — this is the core correctness guarantee of Unlocking under shutdown")
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

type leaseStore struct {
	mu    sync.Mutex
	items map[string][]byte
}

func newLeaseStore() *leaseStore {
	return &leaseStore{items: map[string][]byte{}}
}

func (s *leaseStore) String() string { return "mem::lease" }

func (s *leaseStore) exists(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key]
	return ok
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
	return errors.New("leaseStore: Copy not used by Unlocking")
}

func (s *leaseStore) Rename(_ context.Context, _ string, _ string) error {
	return errors.New("leaseStore: Rename not used by Unlocking")
}

type decodedPayload struct {
	Owner       string    `json:"owner"`
	SessionID   string    `json:"sessionId"`
	AcquiredAt  time.Time `json:"acquiredAt"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
