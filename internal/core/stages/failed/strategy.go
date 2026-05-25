// Package failed is the terminal error stage. Publishes StateFailedInfo
// and returns (nil, rs.Err) so ritual.Run propagates the error.
package failed

import (
	"context"
	"errors"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy emits StateFailedInfo with a caller-supplied origin stage
// name and returns the run's recorded error. Multiple instances may be
// constructed, one per origin stage, so the event accurately names the
// source of the failure.
//
// Failed is purely terminal: there is no retry back-edge (see
// design-log/017). Users dismiss failures (lifecycle.Dismissed) then
// re-engage with a fresh StartRequested.
type Strategy struct {
	from string
}

// New creates a Failed strategy that reports from the given origin stage.
// Use one instance per origin (e.g. failed.New(ritual.StagePreparing)).
func New(from string) *Strategy { return &Strategy{from: from} }

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageFailed }

// Run publishes the failure and terminates the chain.
func (s *Strategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	err := rs.Err
	if err == nil {
		err = errors.New("failed without recorded error")
	}
	rs.FailedStage = s.from
	publish(rs.Bus, ritual.StateFailedInfo{State: s.from, RunID: rs.RunID, Err: err})
	return nil, err
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
