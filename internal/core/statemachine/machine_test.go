package statemachine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

type step struct {
	n    sm.StateName
	next sm.Handler
	err  error
}

func (s *step) Name() sm.StateName                           { return s.n }
func (s *step) Handle(_ context.Context) (sm.Handler, error) { return s.next, s.err }

func TestMachine_TransitionsAndEmitsChange(t *testing.T) {
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	defer cancel()

	end := &step{n: "B"}
	start := &step{n: "A", next: end}

	m := sm.NewMachine(start, bus, "r1")
	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	var got []ports.StateChangedInfo
	deadline := time.After(100 * time.Millisecond)
	for len(got) < 2 {
		select {
		case evt := <-ch:
			if sc, ok := evt.(ports.StateChangedInfo); ok {
				got = append(got, sc)
			}
		case <-deadline:
			t.Fatalf("got %d transitions, want 2", len(got))
		}
	}
	if got[0].From != "A" || got[0].To != "B" {
		t.Errorf("transition[0] = %+v", got[0])
	}
	if got[1].From != "B" || got[1].To != "Done" {
		t.Errorf("transition[1] = %+v", got[1])
	}
}

func TestMachine_PropagatesError(t *testing.T) {
	bus := adapters.NewEventBus(16)
	boom := errors.New("boom")
	m := sm.NewMachine(&step{n: "A", err: boom}, bus, "r1")
	if err := m.Run(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestMachine_NilInitial(t *testing.T) {
	m := sm.NewMachine(nil, nil, "r1")
	if err := m.Run(context.Background()); err == nil {
		t.Fatal("nil initial state must error")
	}
}
