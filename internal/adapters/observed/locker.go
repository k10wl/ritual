// observed/locker.go wraps lock.Locker with bus-backed event publishing.
// Follows the same decorator pattern as observedStorage: one event per
// verb, Err mirrored from the inner call without altering control flow.

package observed

import (
	"context"
	"errors"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"time"
)

// Locker mirrors the *lock.Locker public surface and is the type the
// composition root injects into stages (via method values). Instances
// are produced by NewLocker.
type Locker struct {
	inner *lock.Locker
	bus   ports.EventBus
}

// NewLocker decorates inner with bus-backed event publishing. Exposes the
// inner HeartbeatInterval/TTLMultiplier fields via public getters so
// callers can read the cadence when publishing LockAcquiredInfo.
func NewLocker(inner *lock.Locker, bus ports.EventBus) *Locker {
	return &Locker{inner: inner, bus: bus}
}

// HeartbeatInterval forwards the inner Locker's cadence field. Acquiring
// reads it to populate ritual.LockAcquiredInfo.Interval.
func (l *Locker) HeartbeatInterval() time.Duration { return l.inner.HeartbeatInterval }

// SetHeartbeatInterval mutates the inner cadence. Exposed for integration
// tests that need sub-second ticks; production leaves the default.
func (l *Locker) SetHeartbeatInterval(d time.Duration) { l.inner.HeartbeatInterval = d }

// Acquire claims the remote lease. Publishes LeaseAcquireInfo.
func (l *Locker) Acquire(ctx context.Context) (string, error) {
	start := time.Now()
	priorExisted, _ := l.priorPresence(ctx)
	sessionID, err := l.inner.Acquire(ctx)
	evt := LeaseAcquireInfo{SessionID: sessionID, DurationMs: sinceMs(start), Err: err}
	switch {
	case err == nil && priorExisted:
		evt.Mode = "takeover"
	case err == nil:
		evt.Mode = "fresh"
	case errors.Is(err, lock.ErrLocked):
		evt.Mode = "blocked"
	default:
		evt.Mode = "error"
	}
	l.publish(evt)
	return sessionID, err
}

// Heartbeat refreshes the remote lease. Publishes LeaseHeartbeatInfo.
func (l *Locker) Heartbeat(ctx context.Context, sessionID string) error {
	start := time.Now()
	err := l.inner.Heartbeat(ctx, sessionID)
	evt := LeaseHeartbeatInfo{SessionID: sessionID, DurationMs: sinceMs(start), Err: err}
	switch {
	case err == nil:
		evt.Result = "ok"
	case errors.Is(err, lock.ErrLeaseTakenOver):
		evt.Result = "taken_over"
	case errors.Is(err, lock.ErrLeaseVanished):
		evt.Result = "vanished"
	default:
		evt.Result = "transport"
	}
	l.publish(evt)
	return err
}

// Release deletes the remote lease iff this session still owns it.
// Publishes LeaseReleaseInfo with Mode indicating the observed branch.
func (l *Locker) Release(ctx context.Context, sessionID string) error {
	start := time.Now()
	priorHolder, _ := l.inner.Inspect(ctx)
	err := l.inner.Release(ctx, sessionID)
	evt := LeaseReleaseInfo{SessionID: sessionID, DurationMs: sinceMs(start), Err: err}
	switch {
	case err != nil:
		evt.Mode = "error"
	case priorHolder == nil:
		evt.Mode = "absent"
	case priorHolder.SessionID != sessionID:
		evt.Mode = "foreign"
	default:
		evt.Mode = "owned"
	}
	l.publish(evt)
	return err
}

// Verify checks the remote sessionId matches. Intentionally silent on the
// event bus — verbs fence at high frequency; their own decorators own
// the telemetry surface.
func (l *Locker) Verify(ctx context.Context, sessionID string) error {
	return l.inner.Verify(ctx, sessionID)
}

// Inspect returns a snapshot of the remote lease. Publishes
// LeaseInspectInfo so "who holds the lock right now" reads are visible.
func (l *Locker) Inspect(ctx context.Context) (*lock.Holder, error) {
	start := time.Now()
	h, err := l.inner.Inspect(ctx)
	evt := LeaseInspectInfo{DurationMs: sinceMs(start), Err: err}
	if h != nil {
		evt.Owner = h.Owner
		evt.SessionID = h.SessionID
		evt.ExpiresAt = h.ExpiresAt
		evt.Stale = h.Stale
	}
	l.publish(evt)
	return h, err
}

// priorPresence peeks at the lease object before Acquire so the event
// can report "fresh" vs "takeover" — the Locker itself doesn't expose
// that distinction in its return value.
func (l *Locker) priorPresence(ctx context.Context) (bool, error) {
	h, err := l.inner.Inspect(ctx)
	return h != nil, err
}

func (l *Locker) publish(evt ports.Event) {
	if l.bus != nil {
		l.bus.Publish(evt)
	}
}
