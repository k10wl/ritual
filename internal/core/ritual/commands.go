package ritual

import "ritual/internal/core/domain"

// Bus commands published from outside the FSM (GUI, CLI, scheduler) to
// drive the run lifecycle. The lifecycle subsystem subscribes and acts;
// stages never see these directly.

// StartRequested commands the lifecycle to begin a fresh run. SkipSync
// selects the local-only session pipeline (design-log/036): Checking →
// Running → Draining → Committing → Retaining(local) → Done — no remote
// Pulling/Acquiring/Pushing/Unlock. Zero value (false) is the normal full
// session, so existing StartRequested{} callers are unaffected.
type StartRequested struct{ SkipSync bool }

func (s StartRequested) String() string {
	if s.SkipSync {
		return "start requested (skip sync)"
	}
	return "start requested"
}

// DownloadRequested commands the lifecycle to run the Download flow
// (design-log/031): Checking → Pulling → Retaining(local). Server-free,
// lockless refresh of the local workdir from the remote HEAD. Rejected
// while any other flow is Running (shared status).
type DownloadRequested struct{}

func (DownloadRequested) String() string { return "download requested" }

// UploadRequested commands the lifecycle to run the Upload flow
// (design-log/031): Checking → Probing → Acquiring → Committing → Pushing →
// Retaining → Unlocking. Server-free publish of the local worlds as a new
// remote ref parented on the current remote HEAD. Rejected while any other
// flow is Running (shared status).
type UploadRequested struct{}

func (UploadRequested) String() string { return "upload requested" }

// RestoreRequested commands the lifecycle to run the Restore flow
// (design-log/038): Checking → Pulling(target) → Done. Server-free, lockless,
// read-only on the remote — it pulls the chosen historical ref RefID and
// applies it to the workdir. HEAD never moves and no ref is deleted; the
// restored workdir surfaces as dirty and recovers canonically via Publish
// ([035]). Rejected while any other flow is Running (shared status).
type RestoreRequested struct{ RefID domain.RefID }

func (r RestoreRequested) String() string { return "restore requested " + string(r.RefID) }

// RevertRequested commands the lifecycle to snap the workdir back to local
// HEAD (design-log/045 §C). Same chain shape as Restore but the target is the
// local HEAD resolver, not a per-request ref. Server-free, lockless, read-
// only on the remote. HEAD never moves; the workdir is re-applied from local
// blobs. In the dirty case this drops uncommitted edits; in the unpushed-
// only case it is an observable no-op (workdir already matches HEAD).
// Rejected while any other flow is Running (shared status).
type RevertRequested struct{}

func (RevertRequested) String() string { return "revert requested" }

// ApplyRetentionRequested commands the lifecycle to run the retention prune
// now (design-log/045 §D). Routes to a Checking → Retaining(local) →
// Retaining(remote) → Done chain that reuses the same retention jobs the
// session/sync flows already run. Rejected while any other flow is Running.
type ApplyRetentionRequested struct{}

func (ApplyRetentionRequested) String() string { return "apply retention requested" }

// StopRequested commands the lifecycle to cancel the running pipeline.
// Userstop is graceful: a cancellation propagating through stages resolves
// to Done, not Failed.
type StopRequested struct{}

func (StopRequested) String() string { return "stop requested" }

// DismissRequested acknowledges a Failed outcome and returns the
// lifecycle to Idle. Only valid when current Outcome is Failed.
// Replaces the prior retry-from-failed semantic (see design-log/017): the
// user explicitly clears the failure, then re-engages with a fresh Start.
type DismissRequested struct{}

func (DismissRequested) String() string { return "dismiss requested" }
