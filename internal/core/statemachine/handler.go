package statemachine

import "context"

// StateName is a typed string. Free Stringer, free JSON/slog.
type StateName string

const (
	Preparing StateName = "Preparing"
	Locking   StateName = "Locking"
	Running   StateName = "Running"
	Exiting   StateName = "Exiting"
	Unlocking StateName = "Unlocking"
	Failed    StateName = "Failed"
)

// Handler is the state contract. Returns next state; nil = terminal success.
// Each state struct carries its own deps (constructor injection).
//
// No Enter/Exit hooks: brackets that span phases (e.g. lock acquire/release)
// are modeled as state pairs (Locking ↔ Unlocking/Exiting). Brackets within
// a phase use defer inside Handle.
type Handler interface {
	Name() StateName
	Handle(ctx context.Context) (Handler, error)
}
