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

// Strategy implements the retaining stage.
type Strategy struct {
	retentions []ports.RetentionService
	onFail     machine.Strategy[ritual.RunState]
}

// New builds a retaining Strategy.
func New(retentions []ports.RetentionService, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{retentions: retentions, onFail: onFail}
}

// Name returns the stage name for logging.
func (*Strategy) Name() string { return ritual.StageRetaining }

// Run applies every RetentionService. Error routes to onFail via rs.Err.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "retain"})
	for i, r := range s.retentions {
		if err := r.Apply(ctx); err != nil {
			rs.Err = fmt.Errorf("retention %d: %w", i, err)
			return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
		}
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "retain"})
	if rs.Err != nil {
		return s.onFail, nil //nolint:nilerr // rs.Err came from upstream stage; onFail routes it
	}
	return nil, nil //nolint:nilnil // terminal stage: nil next + nil err signals machine exit
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
