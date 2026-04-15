package statemachine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

func TestExiting_NoLock_IsTerminal(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewExitingState(nil, nil, nil, nil, nil, nil, nil, f, "", nil, nil)
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next != nil {
		t.Fatalf("next = %+v, want nil (terminal)", next)
	}
}

func TestExiting_ExitUpdaterFail(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewExitingState(
		[]ports.UpdaterService{fakeUpd{err: errors.New("upload failed")}},
		nil, nil, nil,
		&fakeManifestStore{m: &domain.Manifest{}},
		&fakeManifestStore{m: &domain.Manifest{}},
		nil, f, "me", &domain.Manifest{}, &domain.Manifest{},
	)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed || f.failedFrom != sm.Exiting {
		t.Fatalf("next=%v from=%v, want Failed/Exiting", next.Name(), f.failedFrom)
	}
}

func TestExiting_RetentionFail(t *testing.T) {
	f := &stubFactory{}
	s := sm.NewExitingState(
		nil,
		[]ports.RetentionService{fakeRetention{err: errors.New("retention failed")}},
		nil, nil,
		&fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}},
		&fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}},
		nil, f, "me", &domain.Manifest{}, &domain.Manifest{},
	)
	next, _ := s.Handle(context.Background())
	if next.Name() != sm.Failed || f.failedFrom != sm.Exiting {
		t.Fatalf("next=%v from=%v, want Failed/Exiting", next.Name(), f.failedFrom)
	}
}

func TestExiting_ReleasesLock(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	f := &stubFactory{}
	s := sm.NewExitingState(nil, nil, nil, nil, local, remote, nil, f, "me", &domain.Manifest{}, &domain.Manifest{})
	next, err := s.Handle(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next != nil {
		t.Fatalf("next = %+v, want nil", next)
	}
	if local.saved == nil || local.saved.LockedBy != "" {
		t.Fatalf("local lock not released: %+v", local.saved)
	}
	if remote.saved == nil || remote.saved.LockedBy != "" {
		t.Fatalf("remote lock not released: %+v", remote.saved)
	}
}

func TestExiting_SkipsReleaseIfLockIDMismatch(t *testing.T) {
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "someone-else"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "someone-else"}}
	f := &stubFactory{}
	s := sm.NewExitingState(nil, nil, nil, nil, local, remote, nil, f, "me", &domain.Manifest{}, &domain.Manifest{})
	_, _ = s.Handle(context.Background())
	if local.saved != nil {
		t.Fatal("local was saved despite lockID mismatch")
	}
	if remote.saved != nil {
		t.Fatal("remote was saved despite lockID mismatch")
	}
}

func TestExiting_IgnoresCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	local := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	remote := &fakeManifestStore{m: &domain.Manifest{LockedBy: "me"}}
	f := &stubFactory{}
	s := sm.NewExitingState(nil, nil, nil, nil, local, remote, nil, f, "me", &domain.Manifest{}, &domain.Manifest{})
	next, err := s.Handle(ctx)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if next != nil {
		t.Fatalf("cancelled ctx should NOT prevent exit completion; next=%+v", next)
	}
	if local.saved == nil || local.saved.LockedBy != "" {
		t.Fatal("cancelled ctx aborted lock release — violates Exiting contract")
	}
}
