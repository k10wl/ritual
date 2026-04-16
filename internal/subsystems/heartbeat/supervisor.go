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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ritual/internal/core/ports"
)

type Supervisor struct {
	localStore  ports.ManifestStore
	remoteStore ports.ManifestStore
	syncer      ports.SyncService
	bus         ports.EventBus

	mu         sync.Mutex
	active     map[string]context.CancelFunc
	syncCtx    context.Context
	syncCancel context.CancelFunc
	syncReady  atomic.Bool
}

// Attach subscribes a new Supervisor to bus and returns it. The consumer
// goroutine runs until the returned cancel is called. Typically called
// once at composition root; cancel on program exit.
func Attach(bus ports.EventBus, localStore, remoteStore ports.ManifestStore, syncer ports.SyncService) (*Supervisor, func()) {
	cancelledCtx, cancelledCancel := context.WithCancel(context.Background())
	cancelledCancel() // starts cancelled — sync only after ServerReadyInfo

	s := &Supervisor{
		localStore:  localStore,
		remoteStore: remoteStore,
		syncer:      syncer,
		bus:         bus,
		active:      map[string]context.CancelFunc{},
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
	case ports.LockAcquiredInfo:
		s.start(ev.RunID, ev.Interval)
	case ports.LockReleasedInfo:
		s.stop(ev.RunID)
	case ports.ServerReadyInfo:
		s.mu.Lock()
		s.syncCtx, s.syncCancel = context.WithCancel(context.Background())
		s.mu.Unlock()
	case ports.ServerStoppedInfo:
		s.cancelSync()
	case ports.ServerCrashedInfo:
		s.cancelSync()
	case ports.ServerOutputInfo:
		if strings.Contains(ev.Line, "Stopping the server") {
			s.cancelSync()
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
	m, err := s.remoteStore.Get(ctx)
	if err != nil || m == nil {
		return
	}
	if m.LockedBy != runID {
		s.bus.Publish(ports.LockLostInfo{RunID: runID, Reason: "owner_mismatch"})
		s.stop(runID)
		return
	}

	// heartbeat — always, synchronous
	m.HeartbeatAt = time.Now().UTC()
	_ = s.remoteStore.Save(ctx, m)

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
	go s.syncTick(syncCtx)
}

func (s *Supervisor) syncTick(ctx context.Context) {
	defer s.syncReady.Store(true)

	s.bus.Publish(ports.SaveRequested{})

	// wait for SaveCompleted
	ch, unsub := s.bus.Subscribe()
	defer unsub()
	timer := time.NewTimer(30 * time.Second)
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
			if _, ok := e.(ports.SaveCompleted); ok {
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
	remote.HeartbeatAt = time.Now().UTC()

	_ = s.localStore.Save(ctx, local)
	_ = s.remoteStore.Save(ctx, remote)
}
