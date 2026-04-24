// Package pushing uploads rs.RefID and every referenced blob to remote.
// On any failure it records rs.Err and routes to onFail, skipping remote
// retention so a half-uploaded ref is not swept. Mirrors pulling.Strategy.
package pushing

import (
	"context"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Pushing stage.
type Strategy struct {
	pusher ports.Pusher
	onOK   machine.Strategy[ritual.RunState]
	onFail machine.Strategy[ritual.RunState]
}

// New builds a Pushing Strategy.
func New(pusher ports.Pusher, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{pusher: pusher, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StagePushing }

// Run pushes rs.RefID. Empty rs.RefID is a no-op success — committing
// produced no new ref so there is nothing to upload. On entry-time ctx
// cancellation the stage short-circuits to onFail.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "push"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	if rs.RefID == "" {
		publish(rs.Bus, ritual.FinishInfo{Operation: "push"})
		return s.onOK, nil
	}
	if err := s.pusher.Push(ctx, rs.RefID); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "push"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
