// Package pipeline wires the ritual state-machine chain. Single place
// that knows the topology — every stage's onOK/onFail edge and the
// failed.* retry back-edges. Returns the entry strategy ready for
// ritual.Runner to drive. Sixth subsystem alongside retention,
// heartbeat, conditions, logging, prompt.
package pipeline

import (
	"ritual/internal/core/checks"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/checking"
	"ritual/internal/core/stages/committing"
	"ritual/internal/core/stages/draining"
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/probing"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/pushing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
	"ritual/internal/subsystems/livesync"
	"time"
)

// Deps bundles every port + value the chain needs. Field order matches
// chain order (Checking → Pulling → … → Unlocking) so the call site
// reads top-to-bottom.
type Deps struct {
	Bus          ports.EventBus
	Checks       []checks.Check
	Puller       ports.Puller
	Applier      ports.Applier
	HeadResolver pulling.HeadResolver
	// LocalHeadResolver resolves the local-store HEAD. Publish (BuildUpload,
	// design-log/035) probes this instead of the remote so a new ref parents
	// on where the operator stands (local HEAD), making a rolled-back state
	// canonical truthfully — reverses design-log/031 §Q1.
	LocalHeadResolver pulling.HeadResolver
	Committer         ports.Committer
	CommitOpts        committing.OptsResolver
	Pusher            ports.Pusher
	LocalRetentions   []retaining.Job
	RemoteRetentions  []retaining.Job
	CmdBuilder        ports.CmdBuilder
	Readiness         ports.ReadinessCheck
	AcquireFn         acquiring.AcquireFn
	InspectFn         acquiring.InspectFn
	ReleaseFn         unlocking.ReleaseFn
	HeartbeatInterval time.Duration
	// Drainable blocks Committing until the live-sync ticker has settled
	// (design-log/016). nil → draining stage is a no-op pass-through; that
	// path is used by tests and the CLI fakerun that have no ticker.
	Drainable livesync.Drainable
}

// Build wires the chain and returns the entry strategy. failed.* nodes
// are purely terminal — design-log/017 cuts retry-from-failed in favour of
// dismiss-to-idle. Side-specific failed instances per spec §2297 still
// attribute retaining failures to the correct side via rs.FailedStage.
func Build(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failPull := failed.New(ritual.StagePulling)
	failAcq := failed.New(ritual.StageAcquiring)
	failCommit := failed.New(ritual.StageCommitting)
	failPush := failed.New(ritual.StagePushing)
	failRetLocal := failed.New(ritual.StageRetainingLocal)
	failRetRemote := failed.New(ritual.StageRetainingRemote)

	unlock := unlocking.New(d.ReleaseFn, nil)
	pruneRemote := retaining.New(d.RemoteRetentions, d.Bus, failRetRemote, unlock)
	push := pushing.New(d.Pusher, pruneRemote, failPush)
	push.OnStop(unlock)
	pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetLocal, push)
	commit := committing.New(d.Committer, d.CommitOpts, pruneLocal, failCommit)
	// Draining inserted only when a live-sync subsystem is wired. Skipped
	// otherwise so tests and the CLI fakerun keep the historical chain
	// shape (Running → Committing) — matches design-log/016 §Drain barrier
	// "no-op pass-through when livesync absent".
	var afterRunning machine.Strategy[ritual.RunState] = commit
	if d.Drainable != nil {
		afterRunning = draining.New(d.Drainable, commit)
	}
	run := running.New(d.CmdBuilder, d.Readiness, afterRunning, unlock)
	acquire := acquiring.New(d.AcquireFn, d.InspectFn, d.HeartbeatInterval, run, failAcq)
	pull := pulling.New(d.Puller, d.Applier, d.HeadResolver, acquire, failPull)
	check := checking.New(d.Checks, pull, failCheck)

	return check
}

// BuildDownload wires the server-free Download flow (design-log/031):
// Checking → Pulling → Retaining(local) → Done. Read-only on the remote, so
// no Acquiring, no Pushing, no Unlocking — a Download never mutates remote
// state and must not block a teammate who is about to play. Local retention
// runs after the pull so repeated refreshes don't pile up local refs. The
// trailing retaining's nil onOK terminates the chain (Done).
func BuildDownload(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failPull := failed.New(ritual.StagePulling)
	failRetLocal := failed.New(ritual.StageRetainingLocal)

	pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetLocal, nil)
	pull := pulling.New(d.Puller, d.Applier, d.HeadResolver, pruneLocal, failPull)
	check := checking.New(d.Checks, pull, failCheck)

	return check
}

// BuildUpload wires the server-free Publish flow (design-log/031 Upload,
// re-pointed by design-log/035): Checking → Probing → Acquiring → Committing
// → Pushing → Retaining → Unlocking. Probing is head-only (no download/apply
// — local files win); it resolves the LOCAL HEAD and writes rs.ParentRefID so
// the unchanged CommitOptsResolver records Parent = local HEAD (design-log/035
// §Q3 — lineage follows where the operator stands; a rolled-back state becomes
// canonical truthfully). failed.* terminals mirror Build, plus
// failed.New(StageProbing). User-facing name is "Publish" (design-log/035);
// the wire name stays Upload (rename deferred to reduce churn).
func BuildUpload(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failProbe := failed.New(ritual.StageProbing)
	failAcq := failed.New(ritual.StageAcquiring)
	failCommit := failed.New(ritual.StageCommitting)
	failPush := failed.New(ritual.StagePushing)
	failRetLocal := failed.New(ritual.StageRetainingLocal)
	failRetRemote := failed.New(ritual.StageRetainingRemote)

	unlock := unlocking.New(d.ReleaseFn, nil)
	pruneRemote := retaining.New(d.RemoteRetentions, d.Bus, failRetRemote, unlock)
	push := pushing.New(d.Pusher, pruneRemote, failPush)
	push.OnStop(unlock)
	pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetLocal, push)
	commit := committing.New(d.Committer, d.CommitOpts, pruneLocal, failCommit)
	acquire := acquiring.New(d.AcquireFn, d.InspectFn, d.HeartbeatInterval, commit, failAcq)
	probe := probing.New(d.LocalHeadResolver, acquire, failProbe)
	check := checking.New(d.Checks, probe, failCheck)

	return check
}

// BuildRestore wires the world-save rollback flow (design-log/038):
// Checking → Pulling(target) → Done. Identical in family to BuildDownload but
// the pull is **target-pinned** (pulling.FromTarget reads rs.TargetRefID set by
// the lifecycle from RestoreRequested) instead of HEAD-resolved, and there is
// **no** trailing retention: a restore pulls an existing ref (adds none), so
// nothing new is to prune and pruning the just-restored old ref would not
// affect the already-applied workdir. Read-only on the remote — no Acquiring
// (lock), no Pushing, no Unlocking — so a restore never blocks a teammate. HEAD
// is unchanged (the old id < newest), so the restored workdir surfaces as dirty
// and recovers canonically via [035] Publish. The trailing nil onOK terminates
// the chain (Done); pulling.FromTarget's ErrNoTarget routes to failPull (a
// restore with no id is a wiring bug, not a no-op like ErrNoHead).
func BuildRestore(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failPull := failed.New(ritual.StagePulling)

	pull := pulling.NewWithResolver(d.Puller, d.Applier, pulling.FromTarget(), nil, failPull)
	check := checking.New(d.Checks, pull, failCheck)

	return check
}

// BuildRevert wires the "snap workdir back to local HEAD" flow (design-log/045
// §C): Checking → Pulling(FromHead(localHeadResolver)) → Done. Same family as
// BuildRestore but the resolver is the local HEAD (not a per-request target),
// so the chain has no `rs.TargetRefID` dependency. Read-only on the remote
// (no Acquiring/Pushing/Unlocking), and no retention — re-applying an existing
// ref creates nothing to prune. HEAD never moves; the workdir is restored from
// local blobs. The trailing nil onOK terminates the chain (Done). On the
// `ErrNoHead` path (no local refs yet — fresh install) the Pulling stage
// short-circuits to onOK as a no-op success; Revert is a no-op there, the
// honest outcome.
func BuildRevert(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failPull := failed.New(ritual.StagePulling)

	pull := pulling.New(d.Puller, d.Applier, d.LocalHeadResolver, nil, failPull)
	check := checking.New(d.Checks, pull, failCheck)

	return check
}

// BuildRetentionApply wires the user-triggered prune-now flow (design-log/045
// §D): Checking → Retaining(local) → Retaining(remote) → Done. Reuses the
// session/sync flows' Jobs unchanged (rules are read fresh from settings.json
// at Select time per [[039]]), so applying the latest Apply'd policy needs no
// new wiring beyond the chain. No Pulling/Pushing/Acquiring — the prune
// operates entirely on the manifest keyspace (refs/, objects/), and remote
// retention is the same lockless Delete pattern the sync flows already use.
// A remote failure routes to failRetRemote (Failed with attribution); local
// always runs first so a remote outage cannot block local cleanup.
func BuildRetentionApply(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failRetLocal := failed.New(ritual.StageRetainingLocal)
	failRetRemote := failed.New(ritual.StageRetainingRemote)

	pruneRemote := retaining.New(d.RemoteRetentions, d.Bus, failRetRemote, nil)
	pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetLocal, pruneRemote)
	check := checking.New(d.Checks, pruneLocal, failCheck)

	return check
}

// BuildLocalSession wires the skip-sync / local-only session (design-log/036,
// no-save reversal): Checking → Running → Done. The entire save half is
// dropped — no Pulling, no Acquiring (lock), no Committing, no Retaining, no
// Pushing, no Unlocking, no livesync — so the server runs on the on-disk
// worlds as-is (offline / R2-down / rollback / mod-testing) and **saves
// nothing to a ref** ("skip sync means we won't save either" — §OQ2). The
// session's work lands in the workdir and is recovered deliberately afterward
// via [035] Publish, which reads the workdir as dirty. running.New(…, nil,
// nil): a clean exit terminates to Done; a crash sets rs.Err and lifecycle
// resolves it to Failed. Skipping is structural — the save nodes are simply
// absent, so the livesync ticker is wholly inert and no rs.LocalOnly gate is
// needed (§OQ5).
func BuildLocalSession(d Deps) machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)

	run := running.New(d.CmdBuilder, d.Readiness, nil, nil)
	check := checking.New(d.Checks, run, failCheck)
	return check
}
