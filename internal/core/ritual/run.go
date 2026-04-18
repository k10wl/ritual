package ritual

import (
	"context"
	"errors"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Runner drives the state machine and tracks the current strategy
// so RunCurrent can re-enter after a failure.
type Runner struct {
	runState *RunState
	current  machine.Strategy[RunState]
}

// NewRunner returns a Runner bound to runState.
func NewRunner(runState *RunState) *Runner {
	return &Runner{runState: runState}
}

// RunState returns the runner's shared state.
func (r *Runner) RunState() *RunState { return r.runState }

// Run drives the machine from the given start strategy.
func (r *Runner) Run(ctx context.Context, start machine.Strategy[RunState]) error {
	r.current = start
	return r.drive(ctx)
}

// RunCurrent re-enters the machine at the last stopped strategy.
func (r *Runner) RunCurrent(ctx context.Context) error {
	if r.current == nil {
		return errors.New("no current strategy to resume")
	}
	return r.drive(ctx)
}

// drive runs the state machine loop. On error, r.current stays on the
// strategy that errored so RunCurrent re-enters there (e.g. the failed
// strategy follows its onRetry back-edge on the next attempt).
func (r *Runner) drive(ctx context.Context) error {
	rs := r.runState
	for r.current != nil {
		curName := stageName(r.current)
		next, err := r.current.Run(ctx, rs)
		if err != nil {
			return err
		}
		nextName := stageName(next)
		publish(rs.Bus, StateChangedInfo{From: curName, To: nextName, RunID: rs.RunID})
		r.current = next
	}
	return nil
}

// Run is a convenience wrapper for one-shot execution without retry support.
func Run(ctx context.Context, rs *RunState, start machine.Strategy[RunState]) error {
	return NewRunner(rs).Run(ctx, start)
}

func stageName(s machine.Strategy[RunState]) string {
	if s == nil {
		return StageDone
	}
	if n, ok := s.(Named); ok {
		return n.Name()
	}
	return "Unknown"
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
