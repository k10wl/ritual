package ports

import (
	"fmt"
	"time"
)

// Event is any fmt.Stringer. Open set, self-describing, compile-safe.
//
// To add a new event type:
//   1. Define a struct (anywhere in the codebase, no central registry).
//   2. Implement String() string — used by default consumers and logs.
//   3. Publish via bus.Publish(MyEvent{...}).
//
// Conventions:
//   - Use UpdateInfo{Operation, Message, Data} for generic progress; only
//     define a new type when you have unique structured fields.
//   - Throttle high-frequency publishes at the call site — slow subscribers
//     drop, and console floods are unfriendly.
//   - Namespace event names if defined outside core (e.g. gui.ScreenChangedInfo).
//   - Per-subscriber FIFO is preserved; cross-subscriber order is not.
//   - Bus delivery is non-blocking and observability-grade. For durable record
//     (audit, billing), attach a file-writing subscriber — out of scope here,
//     trivial when needed.
type Event = fmt.Stringer

type StartInfo struct{ Operation string }

func (s StartInfo) String() string { return fmt.Sprintf("start %s", s.Operation) }

type UpdateInfo struct {
	Operation, Message string
	Data               map[string]any
}

func (u UpdateInfo) String() string {
	if p, ok := u.Data["percent"]; ok {
		return fmt.Sprintf("%s: %s (%.1f%%)", u.Operation, u.Message, p)
	}
	return fmt.Sprintf("%s: %s", u.Operation, u.Message)
}

type FinishInfo struct{ Operation string }

func (f FinishInfo) String() string { return fmt.Sprintf("finish %s", f.Operation) }

type ErrorInfo struct {
	Operation string
	Err       error
}

func (e ErrorInfo) String() string { return fmt.Sprintf("error %s: %v", e.Operation, e.Err) }

type StateChangedInfo struct{ From, To, RunID string }

func (s StateChangedInfo) String() string { return fmt.Sprintf("%s → %s", s.From, s.To) }

type StateFailedInfo struct {
	State, RunID string
	Err          error
}

func (s StateFailedInfo) String() string { return fmt.Sprintf("failed in %s: %v", s.State, s.Err) }

// LockAcquiredInfo is published by Acquiring once the remote lock is
// taken. The heartbeat supervisor subscribes, starts a beat goroutine
// for the run, and writes HeartbeatAt on Interval.
type LockAcquiredInfo struct {
	RunID    string
	LockID   string
	Interval time.Duration
}

func (l LockAcquiredInfo) String() string {
	return fmt.Sprintf("lock acquired run=%s interval=%s", l.RunID, l.Interval)
}

// LockReleasedInfo is published by Unlocking after lock release. The
// heartbeat supervisor stops its beat goroutine for the run.
type LockReleasedInfo struct {
	RunID string
}

func (l LockReleasedInfo) String() string { return fmt.Sprintf("lock released run=%s", l.RunID) }

// LockLostInfo is published by the heartbeat supervisor when a beat
// cycle discovers the manifest's LockedBy no longer matches this run,
// meaning another client took over the stale lease. Locked-span stages
// observe this event to cancel their work and short-circuit to Failed.
type LockLostInfo struct {
	RunID  string
	Reason string
}

func (l LockLostInfo) String() string {
	return fmt.Sprintf("lock lost run=%s reason=%s", l.RunID, l.Reason)
}

// RetryAttemptInfo is published by the R2 adapter on each retry attempt.
// Key is the object key (or empty for ops that don't target a single key, e.g. List).
// Attempt is 1-indexed; Operation is adapter.method (e.g. "r2.Get", "r2.DeleteBatch").
type RetryAttemptInfo struct {
	Operation string
	Key       string
	Attempt   uint
	Err       error
}

func (r RetryAttemptInfo) String() string {
	if r.Key == "" {
		return fmt.Sprintf("retry %s attempt=%d err=%v", r.Operation, r.Attempt, r.Err)
	}
	return fmt.Sprintf("retry %s key=%s attempt=%d err=%v", r.Operation, r.Key, r.Attempt, r.Err)
}

type ServerStartingInfo struct{}

func (ServerStartingInfo) String() string { return "server starting" }

type ServerReadyInfo struct{}

func (ServerReadyInfo) String() string { return "server ready" }

type ServerOutputInfo struct{ Line string }

func (s ServerOutputInfo) String() string { return s.Line }

type ServerStoppedInfo struct{}

func (ServerStoppedInfo) String() string { return "server stopped" }

type ServerCrashedInfo struct{ Err error }

func (s ServerCrashedInfo) String() string { return fmt.Sprintf("server crashed: %v", s.Err) }

type SaveRequested struct{}

func (SaveRequested) String() string { return "save requested" }

type SaveCompleted struct{}

func (SaveCompleted) String() string { return "save completed" }
