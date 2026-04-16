package failed_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/failed"
)

func TestFailedEmitsEventAndReturnsErr(t *testing.T) {
	bus := adapters.NewEventBus(8)
	ch, cancel := bus.Subscribe()
	defer cancel()

	rs := &ritual.RunState{RunID: "r-1", Bus: bus, Err: errors.New("boom")}
	s := failed.New(ritual.StageChecking)

	next, err := s.Run(context.Background(), rs)
	if next != nil {
		t.Fatal("Failed must terminate")
	}
	if !errors.Is(err, rs.Err) {
		t.Fatalf("want boom, got %v", err)
	}

	select {
	case e := <-ch:
		got, ok := e.(ports.StateFailedInfo)
		if !ok {
			t.Fatalf("want StateFailedInfo, got %T", e)
		}
		if got.State != ritual.StageChecking {
			t.Fatalf("From: %s", got.State)
		}
		if got.RunID != "r-1" {
			t.Fatalf("RunID: %s", got.RunID)
		}
	default:
		t.Fatal("no event published")
	}
}

func TestFailedSynthesisesErrorWhenNil(t *testing.T) {
	bus := adapters.NewEventBus(4)
	rs := &ritual.RunState{RunID: "r-2", Bus: bus}
	s := failed.New(ritual.StageRunning)

	_, err := s.Run(context.Background(), rs)
	if err == nil {
		t.Fatal("expected synthesised error")
	}
}
