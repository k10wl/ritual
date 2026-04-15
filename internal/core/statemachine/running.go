package statemachine

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// RunningState executes the server. Always routes to Exiting so the lock is
// released regardless of server success/failure. Manifests are snapshotted
// before server start so ExitingState can decide on backups without a
// second fetch.
type RunningState struct {
	server          *domain.ServerRuntime
	runner          ports.ServerRunner
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	factory         StateFactory
}

func NewRunningState(
	server *domain.ServerRuntime,
	runner ports.ServerRunner,
	local, remote ports.ManifestStore,
	bus ports.EventBus,
	f StateFactory,
) *RunningState {
	return &RunningState{
		server:          server,
		runner:          runner,
		localManifests:  local,
		remoteManifests: remote,
		bus:             bus,
		factory:         f,
	}
}

func (*RunningState) Name() StateName { return Running }

func (s *RunningState) Handle(ctx context.Context) (Handler, error) {
	publish(s.bus, ports.StartInfo{Operation: "server"})
	if next := ctxFailed(ctx, s.factory, Running); next != nil {
		return next, nil
	}

	localBefore, _ := s.localManifests.Get(ctx)
	remoteBefore, _ := s.remoteManifests.Get(ctx)
	lockID := ""
	if localBefore != nil {
		lockID = localBefore.LockedBy
	}

	if err := s.runner.Run(ctx, s.server); err != nil {
		publish(s.bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
	}
	publish(s.bus, ports.FinishInfo{Operation: "server"})
	return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
}

func (f *factory) Running() Handler {
	return NewRunningState(f.d.Server, f.d.ServerRunner, f.d.LocalManifests, f.d.RemoteManifests, f.d.Bus, f)
}
