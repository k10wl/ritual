package ritual

import (
	"fmt"
	"time"
)

// StartInfo marks the start of a named operation.
type StartInfo struct{ Operation string }

func (s StartInfo) String() string { return "start " + s.Operation }

// PlanInfo announces the byte/file budget of a transfer batch (pull or
// push) before any blob streams. Projections use BytesTotal as the
// progress-bar denominator so the bar shows real percent on the first
// Tick instead of staying at 0% until the run finishes.
type PlanInfo struct {
	Operation  string
	BytesTotal int64
	FilesTotal int
}

func (p PlanInfo) String() string {
	return fmt.Sprintf("plan %s files=%d bytes=%d", p.Operation, p.FilesTotal, p.BytesTotal)
}

// UpdateInfo is a generic progress event with optional structured Data.
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

// FinishInfo marks the successful end of a named operation.
type FinishInfo struct{ Operation string }

func (f FinishInfo) String() string { return "finish " + f.Operation }

// ErrorInfo reports a failure in a named operation.
type ErrorInfo struct {
	Operation string
	Err       error
}

func (e ErrorInfo) String() string { return fmt.Sprintf("error %s: %v", e.Operation, e.Err) }

// Flow identifies which pipeline a run is driving (design-log/031). The
// projection folds FlowStartedInfo to disambiguate the sync flows from the
// session: Download and Upload reuse the session's stage nodes, so stage
// name alone can't tell the dial which direction is in flight.
type Flow string

const (
	FlowSession      Flow = "session"
	FlowDownload     Flow = "download"
	FlowUpload       Flow = "upload"
	FlowLocalSession Flow = "local-session"
	// FlowRestore is the world-save rollback flow (design-log/038). It reuses
	// the download dial beat (bytes flow in) — the projection only needs the
	// flow value to name the gesture in logs; no bespoke dial colour.
	FlowRestore Flow = "restore"
)

// FlowStartedInfo is published by the lifecycle at the start of every run,
// before the runner drives the first stage. The GUI projection uses it to
// render Download as one honest "downloading" beat and Upload as one
// "saving" beat, instead of inheriting the session's stage→phase map.
type FlowStartedInfo struct{ Flow Flow }

func (f FlowStartedInfo) String() string { return "flow started " + string(f.Flow) }

// StateChangedInfo fires on every state machine transition.
type StateChangedInfo struct{ From, To, RunID string }

func (s StateChangedInfo) String() string { return fmt.Sprintf("%s → %s", s.From, s.To) }

// StateFailedInfo fires when a state's Run returns a non-nil error.
type StateFailedInfo struct {
	State, RunID string
	Err          error
}

func (s StateFailedInfo) String() string { return fmt.Sprintf("failed in %s: %v", s.State, s.Err) }

// LockAcquiredInfo is published by Acquiring once the remote lock is
// taken. The heartbeat supervisor subscribes, starts a beat goroutine
// for the run, and calls lock.Locker.Heartbeat on Interval.
type LockAcquiredInfo struct {
	RunID     string
	SessionID string
	Interval  time.Duration
}

func (l LockAcquiredInfo) String() string {
	return fmt.Sprintf("lock acquired run=%s interval=%s", l.RunID, l.Interval)
}

// LockReleasedInfo is published by Unlocking after lock release. The
// heartbeat supervisor stops its beat goroutine for the run.
type LockReleasedInfo struct {
	RunID string
}

func (l LockReleasedInfo) String() string { return "lock released run=" + l.RunID }

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
