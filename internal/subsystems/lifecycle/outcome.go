package lifecycle

// Outcome is the run-level terminal status exposed on the bus. Distinct
// from the inner stage FSM; this is the *outer* state that GUI/CLI status
// pills read.
type Outcome int

// Outcome values. Terminal states are Done, Failed, and Dismissed; from
// Failed the only forward transitions are Running (fresh Start) or
// Dismissed (user-acknowledged failure, returns to Idle).
const (
	Idle Outcome = iota
	Running
	Failed
	Done
	Dismissed
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
	case Dismissed:
		return "dismissed"
	default:
		return "unknown"
	}
}
