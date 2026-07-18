package observed

import (
	"fmt"
	"time"
)

// LeaseAcquireInfo fires once per Locker.Acquire call. Mode distinguishes
// "fresh" (empty remote slot) from "takeover" (overwriting an expired
// foreign holder). Failed acquisitions carry Err and empty SessionID.
type LeaseAcquireInfo struct {
	Mode       string // "fresh" | "takeover" | "blocked" | "error"
	SessionID  string
	DurationMs int64
	Err        error
}

func (e LeaseAcquireInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("lease acquire %s err=%v (%dms)", e.Mode, e.Err, e.DurationMs)
	}
	return fmt.Sprintf("lease acquire %s session=%s (%dms)", e.Mode, shortSession(e.SessionID), e.DurationMs)
}

// LeaseHeartbeatInfo fires once per Locker.Heartbeat call. Result
// distinguishes "ok" (lease refreshed), "taken_over" (foreign session),
// "vanished" (object absent), "transport" (read/write error).
type LeaseHeartbeatInfo struct {
	Result     string
	SessionID  string
	DurationMs int64
	Err        error
}

func (e LeaseHeartbeatInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("lease heartbeat %s session=%s err=%v (%dms)",
			e.Result, shortSession(e.SessionID), e.Err, e.DurationMs)
	}
	return fmt.Sprintf("lease heartbeat %s session=%s (%dms)",
		e.Result, shortSession(e.SessionID), e.DurationMs)
}

// LeaseReleaseInfo fires once per Locker.Release call. Mode:
//   - "owned"    — deleted our own live lease (clean shutdown)
//   - "foreign"  — another session holds the slot, silent no-op
//   - "absent"   — object already gone (crashed + swept), silent no-op
//   - "error"    — transport failure
type LeaseReleaseInfo struct {
	Mode       string
	SessionID  string
	DurationMs int64
	Err        error
}

func (e LeaseReleaseInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("lease release %s session=%s err=%v (%dms)",
			e.Mode, shortSession(e.SessionID), e.Err, e.DurationMs)
	}
	return fmt.Sprintf("lease release %s session=%s (%dms)",
		e.Mode, shortSession(e.SessionID), e.DurationMs)
}

// LeaseInspectInfo fires once per Locker.Inspect call. Empty SessionID
// means the slot was free; Stale reports whether ExpiresAt lies in the
// past at observation time.
type LeaseInspectInfo struct {
	Owner      string
	SessionID  string
	ExpiresAt  time.Time
	Stale      bool
	DurationMs int64
	Err        error
}

func (e LeaseInspectInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("lease inspect err=%v (%dms)", e.Err, e.DurationMs)
	}
	if e.SessionID == "" {
		return fmt.Sprintf("lease inspect free (%dms)", e.DurationMs)
	}
	return fmt.Sprintf("lease inspect owner=%s session=%s stale=%t (%dms)",
		e.Owner, shortSession(e.SessionID), e.Stale, e.DurationMs)
}

func shortSession(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}
