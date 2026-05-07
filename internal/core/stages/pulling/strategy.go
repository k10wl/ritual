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
	"errors"

	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// HeadResolver picks the ref id to pull. The composition root builds
// this as a closure over storage (List("refs/") → max timestamp).
type HeadResolver func(ctx context.Context) (domain.RefID, error)

// ErrNoHead is the sentinel a HeadResolver returns when the storage carries
// no refs yet. The pulling stage treats it as a no-op success so a later
// Commit+Push can bootstrap the first ref. Real listing failures must wrap
// a different error so they route to onFail unchanged.
var ErrNoHead = errors.New("pulling: no head ref on storage")

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
// rs.Err and routes to onFail. When the resolver signals ErrNoHead the
// stage treats the pull as a no-op success — there is nothing on storage
// yet, the local workdir is authoritative, and the chain advances so a
// later Commit+Push can bootstrap the first ref.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "pull"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	stopCtx, stopCancel := watchStop(ctx, rs.Bus)
	defer stopCancel()
	id, err := s.resolve(stopCtx)
	if errors.Is(err, ErrNoHead) {
		publish(rs.Bus, ritual.FinishInfo{Operation: "pull"})
		return s.onOK, nil
	}
	if err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	if err := s.puller.Pull(stopCtx, id); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	if err := s.applier.Apply(stopCtx, id); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	rs.ParentRefID = id
	publish(rs.Bus, ritual.FinishInfo{Operation: "pull"})
	return s.onOK, nil
}

// watchStop returns a child ctx that is cancelled the moment a
// ritual.StopRequested event arrives on the bus. Unblocks long-running
// Pull/Apply calls so the user does not have to wait for an in-flight
// blob batch to drain after pressing Stop. The returned cancel must be
// deferred so the watcher goroutine exits when Run returns even if no
// stop was requested.
func watchStop(parent context.Context, bus ports.EventBus) (context.Context, context.CancelFunc) {
	stopCtx, cancel := context.WithCancel(parent)
	if bus == nil {
		return stopCtx, cancel
	}
	sub, unsub := bus.Subscribe()
	go func() {
		defer unsub()
		for {
			select {
			case <-stopCtx.Done():
				return
			case e, ok := <-sub:
				if !ok {
					return
				}
				if _, ok := e.(ritual.StopRequested); ok {
					cancel()
					return
				}
			}
		}
	}()
	return stopCtx, cancel
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
