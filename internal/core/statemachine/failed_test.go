package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

func TestFailed_PublishesStateFailedAndReturnsErr(t *testing.T) {
	bus := adapters.NewEventBus(8)
	ch, cancel := bus.Subscribe()
	defer cancel()

	boom := errors.New("boom")
	s := sm.NewFailedState(bus, "run-1", sm.Preparing, boom)

	next, err := s.Handle(context.Background())
	if next != nil {
		t.Fatalf("next = %+v, want nil", next)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}

	select {
	case evt := <-ch:
		info, ok := evt.(ports.StateFailedInfo)
		if !ok {
			t.Fatalf("got %T, want StateFailedInfo", evt)
		}
		if info.State != "Preparing" || info.RunID != "run-1" || !errors.Is(info.Err, boom) {
			t.Fatalf("got %+v", info)
		}
	default:
		t.Fatal("no event published")
	}
}

func TestFailed_NilErrUsesSentinel(t *testing.T) {
	s := sm.NewFailedState(nil, "", sm.Running, nil)
	_, err := s.Handle(context.Background())
	if err == nil {
		t.Fatal("nil err must become a sentinel")
	}
}

func TestFailed_Name(t *testing.T) {
	s := sm.NewFailedState(nil, "", sm.Locking, errors.New("x"))
	if s.Name() != sm.Failed {
		t.Fatalf("name = %v, want Failed", s.Name())
	}
}
