// Package heartbeat runs the remote-lease heartbeat loop out-of-band on
// the event bus. Acquiring publishes LockAcquiredInfo; the supervisor
// starts a beat goroutine that calls lock.Locker.Heartbeat on Interval.
// Unlocking publishes LockReleasedInfo; the supervisor stops. If a beat
// cycle observes ErrLeaseLost (sessionId mismatch or object gone) the
// supervisor publishes LockLostInfo so locked-span stages can
// short-circuit.
//
// The state machine does not carry lease state — the supervisor owns it
// entirely via bus events.
package heartbeat

import (
	"context"
	"errors"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"sync"
	"time"
)

// HeartbeatFn refreshes the remote lease for the supplied sessionId.
// Returns lock.ErrLeaseLost if the lease was taken over or vanished.
// Injected so the supervisor stays decoupled from the Locker concrete
// type.
type HeartbeatFn func(ctx context.Context, sessionID string) error

// Supervisor owns the per-run heartbeat goroutines.
type Supervisor struct {
	heartbeat HeartbeatFn
	bus       ports.EventBus

	mu         sync.Mutex
	active     map[string]context.CancelFunc
	sessionIDs map[string]string
}

// Attach subscribes a new Supervisor to bus and returns it. The consumer
// goroutine runs until the returned cancel is called. Typically called
// once at composition root; cancel on program exit.
func Attach(bus ports.EventBus, heartbeat HeartbeatFn) (*Supervisor, func()) {
	s := &Supervisor{
		heartbeat:  heartbeat,
		bus:        bus,
		active:     map[string]context.CancelFunc{},
		sessionIDs: map[string]string{},
	}

	ch, cancelSub := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			s.handle(e)
		}
	}()
	cancel := func() {
		cancelSub()
		<-done
		s.stopAll()
	}
	return s, cancel
}

func (s *Supervisor) handle(e ports.Event) {
	switch ev := e.(type) {
	case ritual.LockAcquiredInfo:
		s.start(ev.RunID, ev.SessionID, ev.Interval)
	case ritual.LockReleasedInfo:
		s.stop(ev.RunID)
	case running.ServerStoppedInfo, running.ServerCrashedInfo:
		s.stopAll()
	}
}

func (s *Supervisor) start(runID, sessionID string, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in s.active and invoked by stop/stopAll
	s.mu.Lock()
	if prev, ok := s.active[runID]; ok {
		prev()
	}
	s.active[runID] = cancel
	s.sessionIDs[runID] = sessionID
	s.mu.Unlock()
	go s.beat(ctx, runID, interval)
}

func (s *Supervisor) stop(runID string) {
	s.mu.Lock()
	cancel, ok := s.active[runID]
	delete(s.active, runID)
	delete(s.sessionIDs, runID)
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	for id, cancel := range s.active {
		cancel()
		delete(s.active, id)
		delete(s.sessionIDs, id)
	}
	s.mu.Unlock()
}

func (s *Supervisor) beat(ctx context.Context, runID string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx, runID)
		}
	}
}

func (s *Supervisor) tick(ctx context.Context, runID string) {
	s.mu.Lock()
	sessionID := s.sessionIDs[runID]
	s.mu.Unlock()
	if sessionID == "" {
		return
	}

	if err := s.heartbeat(ctx, sessionID); err != nil {
		switch {
		case errors.Is(err, lock.ErrLeaseTakenOver):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "taken_over"})
			s.stop(runID)
		case errors.Is(err, lock.ErrLeaseVanished):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "vanished"})
			s.stop(runID)
		case errors.Is(err, lock.ErrLeaseLost):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "lease_lost"})
			s.stop(runID)
		default:
			s.bus.Publish(ritual.ErrorInfo{Operation: "heartbeat", Err: err})
		}
	}
}
