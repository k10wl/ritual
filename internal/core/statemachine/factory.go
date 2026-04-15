package statemachine

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// StateFactory builds states with their deps. Transition payloads flow as args.
type StateFactory interface {
	Preparing() Handler
	Locking() Handler
	Running() Handler
	Exiting(lockID string, localBefore, remoteBefore *domain.Manifest) Handler
	Unlocking(lockID string, cause error) Handler
	Failed(from StateName, err error) Handler
	RunID() string
}

// Deps is the full dependency set for a run. Constructed once in main.go.
type Deps struct {
	Bus             ports.EventBus
	Prompter        ports.Prompter
	RunID           string
	Server          *domain.ServerRuntime
	LocalManifests  ports.ManifestStore
	RemoteManifests ports.ManifestStore
	LocalStore      ports.StorageRepository
	RemoteStore     ports.StorageRepository
	ServerRunner    ports.ServerRunner
	Conditions      []ports.ConditionService
	Updaters        []ports.UpdaterService
	ExitUpdaters    []ports.UpdaterService
	Retentions      []ports.RetentionService
}

type factory struct{ d Deps }

// NewFactory returns a StateFactory that wires states from the given Deps.
func NewFactory(d Deps) StateFactory { return &factory{d: d} }

func (f *factory) RunID() string { return f.d.RunID }

// publish is the nil-safe helper shared by every state.
func publish(bus ports.EventBus, evt ports.Event) {
	if bus != nil {
		bus.Publish(evt)
	}
}

// ctxFailed checks ctx cancellation and returns a Failed handler if cancelled,
// nil otherwise. Every state EXCEPT ExitingState uses this at Handle() entry
// and inside any loop to honor GUI-close / OS-shutdown cancellation promptly.
// ExitingState deliberately runs with context.WithoutCancel and must not use
// this helper.
func ctxFailed(ctx context.Context, f StateFactory, from StateName) Handler {
	if err := ctx.Err(); err != nil {
		return f.Failed(from, err)
	}
	return nil
}
