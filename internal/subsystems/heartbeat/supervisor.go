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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// HeartbeatFn refreshes the remote lease for the supplied sessionId.
// Returns lock.ErrLeaseLost if the lease was taken over or vanished.
// Injected so the supervisor stays decoupled from the Locker concrete
// type.
type HeartbeatFn func(ctx context.Context, sessionID string) error

var saveWaitTimeout = 30 * time.Second

func init() {
	if testing.Testing() {
		saveWaitTimeout = 10 * time.Millisecond
	}
}

// Supervisor owns the per-run heartbeat and sync-tick goroutines.
type Supervisor struct {
	heartbeat   HeartbeatFn
	localStore  ports.ManifestStore
	remoteStore ports.ManifestStore
	syncer      ports.SyncService
	bus         ports.EventBus

	mu         sync.Mutex
	active     map[string]context.CancelFunc
	sessionIDs map[string]string
	syncCtx    context.Context
	syncCancel context.CancelFunc
	syncReady  atomic.Bool
}

// Attach subscribes a new Supervisor to bus and returns it. The consumer
// goroutine runs until the returned cancel is called. Typically called
// once at composition root; cancel on program exit.
func Attach(bus ports.EventBus, heartbeat HeartbeatFn, localStore, remoteStore ports.ManifestStore, syncer ports.SyncService) (*Supervisor, func()) {
	cancelledCtx, cancelledCancel := context.WithCancel(context.Background())
	cancelledCancel() // starts cancelled — sync only after ServerReadyInfo

	s := &Supervisor{
		heartbeat:   heartbeat,
		localStore:  localStore,
		remoteStore: remoteStore,
		syncer:      syncer,
		bus:         bus,
		active:      map[string]context.CancelFunc{},
		sessionIDs:  map[string]string{},
		syncCtx:     cancelledCtx,
		syncCancel:  cancelledCancel,
	}
	s.syncReady.Store(true)

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
	case running.ServerReadyInfo:
		s.mu.Lock()
		s.syncCtx, s.syncCancel = context.WithCancel(context.Background())
		s.mu.Unlock()
	case running.ServerStoppedInfo:
		s.cancelSync()
		s.stopAll()
	case running.ServerCrashedInfo:
		s.cancelSync()
		s.stopAll()
	case running.ServerOutputInfo:
		if strings.Contains(ev.Line, "Stopping the server") {
			s.cancelSync()
			s.stopAll()
		}
	}
}

func (s *Supervisor) cancelSync() {
	s.mu.Lock()
	if s.syncCancel != nil {
		s.syncCancel()
	}
	s.mu.Unlock()
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
	if s.syncCancel != nil {
		s.syncCancel()
	}
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
	if s.syncCancel != nil {
		s.syncCancel()
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
		}
		return
	}

	// sync — only when server running and previous sync finished
	s.mu.Lock()
	syncCtx := s.syncCtx
	s.mu.Unlock()

	if syncCtx.Err() != nil {
		return
	}
	if !s.syncReady.CompareAndSwap(true, false) {
		return
	}
	go s.syncTick(syncCtx) //nolint:contextcheck // syncCtx has independent lifetime tied to server lifecycle
}

func (s *Supervisor) syncTick(ctx context.Context) {
	defer s.syncReady.Store(true)

	ch, unsub := s.bus.Subscribe()
	defer unsub()
	s.bus.Publish(running.SaveRequested{})

	timer := time.NewTimer(saveWaitTimeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if _, ok := e.(running.SaveCompleted); ok {
				goto saved
			}
		}
	}
saved:

	local, err := s.localStore.Get(ctx)
	if err != nil || local == nil {
		return
	}
	remote, err := s.remoteStore.Get(ctx)
	if err != nil || remote == nil {
		return
	}

	newState, err := s.syncer.Upload(ctx, local.Worlds.SyncState, remote.Worlds.SyncState)
	if err != nil {
		return
	}

	local.Worlds.SyncState = newState
	remote.Worlds.SyncState = newState

	_ = s.localStore.Save(ctx, local)
	_ = s.remoteStore.Save(ctx, remote)
}
