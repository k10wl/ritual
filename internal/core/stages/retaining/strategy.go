// Package retaining runs the retention + GC Jobs paired with the verb that
// created new refs. Runs while the lease is still held — the chain wires
// pruneLocal → push → pruneRemote → unlock per spec §2303-2304, so the
// preceding Unlocking has not run yet. Each Job is invoked even if earlier
// Jobs failed; their errors are joined and routed to onFail at the end of
// the stage.
package retaining

import (
	"context"
	"errors"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the retaining stage. Chainable: on success, advances
// to onOK so the stage can be wired twice (local + remote) back-to-back per
// spec §2285. A nil onOK terminates the machine.
type Strategy struct {
	jobs   []Job
	bus    ports.EventBus
	onOK   machine.Strategy[ritual.RunState]
	onFail machine.Strategy[ritual.RunState]
}

// New builds a retaining Strategy. Pass nil onOK for a terminal stage.
func New(jobs []Job, bus ports.EventBus, onFail, onOK machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{jobs: jobs, bus: bus, onOK: onOK, onFail: onFail}
}

// Name returns the stage name for logging.
func (*Strategy) Name() string { return ritual.StageRetaining }

// Run invokes every Job, emitting Retention*/GC* lifecycle events keyed off
// each Job's Kind+Label, accumulating errors via errors.Join. A non-nil
// joined error routes to onFail via rs.Err.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(s.bus, ritual.StartInfo{Operation: "retain"})

	var errs []error
	for _, job := range s.jobs {
		publishStart(s.bus, job)
		err := job.Run(ctx)
		publishFinish(s.bus, job, err)
		if err != nil {
			errs = append(errs, err)
		}
	}

	publish(s.bus, ritual.FinishInfo{Operation: "retain"})

	if err := errors.Join(errs...); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	if rs.Err != nil {
		return s.onFail, nil //nolint:nilnil // rs.Err came from upstream stage; onFail routes it
	}
	return s.onOK, nil //nolint:nilnil // onOK==nil is intentional terminal signal for the last prune instance
}

func publishStart(bus ports.EventBus, j Job) {
	if bus == nil {
		return
	}
	switch j.Kind {
	case KindRetention:
		bus.Publish(RetentionStartedInfo{Label: j.Label})
	case KindGC:
		bus.Publish(GCStartedInfo{Label: j.Label})
	}
}

func publishFinish(bus ports.EventBus, j Job, err error) {
	if bus == nil {
		return
	}
	switch j.Kind {
	case KindRetention:
		bus.Publish(RetentionFinishedInfo{Label: j.Label, Err: err})
	case KindGC:
		bus.Publish(GCFinishedInfo{Label: j.Label, Err: err})
	}
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
