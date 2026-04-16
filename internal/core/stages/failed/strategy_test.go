package failed_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/machine"
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

func TestFailedSetsFailedStageOnRunState(t *testing.T) {
	bus := adapters.NewEventBus(8)
	rs := &ritual.RunState{RunID: "r-1", Bus: bus, Err: errors.New("boom")}
	s := failed.New(ritual.StageChecking)

	s.Run(context.Background(), rs)

	if rs.FailedStage != ritual.StageChecking {
		t.Fatalf("want StageChecking, got %s", rs.FailedStage)
	}
}

func TestFailedRetryFollowsOnRetryEdge(t *testing.T) {
	bus := adapters.NewEventBus(8)
	rs := &ritual.RunState{RunID: "r-1", Bus: bus, Err: errors.New("boom")}

	retryTarget := &mockStrategy{name: "Checking"}
	s := failed.New(ritual.StageChecking)
	s.SetRetry(retryTarget)

	// First run: terminates with error
	next, err := s.Run(context.Background(), rs)
	if next != nil {
		t.Fatal("first run must terminate (next should be nil)")
	}
	if err == nil {
		t.Fatal("expected error")
	}

	// Retry: clear error, follow onRetry edge
	rs.Err = nil
	next, err = s.Run(context.Background(), rs)
	if err != nil {
		t.Fatalf("retry should not error: %v", err)
	}
	if next != retryTarget {
		t.Fatal("retry must follow onRetry edge")
	}
}

type mockStrategy struct {
	name string
}

func (m *mockStrategy) Name() string { return m.name }
func (m *mockStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil
}
