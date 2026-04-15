package statemachine

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// FailedState is the terminal error state. Publishes StateFailedInfo and
// returns (nil, err) so Machine.Run propagates the error.
type FailedState struct {
	bus   ports.EventBus
	runID string
	from  StateName
	err   error
}

func NewFailedState(bus ports.EventBus, runID string, from StateName, err error) *FailedState {
	if err == nil {
		err = errors.New("failed without recorded error")
	}
	return &FailedState{bus: bus, runID: runID, from: from, err: err}
}

func (*FailedState) Name() StateName { return Failed }

func (s *FailedState) Handle(_ context.Context) (Handler, error) {
	publish(s.bus, ports.StateFailedInfo{State: string(s.from), RunID: s.runID, Err: s.err})
	return nil, s.err
}

func (f *factory) Failed(from StateName, err error) Handler {
	return NewFailedState(f.d.Bus, f.d.RunID, from, err)
}
