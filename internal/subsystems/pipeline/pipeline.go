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
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/pushing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
	"time"
)

// Deps bundles every port + value the chain needs. Field order matches
// chain order (Checking → Pulling → … → Unlocking) so the call site
// reads top-to-bottom.
type Deps struct {
	Bus               ports.EventBus
	Checks            []checks.Check
	Puller            ports.Puller
	Applier           ports.Applier
	HeadResolver      pulling.HeadResolver
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
}

// Build wires the chain and returns the entry strategy. failed.* nodes
// use SetRetry back-edges so a retry re-enters at the failed stage.
// Retaining is wired with a side-specific failed instance per spec
// §2297 so retry re-enters the side that actually failed.
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
	pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetLocal, push)
	commit := committing.New(d.Committer, d.CommitOpts, pruneLocal, failCommit)
	run := running.New(d.CmdBuilder, d.Readiness, commit, unlock)
	acquire := acquiring.New(d.AcquireFn, d.InspectFn, d.HeartbeatInterval, run, failAcq)
	pull := pulling.New(d.Puller, d.Applier, d.HeadResolver, acquire, failPull)
	check := checking.New(d.Checks, pull, failCheck)

	failCheck.SetRetry(check)
	failPull.SetRetry(pull)
	failAcq.SetRetry(acquire)
	failCommit.SetRetry(commit)
	failPush.SetRetry(push)
	failRetLocal.SetRetry(pruneLocal)
	failRetRemote.SetRetry(pruneRemote)

	return check
}
