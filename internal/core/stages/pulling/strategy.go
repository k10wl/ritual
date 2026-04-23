// Package fetching runs the downstream updaters that pull remote state
// into the local tree (ritual updater + world/server sync-downloaders).
// On any failure it records rs.Err and routes to onFail.
package fetching

import (
	"context"
	"fmt"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Fetching stage.
type Strategy struct {
	updaters []ports.UpdaterService
	onOK     machine.Strategy[ritual.RunState]
	onFail   machine.Strategy[ritual.RunState]
}

// New builds a Fetching Strategy.
func New(updaters []ports.UpdaterService, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{updaters: updaters, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageFetching }

// Run executes each updater; first failure records rs.Err and routes to onFail.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "fetch"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	for i, u := range s.updaters {
		if err := ctx.Err(); err != nil {
			rs.Err = err
			return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
		}
		if err := u.Run(ctx); err != nil {
			rs.Err = fmt.Errorf("updater %d: %w", i, err)
			return s.onFail, nil
		}
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "fetch"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
