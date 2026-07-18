// Package checking runs pre-flight check functions in order. On any
// failure it records rs.Err and routes to onFail; on full success it
// routes to onOK (typically Fetching). Per-check observability lives in
// checks.Observed — this stage only owns the batch-level lifecycle event
// and short-circuit semantics.
package checking

import (
	"context"
	"ritual/internal/core/checks"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Checking stage.
type Strategy struct {
	checks []checks.Check
	onOK   machine.Strategy[ritual.RunState]
	onFail machine.Strategy[ritual.RunState]
}

// New builds a Checking Strategy.
func New(cs []checks.Check, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{checks: cs, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageChecking }

// Run executes each check; the first failure records rs.Err and routes to
// onFail. Per-check naming + lifecycle events come from the Observed
// decorator at composition time.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "check"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	for _, c := range s.checks {
		if err := ctx.Err(); err != nil {
			rs.Err = err
			return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
		}
		if err := c(ctx); err != nil {
			rs.Err = err
			return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
		}
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "check"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
