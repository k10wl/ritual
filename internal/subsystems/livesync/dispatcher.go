package livesync

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/running"
	"sync"
)

// Dispatcher writes the most recent LiveDraftCommitted RefID into a
// caller-owned slot (production: rs.RefID) and tracks the last id it
// applied. Sync waits until a specific id has landed — used by the
// Drain pre-stage so the post-session committing.Strategy sees the
// latest tick draft as rs.RefID before its resolver runs (OQ4 Option A).
//
// Single-writer (one bus subscription, one goroutine). Apply runs under
// the Dispatcher's lock — keep it cheap (a pointer write).
//
// SetTarget rebinds the apply slot per session — lifecycle.SessionHook
// calls it with the new rs at each start. Reset clears lastID between
// sessions so Sync("") returns immediately when zero ticks ran.
type Dispatcher struct {
	mu     sync.Mutex
	apply  func(domain.RefID)
	lastID domain.RefID
	bump   chan struct{}

	done chan struct{}
	stop func()
}

// NewDispatcher subscribes to bus and forwards LiveDraftCommitted to
// apply. Returns the dispatcher plus an idempotent cancel that drains
// the consumer goroutine.
func NewDispatcher(bus ports.EventBus, apply func(domain.RefID)) (*Dispatcher, func()) {
	d := &Dispatcher{
		apply: apply,
		bump:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	ch, cancel := bus.Subscribe()
	go func() {
		defer close(d.done)
		for e := range ch {
			switch ev := e.(type) {
			case LiveDraftCommitted:
				d.applyAndBump(ev.RefID)
			case running.ServerReadyInfo:
				d.Reset()
			}
		}
		// Bus closed — wake any pending Sync so it observes ctx.Err()
		// rather than blocking forever.
		d.mu.Lock()
		close(d.bump)
		d.bump = nil
		d.mu.Unlock()
	}()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			<-d.done
		})
	}
	d.stop = stop
	return d, stop
}

func (d *Dispatcher) applyAndBump(id domain.RefID) {
	d.mu.Lock()
	if d.apply != nil {
		d.apply(id)
	}
	d.lastID = id
	if d.bump != nil {
		close(d.bump)
		d.bump = make(chan struct{})
	}
	d.mu.Unlock()
}

// LastApplied returns the id of the most recent LiveDraftCommitted this
// dispatcher has processed. "" means none yet (or shutdown without any
// tick draft).
func (d *Dispatcher) LastApplied() domain.RefID {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastID
}

// SetTarget rebinds the apply slot. Called once per session start from
// lifecycle.SessionHook with the fresh rs's RefID-setter. nil disables
// writes (idle window between sessions).
func (d *Dispatcher) SetTarget(apply func(domain.RefID)) {
	d.mu.Lock()
	d.apply = apply
	d.mu.Unlock()
}

// Reset clears lastID and refreshes bump. Called from the dispatcher's
// own subscription on ServerReadyInfo so the new session's first
// LiveDraftCommitted is observed cleanly. Idempotent.
func (d *Dispatcher) Reset() {
	d.mu.Lock()
	d.lastID = ""
	if d.bump != nil {
		close(d.bump)
		d.bump = make(chan struct{})
	}
	d.mu.Unlock()
}

// Sync blocks until LastApplied == want, ctx is cancelled, or the bus
// closes. want == "" is a no-op (no tick draft to wait on). Returns
// ctx.Err() on cancellation; nil otherwise.
//
// Invariant: callers must compute want from Engine.LastRefID() AFTER
// the ticker's syncCtx is cancelled, so the engine is no longer
// publishing new events. Otherwise Sync may race the next tick.
func (d *Dispatcher) Sync(ctx context.Context, want domain.RefID) error {
	if want == "" {
		return nil
	}
	for {
		d.mu.Lock()
		cur := d.lastID
		bump := d.bump
		d.mu.Unlock()
		if cur == want {
			return nil
		}
		if bump == nil {
			// Bus closed; dispatcher will never advance further.
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-bump:
		}
	}
}
