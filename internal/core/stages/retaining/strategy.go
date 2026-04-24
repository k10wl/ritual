// Package retaining runs the retention Jobs. Runs without WithoutCancel —
// lock is already released by the preceding Unlocking stage. Each Job is
// invoked even if earlier Jobs failed; their errors are joined and routed
// to onFail at the end of the stage.
package retaining

import (
	"context"
	"errors"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the retaining stage. Chainable: on success, advances to
// onOK so the stage can be wired twice (local + remote) back-to-back per spec
// §2285. A nil onOK terminates the machine.
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

// Run invokes every Job, accumulating their errors via errors.Join.
// Error routes to onFail via rs.Err; otherwise terminates the run.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(s.bus, ritual.StartInfo{Operation: "retain"})

	var errs []error
	for _, job := range s.jobs {
		if err := job(ctx); err != nil {
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

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
