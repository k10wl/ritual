package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ritual/internal/adapters/observed"
	"ritual/internal/config"
	"ritual/internal/core/checks"
	"ritual/internal/core/lock"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/subsystems/pipeline"
	"sync/atomic"
	"time"
)

// Ritual orchestrates the full backup pipeline via bus-driven dispatch.
// New wires the strategy chain once; Listen routes command events to
// start/stop/retry.
type Ritual struct {
	bus              ports.EventBus
	localStorage     ports.StorageRepository
	remoteStorage    ports.StorageRepository
	checks           []checks.Check
	puller           ports.Puller
	applier          ports.Applier
	headResolver     pulling.HeadResolver
	committer        ports.Committer
	pusher           ports.Pusher
	commitTargets    []string
	localRetentions  []retaining.Job
	remoteRetentions []retaining.Job
	cmdBuilder       ports.CmdBuilder
	readiness        ports.ReadinessCheck
	locker           *observed.Locker

	entry    machine.Strategy[ritual.RunState]
	runner   *ritual.Runner
	status   Outcome
	cancel   context.CancelFunc
	userStop atomic.Bool
}

// New builds the Ritual orchestrator with the supplied ports and subsystems.
func New(
	bus ports.EventBus,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	preflightChecks []checks.Check,
	puller ports.Puller,
	applier ports.Applier,
	headResolver pulling.HeadResolver,
	committer ports.Committer,
	pusher ports.Pusher,
	commitTargets []string,
	localRetentions, remoteRetentions []retaining.Job,
	cmdBuilder ports.CmdBuilder,
	readiness ports.ReadinessCheck,
) *Ritual {
	host, _ := os.Hostname()
	r := &Ritual{
		bus:              bus,
		localStorage:     localStorage,
		remoteStorage:    remoteStorage,
		checks:           preflightChecks,
		puller:           puller,
		applier:          applier,
		headResolver:     headResolver,
		committer:        committer,
		pusher:           pusher,
		commitTargets:    commitTargets,
		localRetentions:  localRetentions,
		remoteRetentions: remoteRetentions,
		cmdBuilder:       cmdBuilder,
		readiness:        readiness,
		locker:           observed.NewLocker(lock.New(remoteStorage, host), bus),
		status:           Idle,
	}
	r.entry = r.buildChain()
	return r
}

// Heartbeat returns the heartbeat callback bound to this Ritual's Locker.
// Composition root passes it to heartbeat.Attach so the supervisor
// refreshes the same lease that Acquiring claimed.
func (r *Ritual) Heartbeat(ctx context.Context, sessionID string) error {
	return r.locker.Heartbeat(ctx, sessionID)
}

// SetHeartbeatInterval overrides the lease heartbeat cadence on the
// internal Locker. Integration tests use this to drive sub-second sync
// ticks; production keeps the 1-minute default.
func (r *Ritual) SetHeartbeatInterval(d time.Duration) {
	r.locker.SetHeartbeatInterval(d)
	r.entry = r.buildChain()
}

// Listen subscribes to the bus and spawns a goroutine that dispatches
// command events until ctx is cancelled or the channel closes. Subscription
// happens synchronously before Listen returns — callers that Publish on the
// bus immediately after Listen returns are guaranteed delivery. Wrapping
// Listen in `go` is a race: the goroutine may not have subscribed by the
// time Publish fans out, silently dropping the event.
func (r *Ritual) Listen(ctx context.Context) {
	ch, unsub := r.bus.Subscribe()
	go r.consume(ctx, ch, unsub)
}

func (r *Ritual) consume(ctx context.Context, ch <-chan ports.Event, unsub func()) {
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			switch event.(type) {
			case StartRequested:
				r.start(ctx)
			case StopRequested:
				r.stop()
			case RetryRequested:
				r.retry(ctx)
			}
		}
	}
}

func (r *Ritual) start(ctx context.Context) {
	if r.status == Running {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot start: already %s", r.status)})
		return
	}
	r.userStop.Store(false)
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	runState := &ritual.RunState{RunID: runID, Bus: r.bus}
	r.runner = ritual.NewRunner(runState)

	go func() {
		err := r.runner.Run(ctx, r.entry)
		if err == nil {
			err = runState.Err
		}
		r.resolveStatus(ctx, err)
	}()
}

func (r *Ritual) stop() {
	if r.status != Running {
		return
	}
	r.userStop.Store(true)
	if r.cancel != nil {
		r.cancel()
	}
}

// resolveStatus maps runner exit into Done/Failed. A user-initiated stop
// is graceful: any errors downstream of the cancelled ctx (e.g.
// cmd.Build returning context.Canceled mid-boot) are expected, not real
// failures, so they resolve to Done. Non-cancel errors from stages still
// propagate as Failed even during a user stop.
func (r *Ritual) resolveStatus(ctx context.Context, runErr error) {
	if r.userStop.Load() && (runErr == nil || errors.Is(runErr, context.Canceled)) {
		r.setStatus(Done)
		return
	}
	if runErr != nil || ctx.Err() != nil {
		r.setStatus(Failed)
		return
	}
	r.setStatus(Done)
}

func (r *Ritual) retry(ctx context.Context) {
	if r.status != Failed {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot retry: status is %s", r.status)})
		return
	}
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	r.userStop.Store(false)
	r.runner.RunState().Err = nil
	go func() {
		err := r.runner.Resume(ctx, r.entry)
		if err == nil {
			err = r.runner.RunState().Err
		}
		r.resolveStatus(ctx, err)
	}()
}

func (r *Ritual) setStatus(status Outcome) {
	r.status = status
	r.bus.Publish(StatusChanged{Status: status})
}

func (r *Ritual) buildChain() machine.Strategy[ritual.RunState] {
	return pipeline.Build(pipeline.Deps{
		Bus:               r.bus,
		Checks:            r.checks,
		Puller:            r.puller,
		Applier:           r.applier,
		HeadResolver:      r.headResolver,
		Committer:         r.committer,
		CommitOpts:        NewCommitOptsResolver(r.commitTargets),
		Pusher:            r.pusher,
		LocalRetentions:   r.localRetentions,
		RemoteRetentions:  r.remoteRetentions,
		CmdBuilder:        r.cmdBuilder,
		Readiness:         r.readiness,
		AcquireFn:         r.locker.Acquire,
		InspectFn:         r.locker.Inspect,
		ReleaseFn:         r.locker.Release,
		HeartbeatInterval: r.locker.HeartbeatInterval(),
	})
}
