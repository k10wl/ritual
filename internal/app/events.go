// Package app is the app-level orchestrator that wires bus commands to the Ritual state machine.
package app

import "fmt"

// StartRequested commands the Ritual to begin the full pipeline.
type StartRequested struct{}

func (StartRequested) String() string { return "start requested" }

// StopRequested commands the Ritual to cancel the running pipeline.
type StopRequested struct{}

func (StopRequested) String() string { return "stop requested" }

// RetryRequested commands the Ritual to re-enter at the failed stage.
type RetryRequested struct{}

func (RetryRequested) String() string { return "retry requested" }

// StatusChanged is published when the Ritual transitions between outcomes.
type StatusChanged struct {
	Status Outcome
	Err    error
}

func (s StatusChanged) String() string {
	if s.Err != nil {
		return fmt.Sprintf("status: %s err: %v", s.Status, s.Err)
	}
	return fmt.Sprintf("status: %s", s.Status)
}
