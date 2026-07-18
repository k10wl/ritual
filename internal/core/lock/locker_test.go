// Package lock_test — Locker story suite.
//
// Locker owns a self-describing remote lease at the "lock" key. A session
// acquires once (fresh slot or expired-holder takeover), rewrites the
// payload on every verb boundary to keep the lease alive, and releases at
// session end. Verbs stay lock-agnostic; they see only the returned
// sessionId string and call Verify at fence points.
//
// The protocol is spec §Lock Discipline in docs/superpowers/specs/
// 2026-04-19-fast-sync-v2.1-design.md, with the project-local decision to
// replace conditional-PUT CAS with sessionId-based read-verify-mutate.
// Single-writer invariant is operational, not enforced by etag.
//
// Rules for writing tests in this file (per ritual_integration_test.go):
//
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
//   - Custom fakes with story-friendly names; never generic mocks.
package lock_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ritual/internal/core/lock"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocker_DefaultsAreOneMinuteHeartbeatAndFivePointTwoMultiplier(t *testing.T) {
	locker := lock.New(newLeaseStore(), "hostA")

	assert.Equal(t, time.Minute, locker.HeartbeatInterval,
		"default HeartbeatInterval must be 1 minute per §Lease tuning — the spec baseline for crash-detection budget")
	assert.InDelta(t, 5.2, locker.TTLMultiplier, 1e-9,
		"default TTLMultiplier must be 5.2 per §Lease tuning — absorbs one RTT + GC jitter past the fifth heartbeat")
}

func TestLocker_AcquireOnEmptyRemoteClaimsSlotAndWritesFullPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")

	sessionID, err := locker.Acquire(ctx)

	require.NoError(t, err,
		"Acquire against an empty remote must succeed — no prior holder exists to contend with")
	assert.NotEmpty(t, sessionID,
		"Acquire must return a non-empty sessionId — callers use this as the fencing token for the whole session")
	seen := store.mustDecode(t, lock.Key)
	assert.Equal(t, "hostA", seen.Owner,
		"written payload's owner must be the host passed to New — diagnostic identity for 'who is blocking me'")
	assert.Equal(t, sessionID, seen.SessionID,
		"written payload's sessionId must equal the one returned to the caller — fencing depends on this equality")
	assert.False(t, seen.AcquiredAt.IsZero(),
		"AcquiredAt must be set on fresh acquire — zero value would misrender 'locked for N min' diagnostics")
	assert.Equal(t, seen.AcquiredAt, seen.HeartbeatAt,
		"on fresh acquire HeartbeatAt must equal AcquiredAt — no heartbeat has happened yet")
	assert.Equal(t, seen.HeartbeatAt.Add(time.Minute*52/10), seen.ExpiresAt,
		"ExpiresAt must equal HeartbeatAt + HeartbeatInterval * TTLMultiplier (1 min * 5.2 = 5m12s)")
}

func TestLocker_AcquireWhenLiveLeaseHeldReturnsErrLocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	store.seedLive(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	sessionID, err := locker.Acquire(ctx)

	require.Error(t, err,
		"Acquire against a live foreign lease must error — single-writer invariant forbids double-hold")
	assert.ErrorIs(t, err, lock.ErrLocked,
		"error must be ErrLocked — callers dispatch on sentinel class, not string match")
	assert.Contains(t, err.Error(), "hostB",
		"error message must name the current owner — user-visible 'locked by {owner}' diagnostic")
	assert.Empty(t, sessionID,
		"no sessionId must be returned on failed acquire — callers must not retain a token for a lease they don't hold")
}

func TestLocker_AcquireTakesOverExpiredLeaseWithNewSessionId(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	store.seedExpired(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	sessionID, err := locker.Acquire(ctx)

	require.NoError(t, err,
		"Acquire against an expired foreign lease must succeed — stale lessees self-expire past ExpiresAt")
	assert.NotEmpty(t, sessionID,
		"takeover must return a fresh sessionId — identity rotates so the old holder cannot fence-verify successfully")
	assert.NotEqual(t, "sess-bob", sessionID,
		"takeover sessionId must differ from the prior owner's — zombie writers must fail Verify after takeover")
	seen := store.mustDecode(t, lock.Key)
	assert.Equal(t, "hostA", seen.Owner,
		"takeover must overwrite Owner with the new holder's host — display reflects the active session, not the ghost")
	assert.Equal(t, sessionID, seen.SessionID,
		"persisted sessionId must match the returned one — same rule as fresh-acquire, fencing token round-trips")
}

func TestLocker_HeartbeatRefreshesHeartbeatAtAndExpiryWhilePinningIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed before testing Heartbeat")
	before := store.mustDecode(t, lock.Key)
	time.Sleep(2 * time.Millisecond)

	err = locker.Heartbeat(ctx, sessionID)

	require.NoError(t, err,
		"Heartbeat with the matching sessionId must succeed — the holder is refreshing its own lease")
	after := store.mustDecode(t, lock.Key)
	assert.True(t, after.HeartbeatAt.After(before.HeartbeatAt),
		"HeartbeatAt must advance on every Heartbeat — proof of liveness for takeover readers")
	assert.True(t, after.ExpiresAt.After(before.ExpiresAt),
		"ExpiresAt must advance alongside HeartbeatAt — lease extension is the whole point of Heartbeat")
	assert.Equal(t, before.SessionID, after.SessionID,
		"SessionID must stay pinned across Heartbeat — identity rotation would break outstanding fence checks")
	assert.Equal(t, before.AcquiredAt, after.AcquiredAt,
		"AcquiredAt must stay pinned — diagnostic 'locked for N min' reads the original acquisition instant")
	assert.Equal(t, before.Owner, after.Owner,
		"Owner must stay pinned — Heartbeat is never a takeover, ownership identity is fixed for the session")
}

func TestLocker_HeartbeatAfterTakeoverReturnsErrLeaseLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")
	store.seedLive(t, "hostB", "sess-bob", time.Hour)

	err = locker.Heartbeat(ctx, sessionID)

	require.Error(t, err,
		"Heartbeat against a payload whose sessionId has rotated must fail — zombie-detect path")
	assert.ErrorIs(t, err, lock.ErrLeaseLost,
		"error must be ErrLeaseLost — the orchestrator terminates the session on this sentinel without a Release call")
}

func TestLocker_HeartbeatWhenRemoteVanishedReturnsErrLeaseLost(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")
	store.forceDelete(lock.Key)

	err = locker.Heartbeat(ctx, sessionID)

	require.Error(t, err,
		"Heartbeat against a missing lease must fail — the session's lease is gone, not recoverable")
	assert.ErrorIs(t, err, lock.ErrLeaseLost,
		"vanished lease is a flavor of lease-lost — sentinel unifies both take-over and swept-away cases")
}

func TestLocker_ReleaseRemovesOwnedLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed before Release")

	err = locker.Release(ctx, sessionID)

	require.NoError(t, err,
		"Release with the owning sessionId must succeed — the clean session-end path")
	assert.False(t, store.exists(lock.Key),
		"lease object must be absent after Release — presence IS the held signal per §Remote lock object")
}

func TestLocker_ReleaseNoOpsWhenSessionMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	_, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")
	store.seedLive(t, "hostB", "sess-bob", time.Hour)

	err = locker.Release(ctx, "stale-session-id")

	require.NoError(t, err,
		"Release with a non-matching sessionId must silently no-op — we would be deleting somebody else's lease otherwise")
	assert.True(t, store.exists(lock.Key),
		"foreign lease must remain intact — footgun guard against 'we crashed, woke up, deleted new holder's lock'")
	seen := store.mustDecode(t, lock.Key)
	assert.Equal(t, "sess-bob", seen.SessionID,
		"remote sessionId must still be Bob's — Release never mutates a lease it does not own")
}

func TestLocker_ReleaseNoOpsWhenLeaseAlreadyGone(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")
	store.forceDelete(lock.Key)

	err = locker.Release(ctx, sessionID)

	require.NoError(t, err,
		"Release against a missing lease must be a silent success — crashed-and-swept is indistinguishable from clean release to the next run")
}

func TestLocker_VerifyReturnsNilWhenSessionMatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	sessionID, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")

	err = locker.Verify(ctx, sessionID)

	assert.NoError(t, err,
		"Verify with the owning sessionId must return nil — verbs call this at fence points to prove no takeover happened")
}

func TestLocker_VerifyReturnsErrLeaseLostOnSessionMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")
	_, err := locker.Acquire(ctx)
	require.NoError(t, err, "setup Acquire must succeed")
	store.seedLive(t, "hostB", "sess-bob", time.Hour)

	err = locker.Verify(ctx, "original-session-id")

	require.Error(t, err,
		"Verify against a payload whose sessionId has rotated must fail — this is the core zombie-writer fence")
	assert.ErrorIs(t, err, lock.ErrLeaseLost,
		"error must be ErrLeaseLost — push verb uses errors.Is to decide whether to roll back a manifest PUT")
}

func TestLocker_InspectReturnsNilWhenRemoteEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	locker := lock.New(store, "hostA")

	holder, err := locker.Inspect(ctx)

	require.NoError(t, err,
		"Inspect against an empty remote must not error — absence is a valid answer, not a transport failure")
	assert.Nil(t, holder,
		"nil holder is the protocol's 'slot is free' signal — GUI renders 'Start Sync' safely")
}

func TestLocker_InspectReportsLiveHolderNotStale(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	store.seedLive(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	holder, err := locker.Inspect(ctx)

	require.NoError(t, err, "Inspect of a live foreign lease must not error")
	require.NotNil(t, holder,
		"Inspect must return a populated Holder when a lease exists — nil is reserved for the empty-remote case")
	assert.Equal(t, "hostB", holder.Owner,
		"Holder.Owner must surface the remote owner's hostname — rendered in 'Locked by {Owner}' UI")
	assert.Equal(t, "sess-bob", holder.SessionID,
		"Holder.SessionID must surface the remote sessionId — diagnostic for support logs")
	assert.False(t, holder.Stale,
		"Stale must be false while ExpiresAt is in the future — takeover is not yet legitimate")
}

func TestLocker_InspectReportsStaleHolderWhenExpired(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	store := newLeaseStore()
	store.seedExpired(t, "hostB", "sess-bob", time.Hour)
	locker := lock.New(store, "hostA")

	holder, err := locker.Inspect(ctx)

	require.NoError(t, err, "Inspect of an expired foreign lease must not error")
	require.NotNil(t, holder,
		"Inspect must return the stale Holder even past ExpiresAt — diagnostics need the ghost owner's identity")
	assert.True(t, holder.Stale,
		"Stale must be true once ExpiresAt lies in the past — GUI renders 'Acquire will take over' affordance")
}

// --- leaseStore — in-memory ports.StorageRepository + seed helpers ---

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

func (s *leaseStore) forceDelete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

func (s *leaseStore) mustDecode(t *testing.T, key string) decodedPayload {
	t.Helper()
	s.mu.Lock()
	data, ok := s.items[key]
	s.mu.Unlock()
	require.True(t, ok,
		"leaseStore must contain key %q — the code under test failed to write the lease object", key)
	var p decodedPayload
	require.NoError(t, json.Unmarshal(data, &p),
		"leaseStore key %q must decode as lease payload — invalid JSON means Locker wrote garbage", key)
	return p
}

func (s *leaseStore) seedLive(t *testing.T, owner, sessionID string, ttl time.Duration) {
	t.Helper()
	now := time.Now()
	s.seedPayload(t, decodedPayload{
		Owner:       owner,
		SessionID:   sessionID,
		AcquiredAt:  now,
		HeartbeatAt: now,
		ExpiresAt:   now.Add(ttl),
	})
}

func (s *leaseStore) seedExpired(t *testing.T, owner, sessionID string, ago time.Duration) {
	t.Helper()
	past := time.Now().Add(-2 * ago)
	s.seedPayload(t, decodedPayload{
		Owner:       owner,
		SessionID:   sessionID,
		AcquiredAt:  past,
		HeartbeatAt: past,
		ExpiresAt:   past.Add(ago),
	})
}

func (s *leaseStore) seedPayload(t *testing.T, p decodedPayload) {
	t.Helper()
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
	return errors.New("leaseStore: Copy not used by Locker")
}

func (s *leaseStore) Rename(_ context.Context, _ string, _ string) error {
	return errors.New("leaseStore: Rename not used by Locker")
}

// decodedPayload mirrors the unexported lock.payload for test assertions.
type decodedPayload struct {
	Owner       string    `json:"owner"`
	SessionID   string    `json:"sessionId"`
	AcquiredAt  time.Time `json:"acquiredAt"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
