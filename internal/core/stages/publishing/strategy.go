// Package publishing runs the upload updaters that publish local state
// back to remote after the server session. Runs under
// context.WithoutCancel so shutdown cannot abort uploads. On any error
// it records rs.Err and forwards to onNext so the lock-release chain
// always continues.
package publishing

import (
	"context"
	"fmt"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	updaters []ports.UpdaterService
	onNext   machine.Strategy[ritual.RunState]
}

func New(updaters []ports.UpdaterService, onNext machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{updaters: updaters, onNext: onNext}
}

func (*Strategy) Name() string { return ritual.StagePublishing }

func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "publish"})

	if rs.LockID == "" {
		publish(rs.Bus, ports.FinishInfo{Operation: "publish"})
		return s.onNext, nil
	}

	ctx, done := ritual.WithLostLock(parentCtx, rs)
	defer done()
	for i, u := range s.updaters {
		if err := u.Run(ctx); err != nil {
			rs.Err = fmt.Errorf("publish updater %d: %w", i, err)
			return s.onNext, nil
		}
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "publish"})
	return s.onNext, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
