package statemachine

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// Machine drives a Handler chain to completion. Emits StateChangedInfo
// on every transition (including the final transition to "Done").
type Machine struct {
	current Handler
	bus     ports.EventBus
	runID   string
}

func NewMachine(initial Handler, bus ports.EventBus, runID string) *Machine {
	return &Machine{current: initial, bus: bus, runID: runID}
}

// Run advances Handlers until one returns nil (success) or error.
func (m *Machine) Run(ctx context.Context) error {
	if m.current == nil {
		return errors.New("nil initial state")
	}
	for {
		next, err := m.current.Handle(ctx)
		if err != nil {
			return err
		}
		to := StateName("Done")
		if next != nil {
			to = next.Name()
		}
		m.publish(ports.StateChangedInfo{
			From:  string(m.current.Name()),
			To:    string(to),
			RunID: m.runID,
		})
		if next == nil {
			return nil
		}
		m.current = next
	}
}

func (m *Machine) publish(evt ports.Event) {
	if m.bus != nil {
		m.bus.Publish(evt)
	}
}
