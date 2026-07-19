package adapters_test

import (
	"context"
	"net"
	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"testing"
	"time"
)

// TestReadiness_SuccessEmitsOkEvent: listener already accepting → probe
// succeeds on first dial. One ReadinessDialInfo with Attempt=1, Err=nil.
func TestReadiness_SuccessEmitsOkEvent(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	probe := adapters.NewTCPReadinessCheck(ln.Addr().String(), bus)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := probe.Wait(ctx); err != nil {
		t.Fatalf("probe.Wait: %v", err)
	}

	evt := expectReadinessEvent(t, ch, time.Second)
	if evt.Address != ln.Addr().String() {
		t.Errorf("address: want %s, got %s", ln.Addr().String(), evt.Address)
	}
	if evt.Attempt != 1 {
		t.Errorf("attempt: want 1, got %d", evt.Attempt)
	}
	if evt.Err != nil {
		t.Errorf("err: want nil, got %v", evt.Err)
	}
}

// TestReadiness_FailureThenSuccessEmitsAttempts: first dial fails (port
// closed), listener opens mid-probe, second (or later) dial succeeds.
// Expect N>=2 events; first fails, last succeeds with Attempt>=2.
func TestReadiness_FailureThenSuccessEmitsAttempts(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	t.Cleanup(unsub)

	probe := adapters.NewTCPReadinessCheck(addr, bus)
	probe.SetDialTimeout(20 * time.Millisecond)
	probe.SetInterval(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- probe.Wait(ctx) }()

	time.Sleep(40 * time.Millisecond)
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("relisten: %v", err)
	}
	t.Cleanup(func() { _ = ln2.Close() })

	if err := <-done; err != nil {
		t.Fatalf("probe.Wait: %v", err)
	}

	got := drainReadinessEvents(ch, 100*time.Millisecond)
	if len(got) < 2 {
		t.Fatalf("want >=2 events, got %d: %+v", len(got), got)
	}
	if got[0].Err == nil {
		t.Errorf("first event should be failure, got %+v", got[0])
	}
	last := got[len(got)-1]
	if last.Err != nil {
		t.Errorf("last event should be success, got %+v", last)
	}
	if last.Attempt < 2 {
		t.Errorf("success attempt should be >=2, got %d", last.Attempt)
	}
}

// TestReadiness_CancelReturnsErr: ctx cancel mid-retry returns ctx.Err().
func TestReadiness_CancelReturnsErr(t *testing.T) {
	t.Parallel()

	bus := adapters.NewEventBus(16)
	probe := adapters.NewTCPReadinessCheck("127.0.0.1:1", bus)
	probe.SetDialTimeout(20 * time.Millisecond)
	probe.SetInterval(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := probe.Wait(ctx); err == nil {
		t.Fatalf("want ctx error, got nil")
	}
}

func expectReadinessEvent(t *testing.T, ch <-chan ports.Event, timeout time.Duration) adapters.ReadinessDialInfo {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt := <-ch:
			if r, ok := evt.(adapters.ReadinessDialInfo); ok {
				return r
			}
		case <-deadline:
			t.Fatalf("no ReadinessDialInfo within %s", timeout)
		}
	}
}

func drainReadinessEvents(ch <-chan ports.Event, quiesce time.Duration) []adapters.ReadinessDialInfo {
	var out []adapters.ReadinessDialInfo
	for {
		select {
		case evt := <-ch:
			if r, ok := evt.(adapters.ReadinessDialInfo); ok {
				out = append(out, r)
			}
		case <-time.After(quiesce):
			return out
		}
	}
}
