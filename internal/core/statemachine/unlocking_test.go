package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	sm "ritual/internal/core/statemachine"
)

func TestUnlocking_ReleasesMatchingLock(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	cause := errors.New("remote save failed")
	f := &stubFactory{}
	s := sm.NewUnlockingState(local, remote, nil, f, "me", cause)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v, want Failed", next.Name())
	}
	if !errors.Is(f.failedErr, cause) {
		t.Fatalf("failed cause = %v, want %v", f.failedErr, cause)
	}
	if local.saved == nil || local.saved.LockedBy != "" {
		t.Fatalf("local lock not released: %+v", local.saved)
	}
	if remote.saved == nil || remote.saved.LockedBy != "" {
		t.Fatalf("remote lock not released: %+v", remote.saved)
	}
}

func TestUnlocking_SkipsNonMatchingLock(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "someone-else"}}
	remote := &fakeManifestStore{m: &domain.Manifest{}}
	f := &stubFactory{}
	s := sm.NewUnlockingState(local, remote, nil, f, "me", errors.New("cause"))
	_, _ = s.Handle(context.Background())
	if local.saved != nil {
		t.Fatal("local saved despite lockID mismatch")
	}
	if remote.saved != nil {
		t.Fatal("remote saved despite lockID mismatch")
	}
}

func TestUnlocking_IgnoresCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	f := &stubFactory{}
	s := sm.NewUnlockingState(local, remote, nil, f, "me", errors.New("cause"))
	next, _ := s.Handle(ctx)
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v, want Failed", next.Name())
	}
	if local.saved == nil || local.saved.LockedBy != "" {
		t.Fatal("cancelled ctx aborted Unlocking — stranded lock on local")
	}
}
