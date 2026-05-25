package livesync

import (
	"context"
	"time"
)

// DefaultDrainTimeout caps Drain's wait at 10s per OQ5
// (escalate-and-abandon). State machine logs the typed error and
// proceeds to Committing anyway; sweepSupersededSiblings on the next
// session cleans any orphaned in-flight ref.
const DefaultDrainTimeout = 10 * time.Second

// Drainable is the contract the drain.Strategy pre-stage consumes.
// Implementations block until any in-flight tick has returned AND the
// dispatcher has applied every LiveDraftCommitted the tick published.
// On timeout, returns ctx.Err — caller (state machine) decides whether
// to proceed (typical) or abort.
type Drainable interface {
	Drain(ctx context.Context) error
}

// Drainer wires the Ticker's in-flight wait to the Dispatcher's Sync
// barrier. Created once at composition root; reused per session.
type Drainer struct {
	ticker     *Ticker
	engine     *Engine
	dispatcher *Dispatcher
	timeout    time.Duration
}

// NewDrainer returns a Drainable that, given a session ctx, waits up to
// timeout for the tick + dispatcher pair to settle. timeout == 0 falls
// back to DefaultDrainTimeout.
func NewDrainer(ticker *Ticker, engine *Engine, dispatcher *Dispatcher, timeout time.Duration) *Drainer {
	if timeout <= 0 {
		timeout = DefaultDrainTimeout
	}
	return &Drainer{ticker: ticker, engine: engine, dispatcher: dispatcher, timeout: timeout}
}

// Drain runs the two-step barrier under a 10s ceiling. Returns ctx.Err
// on timeout or parent cancellation. Empty engine.LastRefID() (no tick
// committed) short-circuits the dispatcher wait — Drain returns once
// the in-flight tick (if any) finishes.
func (d *Drainer) Drain(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	if err := d.ticker.WaitInFlight(deadline); err != nil {
		return err
	}
	// engine.LastRefID is stable once WaitInFlight returns: the tick
	// goroutine is no longer running, and the dispatcher (separate
	// goroutine) is draining its bus channel.
	return d.dispatcher.Sync(deadline, d.engine.LastRefID())
}
