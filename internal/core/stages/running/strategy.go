// Package running executes the Minecraft server and always routes to
// onNext (typically Publishing) so the locked-span cleanup chain
// completes regardless of server success/failure. Pre-run manifest
// snapshots are set on rs by Acquiring; this stage does not re-fetch.
package running

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	server *domain.ServerRuntime
	runner ports.ServerRunner
	onNext machine.Strategy[ritual.RunState]
}

func New(
	server *domain.ServerRuntime,
	runner ports.ServerRunner,
	onNext machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{server: server, runner: runner, onNext: onNext}
}

func (*Strategy) Name() string { return ritual.StageRunning }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "server"})
	if err := s.runner.Run(ctx, s.server); err != nil {
		publish(rs.Bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.onNext, nil
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "server"})
	return s.onNext, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
