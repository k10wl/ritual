package app

type Outcome int

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
