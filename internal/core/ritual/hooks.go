package ritual

import (
	"context"

	"ritual/internal/core/ports"
)

// WithLostLock derives a cancellable context that fires when the
// heartbeat supervisor publishes LockLostInfo for rs.RunID. It wraps the
// parent in context.WithoutCancel first, so routine user/shutdown
// cancellation does not affect locked-span stages; only a true
// lost-lock condition does. Caller must defer the returned cleanup.
func WithLostLock(parent context.Context, rs *RunState) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	if rs == nil || rs.Bus == nil {
		return ctx, cancel
	}
	ch, unsubscribe := rs.Bus.Subscribe()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				if lost, ok := e.(ports.LockLostInfo); ok && lost.RunID == rs.RunID {
					cancel()
					return
				}
			}
		}
	}()
	cleanup := func() {
		close(stop)
		unsubscribe()
		cancel()
	}
	return ctx, cleanup
}
