package checks

import (
	"context"
	"fmt"

	"ritual/internal/core/ports"
)

// CheckStarted is published before a wrapped check runs.
type CheckStarted struct{ Name string }

func (e CheckStarted) String() string { return "check started: " + e.Name }

// CheckPassed is published after a wrapped check returns nil.
type CheckPassed struct{ Name string }

func (e CheckPassed) String() string { return "check passed: " + e.Name }

// CheckFailed is published after a wrapped check returns a non-nil error.
type CheckFailed struct {
	Name string
	Err  error
}

func (e CheckFailed) String() string {
	if e.Err == nil {
		return "check failed: " + e.Name
	}
	return "check failed: " + e.Name + ": " + e.Err.Error()
}

// Observed wraps a Check so that its lifecycle is published on the bus and
// any returned error is prefixed with the check's name. The wrapped check
// stays bus-free and unit-testable; observability is a decorator concern.
func Observed(name string, c Check, bus ports.EventBus) Check {
	return func(ctx context.Context) error {
		publish(bus, CheckStarted{Name: name})
		if err := c(ctx); err != nil {
			publish(bus, CheckFailed{Name: name, Err: err})
			return fmt.Errorf("check %s: %w", name, err)
		}
		publish(bus, CheckPassed{Name: name})
		return nil
	}
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
