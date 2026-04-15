package statemachine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// LockingState reads both manifests, writes lockID, commits both. If remote
// save fails after local succeeded, routes to Unlocking for rollback.
type LockingState struct {
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	bus             ports.EventBus
	factory         StateFactory
}

func NewLockingState(local, remote ports.ManifestStore, bus ports.EventBus, f StateFactory) *LockingState {
	return &LockingState{localManifests: local, remoteManifests: remote, bus: bus, factory: f}
}

func (*LockingState) Name() StateName { return Locking }

func (s *LockingState) Handle(ctx context.Context) (Handler, error) {
	publish(s.bus, ports.StartInfo{Operation: "lock"})
	if next := ctxFailed(ctx, s.factory, Locking); next != nil {
		return next, nil
	}

	local, err := s.localManifests.Get(ctx)
	if err != nil {
		return s.factory.Failed(Locking, fmt.Errorf("get local: %w", err)), nil
	}
	remote, err := s.remoteManifests.Get(ctx)
	if err != nil {
		return s.factory.Failed(Locking, fmt.Errorf("get remote: %w", err)), nil
	}
	if local == nil || remote == nil {
		return s.factory.Failed(Locking, errors.New("nil manifest")), nil
	}
	if local.LockedBy != "" || remote.LockedBy != "" {
		return s.factory.Failed(Locking, errors.New("already locked")), nil
	}

	host, err := os.Hostname()
	if err != nil {
		return s.factory.Failed(Locking, fmt.Errorf("hostname: %w", err)), nil
	}
	lockID := fmt.Sprintf("%s%s%d", host, config.LockIDSeparator, time.Now().UnixNano())
	local.Lock(lockID)
	remote.Lock(lockID)

	if err := s.localManifests.Save(ctx, local); err != nil {
		return s.factory.Failed(Locking, fmt.Errorf("save local: %w", err)), nil
	}
	if err := s.remoteManifests.Save(ctx, remote); err != nil {
		return s.factory.Unlocking(lockID, fmt.Errorf("save remote: %w", err)), nil
	}
	publish(s.bus, ports.FinishInfo{Operation: "lock"})
	return s.factory.Running(), nil
}

func (f *factory) Locking() Handler {
	return NewLockingState(f.d.LocalManifests, f.d.RemoteManifests, f.d.Bus, f)
}
