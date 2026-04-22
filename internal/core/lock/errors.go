// Package lock coordinates single-writer access to a remote prefix via a
// self-describing lease object stored at {prefix}/lock. Sessions acquire,
// heartbeat, and release the lease; verbs remain lock-agnostic and receive
// only the sessionId string for fencing.
//
// See docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md §Lock
// Discipline for the protocol.
package lock

import "errors"

// ErrLocked is returned when acquisition finds a live lease owned by another
// session. This session never held the lease.
var ErrLocked = errors.New("lock: held by another session")

// ErrAcquireRace is returned when a same-instant takeover race is detected
// after the PUT. Retryable via Acquire again.
var ErrAcquireRace = errors.New("lock: acquisition race, retry")

// ErrLeaseLost is raised when a pre-operation fence observes that this
// session's sessionId no longer matches the remote payload. No remote
// linearization attributable to this session has happened.
var ErrLeaseLost = errors.New("lock: lease lost before commit point")

// ErrLeaseLostAfterPut is raised when a post-operation fence observes a
// sessionId mismatch AFTER a write has landed. Callers perform best-effort
// rollback of the orphaned write and surface the error loudly.
var ErrLeaseLostAfterPut = errors.New("lock: lease lost after commit point, manifest rolled back")
