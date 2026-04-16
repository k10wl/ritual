// Package heartbeat runs the remote-lock heartbeat loop out-of-band on
// the event bus. Acquiring publishes LockAcquiredInfo; the supervisor
// starts a beat goroutine. Unlocking publishes LockReleasedInfo; the
// supervisor stops. If a beat cycle discovers the manifest's LockedBy
// no longer matches, the supervisor publishes LockLostInfo so
// locked-span stages can short-circuit.
//
// The state machine does not carry lease state — the supervisor owns it
// entirely via bus events.
package heartbeat

import (
	"context"
	"sync"
	"time"

	"ritual/internal/core/ports"
)

type Supervisor struct {
	store ports.ManifestStore
	bus   ports.EventBus

	mu     sync.Mutex
	active map[string]context.CancelFunc
}

// Attach subscribes a new Supervisor to bus and returns it. The consumer
// goroutine runs until the returned cancel is called. Typically called
// once at composition root; cancel on program exit.
func Attach(bus ports.EventBus, store ports.ManifestStore) (*Supervisor, func()) {
	s := &Supervisor{
		store:  store,
		bus:    bus,
		active: map[string]context.CancelFunc{},
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
	case ports.LockAcquiredInfo:
		s.start(ev.RunID, ev.Interval)
	case ports.LockReleasedInfo:
		s.stop(ev.RunID)
	}
}

func (s *Supervisor) start(runID string, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if prev, ok := s.active[runID]; ok {
		prev()
	}
	s.active[runID] = cancel
	s.mu.Unlock()
	go s.beat(ctx, runID, interval)
}

func (s *Supervisor) stop(runID string) {
	s.mu.Lock()
	cancel, ok := s.active[runID]
	delete(s.active, runID)
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
	m, err := s.store.Get(ctx)
	if err != nil || m == nil {
		return
	}
	if m.LockedBy != runID {
		s.bus.Publish(ports.LockLostInfo{RunID: runID, Reason: "owner_mismatch"})
		s.stop(runID)
		return
	}
	m.HeartbeatAt = time.Now().UTC()
	_ = s.store.Save(ctx, m)
}
