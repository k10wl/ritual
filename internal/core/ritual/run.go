package ritual

import (
	"context"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Run drives the state machine while publishing StateChangedInfo on every
// transition, including the final transition to "Done". Mirrors the
// behavior of the pre-v2 statemachine.Machine without coupling the
// generic driver to ports.
func Run(ctx context.Context, rs *RunState, start machine.Strategy[RunState]) error {
	cur := start
	curName := stageName(cur)
	for cur != nil {
		next, err := cur.Run(ctx, rs)
		if err != nil {
			return err
		}
		nextName := stageName(next)
		publish(rs.Bus, ports.StateChangedInfo{From: curName, To: nextName, RunID: rs.RunID})
		cur = next
		curName = nextName
	}
	return nil
}

func stageName(s machine.Strategy[RunState]) string {
	if s == nil {
		return StageDone
	}
	if n, ok := s.(Named); ok {
		return n.Name()
	}
	return "Unknown"
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
