// Package livesync runs the 5-min save+commit+push tick out-of-band on
// the event bus. ServerReadyInfo opens syncCtx and starts a time.Ticker;
// ServerStoppingInfo / ServerStoppedInfo / ServerCrashedInfo / LockLostInfo
// cancel it. Each tick is self-backpressured via CAS so a slow Commit+Push
// never queues behind a faster interval.
//
// Phase 1 wires lifecycle only — tick() is a no-op that the test suite
// observes via the injected Hook. Phase 2 fills in
// SaveRequested → wait SaveCompleted → Commit → publish → Push.
//
// Lives parallel to subsystems/heartbeat (lease) and subsystems/lock;
// cadences and failure modes are independent by design — see
// design-log/016 §"Why this isn't heartbeat v2".
package livesync

import (
	"context"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultInterval is the production tick cadence. Hardcoded per OQ2 —
// revisit when telemetry on actual save-cost lands.
const DefaultInterval = 5 * time.Minute

// Hook is the tick body. Phase 1 injects a no-op; Phase 2 replaces it
// with the real Commit+Push sequence. Tests inject counters / barriers.
type Hook func(ctx context.Context)

// Ticker owns the periodic syncCtx and one in-flight tick goroutine.
// Subscribes to the bus for lifecycle; never reads or writes RunState
// (see OQ4 Option A — bus dispatcher owns rs.RefID propagation).
type Ticker struct {
	bus      ports.EventBus
	hook     Hook
	onStart  func()
	interval time.Duration

	mu       sync.Mutex
	cancel   context.CancelFunc // cancels syncCtx; nil when stopped
	running  bool               // ticker goroutine alive
	inFlight atomic.Bool        // CAS self-backpressure (design §Q6.3)
	wg       sync.WaitGroup     // ticker goroutine + in-flight tick
}

// Options bundles the optional dependencies for Attach. Zero values are
// always safe: nil Hook → no-op, nil OnStart → no-op, zero Interval →
// DefaultInterval. Production wiring (see New) fills in everything.
type Options struct {
	Hook     Hook
	OnStart  func() // invoked each time ServerReadyInfo opens a session
	Interval time.Duration
}

// Attach subscribes a new Ticker to bus and returns it. The consumer
// goroutine runs until the returned cancel is called. Composition root
// invokes once; cancel on program exit.
func Attach(bus ports.EventBus, opts Options) (*Ticker, func()) {
	if opts.Hook == nil {
		opts.Hook = func(context.Context) {}
	}
	if opts.OnStart == nil {
		opts.OnStart = func() {}
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	t := &Ticker{bus: bus, hook: opts.Hook, onStart: opts.OnStart, interval: opts.Interval}

	ch, cancelSub := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			t.handle(e)
		}
	}()
	cancel := func() {
		cancelSub()
		<-done
		t.stop()
		t.wg.Wait()
	}
	return t, cancel
}

func (t *Ticker) handle(e ports.Event) {
	switch e.(type) {
	case running.ServerReadyInfo:
		t.start()
	case running.ServerStoppingInfo, running.ServerStoppedInfo, running.ServerCrashedInfo:
		t.stop()
	case ritual.LockLostInfo:
		t.stop()
	}
}

func (t *Ticker) start() {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored on Ticker, released by stop()
	t.cancel = cancel
	t.running = true
	t.mu.Unlock()

	t.onStart()
	t.wg.Go(func() { t.loop(ctx) })
}

func (t *Ticker) stop() {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	t.running = false
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *Ticker) loop(ctx context.Context) {
	tk := time.NewTicker(t.interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			t.fire(ctx)
		}
	}
}

// fire enforces self-backpressure: if a previous tick's hook has not
// returned, this fire is skipped (not queued). Matches design §Q6.3.
func (t *Ticker) fire(ctx context.Context) {
	if !t.inFlight.CompareAndSwap(false, true) {
		return
	}
	t.wg.Go(func() {
		defer t.inFlight.Store(false)
		t.hook(ctx)
	})
}

// WaitInFlight blocks until any in-flight tick hook has returned, ctx
// is cancelled, or the polling exits. Used by Drain (design-log/016
// §Drain barrier) to block the post-session committing.Strategy until
// the tick goroutine has stopped mutating engine state.
//
// Polling at ~5ms is fine: Drain only fires on shutdown (rare) and the
// in-flight window is bounded by the syncCtx cancel that runs just
// before this call.
func (t *Ticker) WaitInFlight(ctx context.Context) error {
	for {
		if !t.inFlight.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}
