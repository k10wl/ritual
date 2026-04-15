package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	sm "ritual/internal/core/statemachine"
)

func TestLocking_Happy(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{}}
	remote := &fakeManifestStore{m: &domain.Manifest{}}
	f := &stubFactory{}
	s := sm.NewLockingState(local, remote, nil, f)
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next.Name() != sm.Running {
		t.Fatalf("next = %v, want Running", next.Name())
	}
	if local.saved == nil || local.saved.LockedBy == "" {
		t.Fatal("local manifest not saved with lockID")
	}
	if remote.saved == nil || remote.saved.LockedBy == "" {
		t.Fatal("remote manifest not saved with lockID")
	}
	if local.saved.LockedBy != remote.saved.LockedBy {
		t.Fatalf("lockID mismatch: local=%q remote=%q", local.saved.LockedBy, remote.saved.LockedBy)
	}
}

func TestLocking_AlreadyLocked(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "someone-else"}}
	remote := &fakeManifestStore{m: &domain.Manifest{}}
	f := &stubFactory{}
	s := sm.NewLockingState(local, remote, nil, f)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v, want Failed", next.Name())
	}
}

func TestLocking_GetLocalErr(t *testing.T) {
	local := &fakeManifestStore{getErr: errors.New("get fail")}
	remote := &fakeManifestStore{m: &domain.Manifest{}}
	f := &stubFactory{}
	s := sm.NewLockingState(local, remote, nil, f)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed {
		t.Fatalf("next = %v, want Failed", next.Name())
	}
	if f.failedFrom != sm.Locking {
		t.Fatalf("failedFrom = %q", f.failedFrom)
	}
}

func TestLocking_SaveRemoteErrRollsBackViaUnlocking(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{}}
	remote := &fakeManifestStore{m: &domain.Manifest{}, saveErr: errors.New("remote save fail")}
	f := &stubFactory{}
	s := sm.NewLockingState(local, remote, nil, f)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Unlocking {
		t.Fatalf("next = %v, want Unlocking", next.Name())
	}
	if f.lockID == "" {
		t.Fatal("Unlocking factory called without lockID")
	}
}

func TestLocking_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &stubFactory{}
	s := sm.NewLockingState(&fakeManifestStore{}, &fakeManifestStore{}, nil, f)
	next, _ := s.Handle(ctx)
	if next.Name() != sm.Failed || f.failedFrom != sm.Locking {
		t.Fatalf("next=%v from=%v, want Failed/Locking", next.Name(), f.failedFrom)
	}
}
