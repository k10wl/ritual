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
	from    string
	onRetry machine.Strategy[ritual.RunState]
	fired   bool
}

// New creates a Failed strategy that reports from the given origin stage.
// Use one instance per origin (e.g. failed.New(ritual.StagePreparing)).
func New(from string) *Strategy { return &Strategy{from: from} }

// SetRetry wires the back-edge for retry. Called after the full chain
// is constructed to break the circular dependency.
func (s *Strategy) SetRetry(target machine.Strategy[ritual.RunState]) {
	s.onRetry = target
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageFailed }

// Run publishes the failure, optionally following a retry back-edge.
func (s *Strategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	// Retry path: already fired once, error cleared, follow back-edge
	if s.fired && rs.Err == nil && s.onRetry != nil {
		s.fired = false
		return s.onRetry, nil
	}

	err := rs.Err
	if err == nil {
		err = errors.New("failed without recorded error")
	}
	rs.FailedStage = s.from
	s.fired = true
	publish(rs.Bus, ritual.StateFailedInfo{State: s.from, RunID: rs.RunID, Err: err})
	return nil, err
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
