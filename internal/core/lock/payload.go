package lock

import "time"

// Key is the storage key of the lease object, rooted at the bucket/prefix
// boundary and NOT nested under refs/.
const Key = "lock"

// payload is the five-field wire format documented in §Lock Discipline.
// All fields are load-bearing; see the spec for semantics.
type payload struct {
	Owner       string    `json:"owner"`
	SessionID   string    `json:"sessionId"`
	AcquiredAt  time.Time `json:"acquiredAt"`
	HeartbeatAt time.Time `json:"heartbeatAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Holder is a point-in-time snapshot of the remote lease exposed for display.
// Stale reports whether ExpiresAt is in the past relative to the reader's
// wall clock.
type Holder struct {
	Owner       string
	SessionID   string
	AcquiredAt  time.Time
	HeartbeatAt time.Time
	ExpiresAt   time.Time
	Stale       bool
}
