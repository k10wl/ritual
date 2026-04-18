package app

// Outcome is the app-level terminal status exposed on the bus.
type Outcome int

// App status values. Terminal states are Done and Failed.
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
