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

type Strategy struct {
	updaters []ports.UpdaterService
	onOK     machine.Strategy[ritual.RunState]
	onFail   machine.Strategy[ritual.RunState]
}

func New(updaters []ports.UpdaterService, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{updaters: updaters, onOK: onOK, onFail: onFail}
}

func (*Strategy) Name() string { return ritual.StageFetching }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "fetch"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	for i, u := range s.updaters {
		if err := ctx.Err(); err != nil {
			rs.Err = err
			return s.onFail, nil
		}
		if err := u.Run(ctx); err != nil {
			rs.Err = fmt.Errorf("updater %d: %w", i, err)
			return s.onFail, nil
		}
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "fetch"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
