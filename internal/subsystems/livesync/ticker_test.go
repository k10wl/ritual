package livesync_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/livesync"
	"sync/atomic"
	"testing"
	"time"
)

// Story: server reaches ready; ticker starts firing on its interval.
// Hook count reflects elapsed intervals — proves Attach subscribes and
// ServerReadyInfo opens syncCtx.
func TestAttach_FiresHookAfterServerReady(t *testing.T) {
	bus := adapters.NewEventBus(16)
	var fired atomic.Int32
	hook := func(context.Context) { fired.Add(1) }

	_, stop := livesync.Attach(bus, livesync.Options{Hook: hook, Interval: 20 * time.Millisecond})
	defer stop()

	bus.Publish(running.ServerReadyInfo{})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hook fired only %d times — expected >=2", fired.Load())
}

// Story: graceful shutdown — ServerStoppingInfo cancels syncCtx and no
// further hooks fire. Each lifecycle event in this group has the same
// effect (see ServerStoppedInfo / ServerCrashedInfo / LockLostInfo).
func TestAttach_StopsHookOnServerStopping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		evt  ports.Event
	}{
		{"ServerStoppingInfo", running.ServerStoppingInfo{}},
		{"ServerStoppedInfo", running.ServerStoppedInfo{}},
		{"ServerCrashedInfo", running.ServerCrashedInfo{}},
		{"LockLostInfo", ritual.LockLostInfo{RunID: "run-1", Reason: "test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := adapters.NewEventBus(16)
			var fired atomic.Int32
			hook := func(context.Context) { fired.Add(1) }

			_, stop := livesync.Attach(bus, livesync.Options{Hook: hook, Interval: 20 * time.Millisecond})
			defer stop()

			bus.Publish(running.ServerReadyInfo{})
			time.Sleep(80 * time.Millisecond)

			bus.Publish(tc.evt)
			time.Sleep(30 * time.Millisecond)
			before := fired.Load()
			time.Sleep(150 * time.Millisecond)
			after := fired.Load()
			if after != before {
				t.Fatalf("hook fired after stop: before=%d after=%d", before, after)
			}
		})
	}
}

// Story: slow hook + fast interval — second fire is skipped (not queued)
// while first is still running. Confirms CAS self-backpressure.
func TestAttach_OverlappingFireIsSkipped(t *testing.T) {
	bus := adapters.NewEventBus(16)
	release := make(chan struct{})
	var fired atomic.Int32
	hook := func(context.Context) {
		fired.Add(1)
		<-release
	}

	_, stop := livesync.Attach(bus, livesync.Options{Hook: hook, Interval: 20 * time.Millisecond})
	defer stop()
	defer close(release)

	bus.Publish(running.ServerReadyInfo{})

	// Wait for first fire to enter the hook.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("first hook never fired")
	}

	// Let several intervals elapse while first hook is blocked. Count
	// must NOT advance — CAS guard rejects overlap.
	time.Sleep(150 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Fatalf("overlapping fires not skipped: fired=%d (expected 1)", got)
	}
}

// Story: second ServerReadyInfo while already running is a no-op (no
// double goroutine, no double-fire on the same interval).
func TestAttach_DoubleStartIsNoOp(t *testing.T) {
	bus := adapters.NewEventBus(16)
	var fired atomic.Int32
	hook := func(context.Context) { fired.Add(1) }

	_, stop := livesync.Attach(bus, livesync.Options{Hook: hook, Interval: 50 * time.Millisecond})
	defer stop()

	bus.Publish(running.ServerReadyInfo{})
	bus.Publish(running.ServerReadyInfo{})

	time.Sleep(130 * time.Millisecond)
	// 130ms / 50ms interval ≈ 2 fires; would be ≈4 if duplicated.
	if got := fired.Load(); got > 3 {
		t.Fatalf("double-start spawned extra ticker: fired=%d", got)
	}
}

// Story: cancel returned by Attach drains the consumer goroutine and any
// in-flight hook before returning.
func TestAttach_CancelDrainsInFlightHook(t *testing.T) {
	bus := adapters.NewEventBus(16)
	entered := make(chan struct{})
	release := make(chan struct{})
	hook := func(context.Context) {
		close(entered)
		<-release
	}

	_, stop := livesync.Attach(bus, livesync.Options{Hook: hook, Interval: 20 * time.Millisecond})

	bus.Publish(running.ServerReadyInfo{})
	<-entered

	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()

	// stop() must block until hook releases — proves it waits on wg.
	select {
	case <-stopped:
		t.Fatal("stop returned while hook was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not drain after hook released")
	}
}
