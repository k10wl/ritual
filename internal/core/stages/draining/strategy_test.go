package draining_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/draining"
	"sync/atomic"
	"testing"
)

type stubDrainable struct {
	calls atomic.Int32
	err   error
}

func (s *stubDrainable) Drain(context.Context) error {
	s.calls.Add(1)
	return s.err
}

// Story: nil Drainable → no-op pass-through. Tests + CLI fakerun rely
// on this so the historical Running → Committing chain stays untouched
// when livesync isn't wired.
func TestStrategy_NilDrainable_PassThroughNoEvents(t *testing.T) {
	bus := adapters.NewEventBus(8)
	ch, cancel := bus.Subscribe()
	defer cancel()

	onNext := &recordingStrategy{}
	s := draining.New(nil, onNext)

	rs := &ritual.RunState{Bus: bus}
	next, err := s.Run(context.Background(), rs)
	if err != nil {
		t.Fatalf("Run err=%v", err)
	}
	if next != onNext {
		t.Fatal("nil Drainable must route to onNext unchanged")
	}
	select {
	case e := <-ch:
		t.Fatalf("nil Drainable must publish no events, got: %v", e)
	default:
	}
}

// Story: Drainable returning nil → success path, Start/Finish events
// surround the call, onNext is returned.
func TestStrategy_HappyPath_PublishesStartAndFinish(t *testing.T) {
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	drainable := &stubDrainable{}
	onNext := &recordingStrategy{}
	s := draining.New(drainable, onNext)

	rs := &ritual.RunState{Bus: bus}
	next, err := s.Run(context.Background(), rs)
	if err != nil {
		t.Fatalf("Run err=%v", err)
	}
	if next != onNext {
		t.Fatalf("next != onNext")
	}
	if got := drainable.calls.Load(); got != 1 {
		t.Fatalf("Drain called %d times, expected 1", got)
	}

	want := []string{
		"start drain",
		"finish drain",
	}
	var got []string
	for range want {
		select {
		case e := <-ch:
			got = append(got, e.String())
		default:
		}
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events %v, want %v", got, want)
	}
}

// Story: Drainable timeout (ctx.Err) is non-fatal — Drain publishes
// ErrorInfo and proceeds. OQ5 escalate-and-abandon: sweep on next
// session is the safety net.
func TestStrategy_DrainErrorIsNonFatal(t *testing.T) {
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	drainable := &stubDrainable{err: errors.New("drain timeout")}
	onNext := &recordingStrategy{}
	s := draining.New(drainable, onNext)

	rs := &ritual.RunState{Bus: bus}
	next, err := s.Run(context.Background(), rs)
	if err != nil {
		t.Fatalf("Run err=%v (must be nil — timeout is non-fatal)", err)
	}
	if next != onNext {
		t.Fatal("error must still route to onNext, not fail")
	}

	var seenError bool
	for range 3 {
		select {
		case e := <-ch:
			if _, ok := e.(ritual.ErrorInfo); ok {
				seenError = true
			}
		default:
		}
	}
	if !seenError {
		t.Fatal("Drain error must publish ErrorInfo")
	}
}

// recordingStrategy is the cheapest possible no-op onNext.
type recordingStrategy struct{}

func (*recordingStrategy) Name() string { return "Recording" }
func (r *recordingStrategy) Run(context.Context, *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil
}
