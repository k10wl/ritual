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
	onStop machine.Strategy[ritual.RunState]
}

// New builds a Pushing Strategy.
func New(pusher ports.Pusher, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{pusher: pusher, onOK: onOK, onFail: onFail}
}

// OnStop wires the edge taken when ritual.StopRequested arrives mid-push.
// Production points it at unlocking so the held lock is released even
// though the user-cancelled run never finishes the upload. Unset by
// default — a stop arriving without OnStop wired falls back to onFail
// for backwards compatibility (audit open item #3).
func (s *Strategy) OnStop(target machine.Strategy[ritual.RunState]) {
	s.onStop = target
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StagePushing }

// Run pushes rs.RefID. Empty rs.RefID is a no-op success — committing
// produced no new ref so there is nothing to upload. On entry-time ctx
// cancellation the stage short-circuits to onFail. ritual.StopRequested
// arriving mid-push cancels a local stop-ctx so Push returns fast and
// the stage routes to onStop (or onFail when onStop is unset).
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
	stopCtx, stopCancel := watchStop(ctx, rs.Bus)
	defer stopCancel()
	if err := s.pusher.Push(stopCtx, rs.RefID); err != nil {
		rs.Err = err
		if stopCtx.Err() != nil && s.onStop != nil {
			return s.onStop, nil
		}
		return s.onFail, nil
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "push"})
	return s.onOK, nil
}

// watchStop returns a child ctx that is cancelled the moment a
// ritual.StopRequested event arrives on the bus. Mirrors the helper in
// pulling.Strategy — local copy keeps the dependency graph thin.
func watchStop(parent context.Context, bus ports.EventBus) (context.Context, context.CancelFunc) {
	stopCtx, cancel := context.WithCancel(parent)
	if bus == nil {
		return stopCtx, cancel
	}
	sub, unsub := bus.Subscribe()
	go func() {
		defer unsub()
		for {
			select {
			case <-stopCtx.Done():
				return
			case e, ok := <-sub:
				if !ok {
					return
				}
				if _, ok := e.(ritual.StopRequested); ok {
					cancel()
					return
				}
			}
		}
	}()
	return stopCtx, cancel
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
