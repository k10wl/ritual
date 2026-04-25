package lifecycle

// Outcome is the run-level terminal status exposed on the bus. Distinct
// from the inner stage FSM; this is the *outer* state that GUI/CLI status
// pills read.
type Outcome int

// Outcome values. Terminal states are Done and Failed; from Failed the
// only forward transition is Running (via RetryRequested).
const (
	Idle Outcome = iota
	Running
	Failed
	Done
)

func (o Outcome) String() string {
	switch o {
	case Idle:
		return "idle"
	case Running:
		return "running"
	case Failed:
		return "failed"
	case Done:
		return "done"
	default:
		return "unknown"
	}
}
