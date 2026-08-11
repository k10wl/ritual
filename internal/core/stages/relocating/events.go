package relocating

import "fmt"

// Relocate* events are the single stream that drives BOTH the dial
// (projection folds them into StageRelocating/PhaseRelocating) and the
// on-disk log (logging.write's default %v branch formats them via
// String()) — same shape as internal/adapters/observed's Update* events
// (design-log/037 §Q8). Dedicated types rather than the generic
// ritual.StartInfo/PlanInfo/UpdateInfo/FinishInfo/ErrorInfo: those are
// switched on by concrete type alone in projection.fold() with no
// Operation check (design-log/055 addendum — a ritual.PlanInfo published
// here once landed straight on the session ViewModel), and PlanInfo's own
// doc comment scopes it to pull/push. A relocate-owned vocabulary can't
// collide with the session's, by construction.

// RelocateStarted fires once at the top of Strategy.Run.
type RelocateStarted struct{}

func (RelocateStarted) String() string { return "relocate: started" }

// RelocatePlanned fires after planCopy, before any bytes move — the
// progress-bar denominator, mirroring ritual.PlanInfo's role for pull/push.
type RelocatePlanned struct {
	BytesTotal int64
	FilesTotal int
}

func (e RelocatePlanned) String() string {
	return fmt.Sprintf("relocate: planned files=%d bytes=%d", e.FilesTotal, e.BytesTotal)
}

// RelocateProgress fires periodically during copyContent. FilesDone/
// FilesTotal (not a pre-computed percent) so the projection derives
// Progress the same way it would from any other counter — one fewer unit
// conversion to keep in sync between here and there.
type RelocateProgress struct {
	FilesDone  int
	FilesTotal int
}

func (e RelocateProgress) String() string {
	return fmt.Sprintf("relocate: copying %d/%d files", e.FilesDone, e.FilesTotal)
}

// RelocateVerifying fires once copyContent finishes, before verify() reads
// the destination back. No byte/file counter — verify is a fixed-cost pass
// over what was just written, not incremental.
type RelocateVerifying struct{}

func (RelocateVerifying) String() string { return "relocate: verifying" }

// RelocateCommitting fires once verify() passes, before the facade swap +
// settings.Save().
type RelocateCommitting struct{}

func (RelocateCommitting) String() string { return "relocate: committing" }

// RelocateFinished fires on success, after cleanup() of the old root.
type RelocateFinished struct{}

func (RelocateFinished) String() string { return "relocate: finished" }

// RelocateFailed fires on any failure path (validate/copy/verify/commit).
type RelocateFailed struct{ Err error }

func (e RelocateFailed) String() string { return fmt.Sprintf("relocate: failed: %v", e.Err) }
