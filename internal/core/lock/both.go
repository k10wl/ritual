package lock

import (
	"context"
	"strings"
	"time"
)

// Side is the subset of Locker behaviour Both needs to compose two
// lease-bearing sides. Both *Locker and observed.Locker satisfy it
// without changes.
type Side interface {
	Acquire(ctx context.Context) (string, error)
	Release(ctx context.Context, sessionID string) error
	Heartbeat(ctx context.Context, sessionID string) error
	Inspect(ctx context.Context) (*Holder, error)
}

// Both stacks two Sides — typically a local-storage-backed Locker and a
// remote one — so the pipeline acquires both before the run proceeds and
// releases both regardless of which one fails. Local is checked first so
// a stranded same-host PID cannot pin a remote lease that no live process
// can release. SessionID is the local SID joined with the remote SID by
// `sidSeparator`.
type Both struct {
	local  Side
	remote Side
}

const sidSeparator = "|"

// NewBoth wraps two Sides into a single Side that satisfies the same
// contract. Order is meaningful: local is the first to acquire and the
// last to release.
func NewBoth(local, remote Side) *Both {
	return &Both{local: local, remote: remote}
}

// Acquire takes the local lease first. On local failure the remote is
// untouched. On remote failure the local lease is rolled back so a
// stranded local file does not block subsequent runs on this machine.
func (b *Both) Acquire(ctx context.Context) (string, error) {
	localSID, err := b.local.Acquire(ctx)
	if err != nil {
		return "", err
	}
	remoteSID, err := b.remote.Acquire(ctx)
	if err != nil {
		_ = b.local.Release(ctx, localSID)
		return "", err
	}
	return localSID + sidSeparator + remoteSID, nil
}

// Release splits the composite SessionID and releases both sides. An
// empty composite SID is a silent no-op so unlocking-stage runs on
// failure paths (where Acquire never succeeded) do not surface a
// foreign-owner error from the inner Lockers.
func (b *Both) Release(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	localSID, remoteSID := splitSID(sessionID)
	if err := b.remote.Release(ctx, remoteSID); err != nil {
		return err
	}
	return b.local.Release(ctx, localSID)
}

// Heartbeat splits the composite SessionID and refreshes both leases.
// Both must be refreshed every interval or the TTL on whichever side
// missed expires and the run loses its slot.
func (b *Both) Heartbeat(ctx context.Context, sessionID string) error {
	localSID, remoteSID := splitSID(sessionID)
	if err := b.local.Heartbeat(ctx, localSID); err != nil {
		return err
	}
	return b.remote.Heartbeat(ctx, remoteSID)
}

// Inspect returns the local holder if held, falling back to the remote
// holder otherwise. Local-first matches Acquire's order: the side that
// blocks Acquire is the side the user wants to see in the locked
// screen.
func (b *Both) Inspect(ctx context.Context) (*Holder, error) {
	if h, err := b.local.Inspect(ctx); err != nil {
		return nil, err
	} else if h != nil {
		return h, nil
	}
	return b.remote.Inspect(ctx)
}

// HeartbeatInterval mirrors lock.Locker's spec default. Both inner sides
// run the same Locker primitive with the same interval, so the supervisor
// only needs one cadence to refresh both leases together.
func (b *Both) HeartbeatInterval() time.Duration {
	return DefaultHeartbeatInterval
}

func splitSID(composite string) (local, remote string) {
	parts := strings.SplitN(composite, sidSeparator, 2)
	if len(parts) != 2 {
		return composite, ""
	}
	return parts[0], parts[1]
}
