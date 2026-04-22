package lock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ritual/internal/core/ports"

	"github.com/google/uuid"
)

// DefaultHeartbeatInterval is the cadence at which a live session rewrites
// the remote payload. Matches §Lease tuning in the v2.1 spec.
const DefaultHeartbeatInterval = time.Minute

// DefaultTTLMultiplier is applied to HeartbeatInterval to derive ExpiresAt on
// every write. 5.2 absorbs a full round-trip plus GC jitter beyond five
// heartbeat intervals before a reader considers the lease stale.
const DefaultTTLMultiplier = 5.2

// Locker owns the remote lease lifecycle. Tunables are public fields;
// callers assign directly at composition time or in tests.
type Locker struct {
	HeartbeatInterval time.Duration
	TTLMultiplier     float64

	storage ports.StorageRepository
	host    string
}

// New returns a Locker with spec defaults (1 min heartbeat, 5.2 multiplier).
func New(storage ports.StorageRepository, host string) *Locker {
	return &Locker{
		HeartbeatInterval: DefaultHeartbeatInterval,
		TTLMultiplier:     DefaultTTLMultiplier,
		storage:           storage,
		host:              host,
	}
}

// Acquire claims the remote lease, returning the freshly-minted sessionId.
// Fresh-slot path: writes a new payload. Expired-holder path: overwrites the
// dead lease. Live-holder path: returns ErrLocked.
func (l *Locker) Acquire(ctx context.Context) (string, error) {
	current, found, err := l.read(ctx)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if found && now.Before(current.ExpiresAt) {
		return "", fmt.Errorf("%w: by %s (session %s) until %s",
			ErrLocked, current.Owner, current.SessionID, current.ExpiresAt.Format(time.RFC3339))
	}
	fresh := payload{
		Owner:       l.host,
		SessionID:   uuid.NewString(),
		AcquiredAt:  now,
		HeartbeatAt: now,
		ExpiresAt:   now.Add(l.ttl()),
	}
	if err := l.write(ctx, fresh); err != nil {
		return "", err
	}
	return fresh.SessionID, nil
}

// Heartbeat rewrites the remote payload with a fresh HeartbeatAt/ExpiresAt,
// pinning Owner/SessionID/AcquiredAt. Returns ErrLeaseLost if the remote
// sessionId no longer matches or the lease has vanished.
func (l *Locker) Heartbeat(ctx context.Context, sessionID string) error {
	current, found, err := l.read(ctx)
	if err != nil {
		return err
	}
	if !found || current.SessionID != sessionID {
		return ErrLeaseLost
	}
	now := time.Now()
	current.HeartbeatAt = now
	current.ExpiresAt = now.Add(l.ttl())
	return l.write(ctx, *current)
}

// Release deletes the remote lease iff the caller still owns it. Absent or
// foreign leases are silent no-ops — the caller already lost the race.
func (l *Locker) Release(ctx context.Context, sessionID string) error {
	current, found, err := l.read(ctx)
	if err != nil {
		return err
	}
	if !found || current.SessionID != sessionID {
		return nil
	}
	return l.storage.Delete(ctx, Key)
}

// Verify checks that the remote sessionId still matches. Used by verbs at
// pre- and post-commit fence points to detect zombie-writer scenarios.
func (l *Locker) Verify(ctx context.Context, sessionID string) error {
	current, found, err := l.read(ctx)
	if err != nil {
		return err
	}
	if !found || current.SessionID != sessionID {
		return ErrLeaseLost
	}
	return nil
}

// Inspect returns a read-only snapshot of the remote lease for display.
// (nil, nil) means the slot is free.
func (l *Locker) Inspect(ctx context.Context) (*Holder, error) {
	current, found, err := l.read(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &Holder{
		Owner:       current.Owner,
		SessionID:   current.SessionID,
		AcquiredAt:  current.AcquiredAt,
		HeartbeatAt: current.HeartbeatAt,
		ExpiresAt:   current.ExpiresAt,
		Stale:       !time.Now().Before(current.ExpiresAt),
	}, nil
}

func (l *Locker) ttl() time.Duration {
	return time.Duration(float64(l.HeartbeatInterval) * l.TTLMultiplier)
}

func (l *Locker) read(ctx context.Context) (*payload, bool, error) {
	exists, err := l.storage.Exists(ctx, Key)
	if err != nil {
		return nil, false, fmt.Errorf("lock: exists check: %w", err)
	}
	if !exists {
		return nil, false, nil
	}
	body, err := l.storage.GetStream(ctx, Key)
	if err != nil {
		return nil, false, fmt.Errorf("lock: read: %w", err)
	}
	defer body.Close()
	var p payload
	if err := json.NewDecoder(body).Decode(&p); err != nil {
		return nil, false, fmt.Errorf("lock: decode: %w", err)
	}
	return &p, true, nil
}

func (l *Locker) write(ctx context.Context, p payload) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("lock: encode: %w", err)
	}
	if err := l.storage.PutStream(ctx, Key, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("lock: write: %w", err)
	}
	return nil
}
