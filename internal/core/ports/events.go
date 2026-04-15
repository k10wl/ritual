package ports

// Event is the sealed interface for all event types
type Event interface {
	sealed()
}

// StartEvent signals the beginning of an operation
type StartEvent struct {
	Operation string
}

// UpdateEvent provides progress or status information during an operation
type UpdateEvent struct {
	Operation string
	Message   string
	Data      map[string]any
}

// FinishEvent signals the successful completion of an operation
type FinishEvent struct {
	Operation string
}

// ErrorEvent signals an error during an operation
type ErrorEvent struct {
	Operation string
	Err       error
}

// PromptEvent requests user input
type PromptEvent struct {
	ID           string
	Prompt       string
	DefaultValue string
	ResponseChan chan<- any
}

// RetryAttemptInfo is emitted on each transient failure that triggers a retry.
// Operation is adapter-qualified (e.g. "r2.Get"). Key is the object key or
// prefix when available, empty otherwise. Attempt is 1-indexed.
type RetryAttemptInfo struct {
	Operation string
	Key       string
	Attempt   uint
	Err       error
}

func (StartEvent) sealed()       {}
func (UpdateEvent) sealed()      {}
func (FinishEvent) sealed()      {}
func (ErrorEvent) sealed()       {}
func (PromptEvent) sealed()      {}
func (RetryAttemptInfo) sealed() {}

// SendEvent safely sends an event to the channel if it's not nil
func SendEvent(events chan<- Event, evt Event) {
	if events != nil {
		events <- evt
	}
}
