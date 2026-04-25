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

// RetryRequested commands the lifecycle to re-enter at the failed stage.
// Only valid when the current Outcome is Failed.
type RetryRequested struct{}

func (RetryRequested) String() string { return "retry requested" }
