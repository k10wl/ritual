package observed

import "fmt"

// Update* events are the single stream that drives BOTH the dial (projection
// folds them into PhasePreflight/PhaseUpdating) and the on-disk log
// (logging.write formats them) — design-log/037 §Q8. They are published by
// the observed.Updater decorator and the relaunch closure, so the gray-dial
// UX and the audit trail cannot drift.

// UpdateCheckStarted fires before a Check runs. It is the entry signal for the
// gray "Checking for updates···" Preflight beat — emitted on both launch and
// manual re-check, so neither path hand-rolls the state (design-log/037 §Q6).
type UpdateCheckStarted struct{}

func (e UpdateCheckStarted) String() string { return "update check: started" }

// UpdateCheckInfo fires after a Check completes. Outdated reports whether the
// running From is older than the latest remote To. Err is the listing/decision
// error (offline, malformed); To is empty when the remote has nothing.
type UpdateCheckInfo struct {
	From       string
	To         string
	Outdated   bool
	Candidates int // keys listed under the platform prefix (reasoning for the decision)
	Err        error
}

func (e UpdateCheckInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("update check: %s err=%v", e.From, e.Err)
	}
	if e.To == "" {
		// Distinguish an empty remote from one that had keys we couldn't parse —
		// the latter is a real misconfiguration (wrong key shape) that otherwise
		// hides behind the same "no remote build" line.
		if e.Candidates > 0 {
			return fmt.Sprintf("update check: %s — no usable remote build (%d key(s) under prefix, none parsed as <version>/<sha>)", e.From, e.Candidates)
		}
		return fmt.Sprintf("update check: %s — no remote build (prefix empty)", e.From)
	}
	if e.Outdated {
		return fmt.Sprintf("update check: %s → %s (outdated, %d candidate(s))", e.From, e.To, e.Candidates)
	}
	return fmt.Sprintf("update check: %s (up to date, remote %s, %d candidate(s))", e.From, e.To, e.Candidates)
}

// UpdateApplyStarted fires before the byte replace begins. Entry signal for the
// gray ring-fill "Updating → vN" beat.
type UpdateApplyStarted struct{ Version string }

func (e UpdateApplyStarted) String() string {
	return "update apply: started → " + e.Version
}

// UpdateApplyInfo fires after Apply returns. On success Apply does not return
// (the process is replaced), so a non-nil Err is the common case here:
// minio rolled back, the running binary is intact.
type UpdateApplyInfo struct {
	Version string
	Err     error
}

func (e UpdateApplyInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("update apply: %s err=%v", e.Version, e.Err)
	}
	return fmt.Sprintf("update apply: %s ok", e.Version)
}

// UpdateRestartInfo fires from the relaunch closure just before exec + Quit —
// the brief "Restarting···" sub-copy of the Updating beat.
type UpdateRestartInfo struct{ Version string }

func (e UpdateRestartInfo) String() string {
	if e.Version == "" {
		return "update restart: relaunching"
	}
	return "update restart: relaunching → " + e.Version
}

// UpdateCleanupInfo fires at startup when the leftover ".old" backup from a
// prior in-place update is swept — Windows can't delete the running binary
// mid-update, so minio/selfupdate's sidecar lingers until the next launch.
// Published only when there is something to report (a removal or an error), so
// a clean launch stays quiet.
type UpdateCleanupInfo struct {
	Path    string
	Removed bool
	Err     error
}

func (e UpdateCleanupInfo) String() string {
	if e.Err != nil {
		return fmt.Sprintf("update cleanup: %s err=%v", e.Path, e.Err)
	}
	return "update cleanup: removed stale backup " + e.Path
}

// UpdateFailed fires on any update failure so the failure is a first-class
// event, not just a return value (design-log/037 §Q8). Stage is "check" or
// "apply"; the projection routes it to PhaseFailed (017's single pathway).
type UpdateFailed struct {
	Stage string
	Err   error
}

func (e UpdateFailed) String() string {
	return fmt.Sprintf("update failed [%s]: %v", e.Stage, e.Err)
}
