package ritual

import (
	"context"
	"fmt"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Runner drives the state machine and tracks the current strategy
// so RunCurrent can re-enter after a failure.
type Runner struct {
	runState *RunState
	current  machine.Strategy[RunState]
}

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
		return fmt.Errorf("no current strategy to resume")
	}
	return r.drive(ctx)
}

// drive runs the state machine loop.
//
// On a successful transition cur → next, r.current advances to next.
// When next.Run() errors on the following iteration, that means the
// failure originated in the hand-off from cur. To allow RunCurrent to
// re-enter at the stage that produced the failing next (i.e. cur), we
// restore r.current = prev on error, where prev was the strategy that
// ran successfully before the one that errored.
func (r *Runner) drive(ctx context.Context) error {
	rs := r.runState
	var prev machine.Strategy[RunState]
	for r.current != nil {
		cur := r.current
		curName := stageName(cur)
		next, err := cur.Run(ctx, rs)
		if err != nil {
			// cur errored. Re-enter at prev (the stage that transitioned
			// into cur), so the caller can retry the work that led here.
			// If there is no prev (cur was the start), re-enter at cur itself.
			if prev != nil {
				r.current = prev
			}
			return err
		}
		nextName := stageName(next)
		publish(rs.Bus, ports.StateChangedInfo{From: curName, To: nextName, RunID: rs.RunID})
		prev = cur
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
