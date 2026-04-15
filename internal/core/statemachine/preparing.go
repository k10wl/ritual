package statemachine

import (
	"context"
	"fmt"

	"ritual/internal/core/ports"
)

// PreparingState runs conditions and updaters. Any failure routes to Failed.
type PreparingState struct {
	conditions []ports.ConditionService
	updaters   []ports.UpdaterService
	bus        ports.EventBus
	factory    StateFactory
}

func NewPreparingState(
	c []ports.ConditionService,
	u []ports.UpdaterService,
	bus ports.EventBus,
	f StateFactory,
) *PreparingState {
	return &PreparingState{conditions: c, updaters: u, bus: bus, factory: f}
}

func (*PreparingState) Name() StateName { return Preparing }

func (s *PreparingState) Handle(ctx context.Context) (Handler, error) {
	publish(s.bus, ports.StartInfo{Operation: "prepare"})
	if err := ctx.Err(); err != nil {
		return s.factory.Failed(Preparing, err), nil
	}
	for i, c := range s.conditions {
		if err := ctx.Err(); err != nil {
			return s.factory.Failed(Preparing, err), nil
		}
		if err := c.Check(ctx); err != nil {
			return s.factory.Failed(Preparing, fmt.Errorf("condition %d: %w", i, err)), nil
		}
	}
	for i, u := range s.updaters {
		if err := ctx.Err(); err != nil {
			return s.factory.Failed(Preparing, err), nil
		}
		if err := u.Run(ctx); err != nil {
			return s.factory.Failed(Preparing, fmt.Errorf("updater %d: %w", i, err)), nil
		}
	}
	publish(s.bus, ports.FinishInfo{Operation: "prepare"})
	return s.factory.Locking(), nil
}

func (f *factory) Preparing() Handler {
	return NewPreparingState(f.d.Conditions, f.d.Updaters, f.d.Bus, f)
}
