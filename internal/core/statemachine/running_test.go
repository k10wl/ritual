package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	sm "ritual/internal/core/statemachine"
)

func TestRunning_Happy_RoutesToExiting(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	f := &stubFactory{}
	s := sm.NewRunningState(&domain.ServerRuntime{}, fakeServerRunner{}, local, remote, nil, f)
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next.Name() != sm.Exiting {
		t.Fatalf("next = %v, want Exiting", next.Name())
	}
	if f.lockID != "me" {
		t.Fatalf("lockID passed to Exiting = %q, want me", f.lockID)
	}
}

func TestRunning_ServerErr_StillRoutesToExiting(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	f := &stubFactory{}
	s := sm.NewRunningState(
		&domain.ServerRuntime{},
		fakeServerRunner{err: errors.New("server crashed")},
		local, remote, nil, f,
	)
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err must not bubble (lock-release guarantee): %v", err)
	}
	if next.Name() != sm.Exiting {
		t.Fatalf("next = %v, want Exiting (even on server error)", next.Name())
	}
}

func TestRunning_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &stubFactory{}
	s := sm.NewRunningState(&domain.ServerRuntime{}, fakeServerRunner{}, &fakeManifestStore{}, &fakeManifestStore{}, nil, f)
	next, _ := s.Handle(ctx)
	if next.Name() != sm.Failed || f.failedFrom != sm.Running {
		t.Fatalf("next=%v from=%v, want Failed/Running", next.Name(), f.failedFrom)
	}
}
