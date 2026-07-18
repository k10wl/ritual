package livesync_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/subsystems/livesync"
	"sync"
	"testing"
	"time"
)

// Story: dispatcher writes incoming LiveDraftCommitted RefID into the
// caller-owned slot. Production maps this to rs.RefID = id.
func TestDispatcher_AppliesIncomingRefID(t *testing.T) {
	bus := adapters.NewEventBus(8)
	var mu sync.Mutex
	var applied domain.RefID
	apply := func(id domain.RefID) {
		mu.Lock()
		applied = id
		mu.Unlock()
	}
	d, stop := livesync.NewDispatcher(bus, apply)
	defer stop()

	bus.Publish(livesync.LiveDraftCommitted{RefID: "A"})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := applied
		mu.Unlock()
		if got == "A" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := applied
	mu.Unlock()
	if got != "A" {
		t.Fatalf("apply slot=%q, expected A", got)
	}
	if last := d.LastApplied(); last != "A" {
		t.Fatalf("LastApplied=%q, expected A", last)
	}
}

// Story: Sync returns immediately when want == LastApplied.
func TestDispatcher_SyncReturnsOnCurrentMatch(t *testing.T) {
	bus := adapters.NewEventBus(8)
	d, stop := livesync.NewDispatcher(bus, func(domain.RefID) {})
	defer stop()

	bus.Publish(livesync.LiveDraftCommitted{RefID: "A"})

	// Wait for dispatcher to catch up before calling Sync.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if d.LastApplied() == "A" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := d.Sync(ctx, "A"); err != nil {
		t.Fatalf("Sync should return nil on match: %v", err)
	}
}

// Story: Sync blocks until LastApplied advances to want. Confirms the
// barrier semantics Drain relies on.
func TestDispatcher_SyncBlocksUntilApplied(t *testing.T) {
	bus := adapters.NewEventBus(8)
	d, stop := livesync.NewDispatcher(bus, func(domain.RefID) {})
	defer stop()

	syncDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		syncDone <- d.Sync(ctx, "TICK-3")
	}()

	select {
	case <-syncDone:
		t.Fatal("Sync returned before TICK-3 was applied")
	case <-time.After(50 * time.Millisecond):
	}

	bus.Publish(livesync.LiveDraftCommitted{RefID: "TICK-1"})
	bus.Publish(livesync.LiveDraftCommitted{RefID: "TICK-2"})
	time.Sleep(50 * time.Millisecond)

	select {
	case <-syncDone:
		t.Fatal("Sync returned before TICK-3")
	case <-time.After(30 * time.Millisecond):
	}

	bus.Publish(livesync.LiveDraftCommitted{RefID: "TICK-3"})

	select {
	case err := <-syncDone:
		if err != nil {
			t.Fatalf("Sync err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Sync did not return after TICK-3 applied")
	}
}

// Story: Sync honours ctx cancellation — Drain timeout (10s ceiling)
// propagates correctly.
func TestDispatcher_SyncRespectsCtxCancel(t *testing.T) {
	bus := adapters.NewEventBus(8)
	d, stop := livesync.NewDispatcher(bus, func(domain.RefID) {})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := d.Sync(ctx, "NEVER")
	if err == nil {
		t.Fatal("Sync must return ctx.Err on cancellation")
	}
}

// Story: empty want is a no-op — first-session shutdown with zero ticks
// must not block Drain.
func TestDispatcher_SyncEmptyWantIsNoOp(t *testing.T) {
	bus := adapters.NewEventBus(8)
	d, stop := livesync.NewDispatcher(bus, func(domain.RefID) {})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := d.Sync(ctx, ""); err != nil {
		t.Fatalf("Sync(\"\") must return nil, got %v", err)
	}
}

// Story: bus close (stop()) wakes pending Sync with ctx.Err so callers
// don't hang on shutdown ordering bugs.
func TestDispatcher_StopWakesPendingSync(t *testing.T) {
	bus := adapters.NewEventBus(8)
	d, stop := livesync.NewDispatcher(bus, func(domain.RefID) {})

	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result <- d.Sync(ctx, "NEVER-COMES")
	}()

	time.Sleep(30 * time.Millisecond)
	stop()

	select {
	case err := <-result:
		_ = err // either ctx.Err or nil — either way must NOT hang
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Sync did not unblock after dispatcher stop")
	}
}
