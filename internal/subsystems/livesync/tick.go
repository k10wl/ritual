package livesync

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/committing"
	"ritual/internal/core/stages/running"
	"sync"
	"time"
)

// DefaultSaveTimeout bounds the SaveRequested → SaveCompleted handshake.
// If the server's `save-all flush` does not echo "Saved the game" within
// this window, the tick is abandoned and the bus sees a livesync.save
// ErrorInfo. Mirrors the 30s used by running.Strategy's blocking wait.
const DefaultSaveTimeout = 30 * time.Second

// ParentFn returns the pulled-head RefID for the current session. The
// composition root supplies a closure over rs.ParentRefID (settled by
// the Pulling stage). Returning "" means "no head pulled yet" — tick
// aborts silently so the very-early ServerReadyInfo window can't push a
// parentless commit.
type ParentFn func() domain.RefID

// Engine carries the per-session tick state across fire() invocations.
// Tick goroutine and reset()/LastRefID() callers (bus consumer +
// Drainer) all touch parent / lastRefID, so a mutex guards them.
// Critical sections are tiny — single field load/store — so contention
// is negligible even at full tick cadence.
type Engine struct {
	bus         ports.EventBus
	committer   ports.Committer
	pusher      ports.Pusher
	targets     []string
	parentFn    ParentFn
	saveTimeout time.Duration

	mu        sync.Mutex
	parent    domain.RefID
	lastRefID domain.RefID
}

// New wires a production live-sync ticker. saveTimeout == 0 falls back
// to DefaultSaveTimeout; interval == 0 to DefaultInterval. Returns the
// ticker plus a cancel that drains the consumer + in-flight tick.
func New(
	bus ports.EventBus,
	committer ports.Committer,
	pusher ports.Pusher,
	targets []string,
	parentFn ParentFn,
	interval time.Duration,
	saveTimeout time.Duration,
) (*Ticker, *Engine, func()) {
	if saveTimeout <= 0 {
		saveTimeout = DefaultSaveTimeout
	}
	eng := &Engine{
		bus:         bus,
		committer:   committer,
		pusher:      pusher,
		targets:     targets,
		parentFn:    parentFn,
		saveTimeout: saveTimeout,
	}
	t, cancel := Attach(bus, Options{
		Hook:     eng.tick,
		OnStart:  eng.reset,
		Interval: interval,
	})
	return t, eng, cancel
}

// LastRefID exposes the most recently committed draft for this session.
// Returned value is "" until the first tick's Commit succeeds. Phase 3
// Drainer reads this when Drain flushes to compute the value the
// dispatcher must catch up to.
func (e *Engine) LastRefID() domain.RefID {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastRefID
}

func (e *Engine) reset() {
	e.mu.Lock()
	e.parent = ""
	e.lastRefID = ""
	e.mu.Unlock()
}

func (e *Engine) tick(ctx context.Context) {
	e.mu.Lock()
	parent := e.parent
	e.mu.Unlock()
	if parent == "" {
		parent = e.parentFn()
		if parent == "" {
			return
		}
		e.mu.Lock()
		e.parent = parent
		e.mu.Unlock()
	}

	if !e.requestSave(ctx) {
		return
	}

	e.mu.Lock()
	amend := e.lastRefID
	e.mu.Unlock()
	opts := ports.CommitOpts{
		Amend:   amend,
		Parent:  parent,
		Targets: e.targets,
	}
	id, err := e.committer.Commit(ctx, opts)
	if err != nil {
		e.bus.Publish(ritual.ErrorInfo{Operation: "livesync.commit", Err: err})
		return
	}
	// Update lastRefID immediately so the next tick's Amend sweeps this
	// draft even if Push fails. Honest local-disk invariant: at most one
	// tick draft on the parent chain at any time. See design-log/016
	// §"amend gap".
	e.mu.Lock()
	e.lastRefID = id
	e.mu.Unlock()
	// CommittedInfo mirrors the Committing stage so the loadedref subsystem
	// (design-log/044) can refresh settings.LoadedRefID off the bus without
	// caring whether the commit came from a stage or a livesync tick.
	e.bus.Publish(committing.CommittedInfo{RefID: id})
	e.bus.Publish(LiveDraftCommitted{RefID: id})

	if err := e.pusher.Push(ctx, id); err != nil {
		e.bus.Publish(ritual.ErrorInfo{Operation: "livesync.push", Err: err})
	}
}

// requestSave subscribes BEFORE publishing SaveRequested so a fast Java
// server cannot finish "save-all flush" and emit SaveCompleted in the
// gap between Publish and Subscribe. Per-subscriber FIFO guarantees we
// see SaveCompleted in the same channel.
func (e *Engine) requestSave(ctx context.Context) bool {
	ch, cancel := e.bus.Subscribe()
	defer cancel()

	e.bus.Publish(running.SaveRequested{})

	timer := time.NewTimer(e.saveTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			e.bus.Publish(ritual.ErrorInfo{Operation: "livesync.save", Err: errors.New("SaveCompleted timeout")})
			return false
		case ev, ok := <-ch:
			if !ok {
				return false
			}
			if _, isCompleted := ev.(running.SaveCompleted); isCompleted {
				return true
			}
		}
	}
}
