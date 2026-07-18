package ports

import "context"

// Prompter requests a single line of user input.
// Implementations: stdin (CLI), modal dialog (GUI), scripted (tests).
//
// Synchronous RPC, separate from the fan-out EventBus —
// fan-out semantics would race the response across subscribers.
type Prompter interface {
	Prompt(ctx context.Context, id, prompt, defaultValue string) (string, error)
}
