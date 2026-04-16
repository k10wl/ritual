package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/archiving"
	"ritual/internal/core/stages/checking"
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/fetching"
	"ritual/internal/core/stages/publishing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
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
	retentions      []ports.RetentionService
	cmdBuilder      ports.CmdBuilder

	entry  machine.Strategy[ritual.RunState]
	runner *ritual.Runner
	status Outcome
	cancel context.CancelFunc
}

func New(
	bus ports.EventBus,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	localManifests ports.ManifestStore,
	remoteManifests ports.ManifestStore,
	conditions []ports.ConditionService,
	updaters []ports.UpdaterService,
	exitUpdaters []ports.UpdaterService,
	retentions []ports.RetentionService,
	cmdBuilder ports.CmdBuilder,
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
	if r.status != Idle {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot start: status is %s", r.status)})
		return
	}
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	runState := &ritual.RunState{RunID: runID, Bus: r.bus}
	r.runner = ritual.NewRunner(runState)

	err := r.runner.Run(ctx, r.entry)
	if err != nil {
		r.setStatus(Failed)
		return
	}
	r.setStatus(Done)
}

func (r *Ritual) stop() {
	if r.status != Running {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Ritual) retry(ctx context.Context) {
	if r.status != Failed {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot retry: status is %s", r.status)})
		return
	}
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	r.runner.RunState().Err = nil
	err := r.runner.RunCurrent(ctx)
	if err != nil {
		r.setStatus(Failed)
		return
	}
	r.setStatus(Done)
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

	retain := retaining.New(r.retentions, failRet)
	unlock := unlocking.New(r.localManifests, r.remoteManifests, retain)
	archive := archiving.New(r.localStorage, r.remoteStorage, r.localManifests, unlock)
	publish := publishing.New(r.exitUpdaters, archive)
	run := running.New(r.cmdBuilder, publish)
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
