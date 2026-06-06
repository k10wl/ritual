package notify_test

import (
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/notify"
	"sync"
	"testing"
	"time"
)

type call struct{ id, title, body string }

// fakeNotifier records every Notify under a mutex and signals on ch so a test
// can wait for an exact number of toasts. notify.send dispatches each Notify on
// a detached goroutine, so tests must synchronise on the count rather than
// assume the call has landed by the time Publish returns.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []call
	ch    chan struct{}
}

func newFake() *fakeNotifier { return &fakeNotifier{ch: make(chan struct{}, 16)} }

func (f *fakeNotifier) Notify(id, title, body string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call{id, title, body})
	f.mu.Unlock()
	f.ch <- struct{}{}
	return nil
}

func (f *fakeNotifier) snapshot() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// waitN blocks until at least n toasts have been recorded (cumulative) or the
// deadline passes. Re-checks the recorded count after each signal so repeated
// calls compose correctly.
func (f *fakeNotifier) waitN(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(f.snapshot()) >= n {
			return
		}
		select {
		case <-f.ch:
		case <-deadline:
			t.Fatalf("only %d/%d toasts arrived: %+v", len(f.snapshot()), n, f.snapshot())
		}
	}
}

// assertSilent fails if any toast arrives within a short window.
func (f *fakeNotifier) assertSilent(t *testing.T) {
	t.Helper()
	select {
	case <-f.ch:
		t.Fatalf("unexpected toast: %+v", f.snapshot())
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAttach_StartThenStop(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(running.ServerReadyInfo{})
	f.waitN(t, 1)
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})
	f.waitN(t, 2)

	calls := f.snapshot()
	if calls[0].body != "Server started" {
		t.Fatalf("first toast body = %q, want %q", calls[0].body, "Server started")
	}
	if calls[1].body != "Server stopped" {
		t.Fatalf("second toast body = %q, want %q", calls[1].body, "Server stopped")
	}
	// Tests run via `go test` which keeps the default config.AppName
	// ("ritualdev"), so DisplayName() returns "Ritual Dev".
	if calls[0].title != "Ritual Dev" {
		t.Fatalf("title = %q, want %q", calls[0].title, "Ritual Dev")
	}
}

func TestAttach_NonServerDoneStaysSilent(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	// A server-free flow (e.g. Download): no ServerReadyInfo, so its clean Done
	// must NOT toast — only real server stops are critical.
	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowDownload})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})
	f.assertSilent(t)
}

func TestAttach_FailureToastsForAnyFlow(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	// A Download that never reached server-ready still toasts on failure
	// (confirmed Q3: any failure stage is critical), with the error in the body.
	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowDownload})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed, Err: errors.New("pull: connection reset")})
	f.waitN(t, 1)

	got := f.snapshot()[0]
	if got.body != "Run failed: pull: connection reset" {
		t.Fatalf("failure body = %q", got.body)
	}
}

func TestAttach_FailureWithoutErr(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed})
	f.waitN(t, 1)

	if got := f.snapshot()[0].body; got != "Run failed" {
		t.Fatalf("body = %q, want bare %q", got, "Run failed")
	}
}

func TestAttach_RunSeqMakesUniqueIDs(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(running.ServerReadyInfo{})
	f.waitN(t, 1)
	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(running.ServerReadyInfo{})
	f.waitN(t, 2)

	calls := f.snapshot()
	if calls[0].id == calls[1].id {
		t.Fatalf("ids not unique across runs: both %q", calls[0].id)
	}
	if calls[0].id != "ritual-ready-1" || calls[1].id != "ritual-ready-2" {
		t.Fatalf("ids = %q, %q; want ritual-ready-1, ritual-ready-2", calls[0].id, calls[1].id)
	}
}

func TestAttach_StopIsIdempotent(t *testing.T) {
	bus := adapters.NewEventBus(16)
	stop := notify.Attach(t.Context(), bus, newFake())
	stop()
	stop() // must not panic
}

func TestAttach_IdleAndRunningIgnored(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFake()
	stop := notify.Attach(t.Context(), bus, f)
	defer stop()

	// Non-terminal / non-critical transitions never toast.
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Idle})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Running})
	f.assertSilent(t)
}
