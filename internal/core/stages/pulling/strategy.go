// Package pulling resolves the remote HEAD ref, pulls it with every
// referenced blob into local storage, then applies it to the workdir.
// On any failure it records rs.Err and routes to onFail. Per-verb
// observability is delegated to the storage-decorator stack (counter
// / compressing / observed) and the progress ticker — this stage only
// owns batch-level lifecycle and short-circuit semantics, mirroring
// checking.Strategy.
package pulling

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// HeadResolver picks the ref id to pull. The composition root builds
// this as a closure over remote storage (List("refs/") → max timestamp).
type HeadResolver func(ctx context.Context) (domain.RefID, error)

// Strategy implements the Pulling stage.
type Strategy struct {
	puller  ports.Puller
	applier ports.Applier
	resolve HeadResolver
	onOK    machine.Strategy[ritual.RunState]
	onFail  machine.Strategy[ritual.RunState]
}

// New builds a Pulling Strategy.
func New(puller ports.Puller, applier ports.Applier, resolve HeadResolver, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{puller: puller, applier: applier, resolve: resolve, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StagePulling }

// Run resolves HEAD, pulls it, then applies it. First failure records
// rs.Err and routes to onFail. When the resolver returns an empty id the
// stage treats the pull as a no-op success — there is nothing on remote
// yet, the local workdir is authoritative, and the chain advances so a
// later Commit+Push can bootstrap the remote.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "pull"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	id, err := s.resolve(ctx)
	if err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	if id == "" {
		publish(rs.Bus, ritual.FinishInfo{Operation: "pull"})
		return s.onOK, nil
	}
	if err := s.puller.Pull(ctx, id); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	if err := s.applier.Apply(ctx, id); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "pull"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
