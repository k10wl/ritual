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
type Strategy struct {
	from string
}

// New creates a Failed strategy that reports from the given origin stage.
// Use one instance per origin (e.g. failed.New(ritual.StagePreparing)).
func New(from string) *Strategy { return &Strategy{from: from} }

func (*Strategy) Name() string { return ritual.StageFailed }

func (s *Strategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	err := rs.Err
	if err == nil {
		err = errors.New("failed without recorded error")
	}
	publish(rs.Bus, ports.StateFailedInfo{State: s.from, RunID: rs.RunID, Err: err})
	return nil, err
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
