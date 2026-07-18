package livesync

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/pulling"
	"sync/atomic"
)

// ParentFromBus subscribes to pulling.HeadResolvedInfo and exposes the
// latest pulled head as a ParentFn the live-sync ticker can consume.
// Returns "" until the first HeadResolvedInfo arrives; tick aborts
// silently in that window (no Commit attempted without a Parent).
//
// stop drains the consumer goroutine; safe to call multiple times.
func ParentFromBus(bus ports.EventBus) (parentFn ParentFn, stop func()) {
	var p atomic.Pointer[domain.RefID]
	ch, cancel := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			if ev, ok := e.(pulling.HeadResolvedInfo); ok {
				ref := ev.RefID
				p.Store(&ref)
			}
		}
	}()
	parentFn = func() domain.RefID {
		v := p.Load()
		if v == nil {
			return ""
		}
		return *v
	}
	var stopped atomic.Bool
	stop = func() {
		if stopped.Swap(true) {
			return
		}
		cancel()
		<-done
	}
	return parentFn, stop
}
