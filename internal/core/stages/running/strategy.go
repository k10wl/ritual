package running

import (
	"context"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	cmd    ports.CmdBuilder
	onNext machine.Strategy[ritual.RunState]
	onFail machine.Strategy[ritual.RunState]
}

func New(
	cmd ports.CmdBuilder,
	onNext, onFail machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{cmd: cmd, onNext: onNext, onFail: onFail}
}

func (*Strategy) Name() string { return ritual.StageRunning }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "server"})
	cmd, err := s.cmd.Build(ctx)
	if err != nil {
		rs.Err = err
		publish(rs.Bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.onFail, nil
	}
	if err := cmd.Run(); err != nil {
		rs.Err = err
		publish(rs.Bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.onFail, nil
	}
	publish(rs.Bus, ports.FinishInfo{Operation: "server"})
	return s.onNext, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
