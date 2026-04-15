package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

func TestPreparing_Happy(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewPreparingState(
		[]ports.ConditionService{fakeCond{}},
		[]ports.UpdaterService{fakeUpd{}},
		nil, f,
	)
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next.Name() != sm.Locking {
		t.Fatalf("next = %v", next.Name())
	}
}

func TestPreparing_CondFail(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewPreparingState(
		[]ports.ConditionService{fakeCond{err: errors.New("bad")}},
		nil, nil, f,
	)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v", next.Name())
	}
	if f.failedFrom != sm.Preparing {
		t.Fatalf("failedFrom = %q, want Preparing", f.failedFrom)
	}
}

func TestPreparing_UpdaterFail(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewPreparingState(
		[]ports.ConditionService{fakeCond{}},
		[]ports.UpdaterService{fakeUpd{err: errors.New("update failed")}},
		nil, f,
	)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v", next.Name())
	}
}

func TestPreparing_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &stubFactory{}
	s := sm.NewPreparingState(nil, nil, nil, f)
	next, _ := s.Handle(ctx)
	if next.Name() != sm.Failed {
		t.Fatalf("ctx-cancelled should route to Failed; got %v", next.Name())
	}
	if f.failedFrom != sm.Preparing {
		t.Fatalf("failedFrom = %q", f.failedFrom)
	}
}
