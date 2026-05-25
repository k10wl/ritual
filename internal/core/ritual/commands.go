package ritual

// Bus commands published from outside the FSM (GUI, CLI, scheduler) to
// drive the run lifecycle. The lifecycle subsystem subscribes and acts;
// stages never see these directly.

// StartRequested commands the lifecycle to begin a fresh run from the
// pipeline's entry strategy.
type StartRequested struct{}

func (StartRequested) String() string { return "start requested" }

// StopRequested commands the lifecycle to cancel the running pipeline.
// Userstop is graceful: a cancellation propagating through stages resolves
// to Done, not Failed.
type StopRequested struct{}

func (StopRequested) String() string { return "stop requested" }

// DismissRequested acknowledges a Failed outcome and returns the
// lifecycle to Idle. Only valid when current Outcome is Failed.
// Replaces the prior retry-from-failed semantic (see design-log/017): the
// user explicitly clears the failure, then re-engages with a fresh Start.
type DismissRequested struct{}

func (DismissRequested) String() string { return "dismiss requested" }
