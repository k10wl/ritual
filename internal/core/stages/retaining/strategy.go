// Package retaining prunes backups per RetentionService rules. Runs
// without WithoutCancel — lock is already released by the preceding
// Unlocking stage. Terminates the run: if rs.Err is set (from any
// upstream stage), routes to onFail; otherwise returns nil.
package retaining

import (
	"context"
	"fmt"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	retentions []ports.RetentionService
	onFail     machine.Strategy[ritual.RunState]
}

func New(retentions []ports.RetentionService, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{retentions: retentions, onFail: onFail}
}

func (*Strategy) Name() string { return ritual.StageRetaining }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "retain"})
	for i, r := range s.retentions {
		if err := r.Apply(ctx); err != nil {
			rs.Err = fmt.Errorf("retention %d: %w", i, err)
			return s.onFail, nil
		}
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "retain"})
	if rs.Err != nil {
		return s.onFail, nil
	}
	return nil, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
