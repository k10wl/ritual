// Package checking runs pre-flight condition gates (disk, RAM, Java
// version, manifest lock). On any failure it records rs.Err and routes
// to onFail. On success routes to onOK (typically Fetching).
package checking

import (
	"context"
	"fmt"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Checking stage.
type Strategy struct {
	conditions []ports.ConditionService
	onOK       machine.Strategy[ritual.RunState]
	onFail     machine.Strategy[ritual.RunState]
}

// New builds a Checking Strategy.
func New(conditions []ports.ConditionService, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{conditions: conditions, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageChecking }

// Run executes each condition; first failure records rs.Err and routes to onFail.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "check"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	for i, c := range s.conditions {
		if err := ctx.Err(); err != nil {
			rs.Err = err
			return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
		}
		if err := c.Check(ctx); err != nil {
			rs.Err = fmt.Errorf("condition %d: %w", i, err)
			return s.onFail, nil
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
