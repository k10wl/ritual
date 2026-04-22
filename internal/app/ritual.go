package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/backup"
	"ritual/internal/core/stages/checking"
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/fetching"
	"ritual/internal/core/stages/publishing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
	"sync/atomic"
	"time"
)

// Ritual orchestrates the full backup pipeline via bus-driven dispatch.
// New wires the strategy chain once; Listen routes command events to
// start/stop/retry.
type Ritual struct {
	bus             ports.EventBus
	localStorage    ports.StorageRepository
	remoteStorage   ports.StorageRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	conds           []ports.ConditionService
	updaters        []ports.UpdaterService
	exitUpdaters    []ports.UpdaterService
	retentions      []retaining.Job
	cmdBuilder      ports.CmdBuilder
	readiness       ports.ReadinessCheck

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
	localManifests ports.ManifestStore,
	remoteManifests ports.ManifestStore,
	conditions []ports.ConditionService,
	updaters []ports.UpdaterService,
	exitUpdaters []ports.UpdaterService,
	retentions []retaining.Job,
	cmdBuilder ports.CmdBuilder,
	readiness ports.ReadinessCheck,
) *Ritual {
	r := &Ritual{
		bus:             bus,
		localStorage:    localStorage,
		remoteStorage:   remoteStorage,
		localManifests:  localManifests,
		remoteManifests: remoteManifests,
		conds:           conditions,
		updaters:        updaters,
		exitUpdaters:    exitUpdaters,
		retentions:      retentions,
		cmdBuilder:      cmdBuilder,
		readiness:       readiness,
		status:          Idle,
	}
	r.entry = r.buildChain()
	return r
}

// Listen subscribes to the bus and dispatches command events until ctx
// is cancelled or the channel closes.
func (r *Ritual) Listen(ctx context.Context) {
	ch, unsub := r.bus.Subscribe()
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
		err := r.runner.RunCurrent(ctx)
		r.resolveStatus(ctx, err)
	}()
}

func (r *Ritual) setStatus(status Outcome) {
	r.status = status
	r.bus.Publish(StatusChanged{Status: status})
}

func (r *Ritual) buildChain() machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failFetch := failed.New(ritual.StageFetching)
	failAcq := failed.New(ritual.StageAcquiring)
	failRet := failed.New(ritual.StageRetaining)

	retain := retaining.New(r.retentions, r.bus, failRet)
	unlock := unlocking.New(r.localManifests, r.remoteManifests, retain)
	backupStage := backup.New(r.localStorage, r.remoteStorage, r.localManifests, unlock)
	publish := publishing.New(r.exitUpdaters, backupStage)
	run := running.New(r.cmdBuilder, r.readiness, publish, unlock)
	rollback := unlocking.New(r.localManifests, r.remoteManifests, failAcq)
	acquire := acquiring.New(r.localManifests, r.remoteManifests, run, failAcq, rollback)
	fetch := fetching.New(r.updaters, acquire, failFetch)
	check := checking.New(r.conds, fetch, failCheck)

	failCheck.SetRetry(check)
	failFetch.SetRetry(fetch)
	failAcq.SetRetry(acquire)
	failRet.SetRetry(retain)

	return check
}
