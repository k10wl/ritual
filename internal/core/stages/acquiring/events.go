package acquiring

import (
	"fmt"
	"time"
)

// LockHeldInfo fires when the remote lease is still live under a foreign
// holder and this run must abort. Carries the full Inspect snapshot so
// consumers (GUI projection, logs) can render "locked by {Holder} since
// {AcquiredAt}, expires {ExpiresAt}" without a second round-trip to the
// lock store.
type LockHeldInfo struct {
	Holder      string
	SessionID   string
	AcquiredAt  time.Time
	HeartbeatAt time.Time
	ExpiresAt   time.Time
	Stale       bool
}

func (l LockHeldInfo) String() string {
	return fmt.Sprintf("lock held by %s (session %s) until %s",
		l.Holder, shortSession(l.SessionID), l.ExpiresAt.Format(time.RFC3339))
}

func shortSession(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}
