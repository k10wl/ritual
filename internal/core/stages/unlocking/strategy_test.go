package unlocking_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/unlocking"
	"testing"
)

func TestUnlockingClearsLocksMatchingLockID(t *testing.T) {
	local := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "lock-1"}, nil
		},
	}
	remote := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "lock-1"}, nil
		},
	}
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), LockID: "lock-1"}
	s := unlocking.New(local, remote, nil)

	next, err := s.Run(context.Background(), rs)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("want nil next, got %T", next)
	}
	if local.SaveCalls != 1 || remote.SaveCalls != 1 {
		t.Fatalf("saves: local=%d remote=%d", local.SaveCalls, remote.SaveCalls)
	}
}

func TestUnlockingSkipsMismatchedLockID(t *testing.T) {
	local := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "other"}, nil
		},
	}
	remote := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: ""}, nil
		},
	}
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), LockID: "lock-1"}
	s := unlocking.New(local, remote, nil)

	_, err := s.Run(context.Background(), rs)
	if err != nil {
		t.Fatal(err)
	}
	if local.SaveCalls != 0 || remote.SaveCalls != 0 {
		t.Fatalf("unexpected saves: local=%d remote=%d", local.SaveCalls, remote.SaveCalls)
	}
}

func TestUnlockingHonorsCancelledParentViaWithoutCancel(t *testing.T) {
	local := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "lock-1"}, nil
		},
	}
	remote := &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) {
			return &domain.Manifest{LockedBy: "lock-1"}, nil
		},
	}
	rs := &ritual.RunState{RunID: "r", Bus: adapters.NewEventBus(4), LockID: "lock-1"}
	s := unlocking.New(local, remote, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Run(ctx, rs); err != nil {
		t.Fatal(err)
	}
	if local.SaveCalls != 1 || remote.SaveCalls != 1 {
		t.Fatalf("saves despite cancel: local=%d remote=%d", local.SaveCalls, remote.SaveCalls)
	}
}
