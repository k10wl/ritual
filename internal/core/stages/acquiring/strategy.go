// Package acquiring takes the remote lock under the manifest's lease
// semantics. A lease is considered active iff LockedBy is set and
// HeartbeatAt is within Lease.TTL; stale leases are taken over. On
// partial failure (local saved, remote save failed) it routes to
// onRollback so the half-taken lock is released before terminating.
package acquiring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	local      ports.ManifestStore
	remote     ports.ManifestStore
	onOK       machine.Strategy[ritual.RunState]
	onFail     machine.Strategy[ritual.RunState]
	onRollback machine.Strategy[ritual.RunState]
}

func New(
	local, remote ports.ManifestStore,
	onOK, onFail, onRollback machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{local: local, remote: remote, onOK: onOK, onFail: onFail, onRollback: onRollback}
}

func (*Strategy) Name() string { return ritual.StageAcquiring }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ports.StartInfo{Operation: "acquire"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil
	}

	local, err := s.local.Get(ctx)
	if err != nil {
		rs.Err = fmt.Errorf("get local: %w", err)
		return s.onFail, nil
	}
	remote, err := s.remote.Get(ctx)
	if err != nil {
		rs.Err = fmt.Errorf("get remote: %w", err)
		return s.onFail, nil
	}
	if local == nil || remote == nil {
		rs.Err = errors.New("nil manifest")
		return s.onFail, nil
	}

	now := time.Now().UTC()
	if remote.IsLeaseActive(now) {
		rs.Err = fmt.Errorf("already locked by %s", remote.LockedBy)
		return s.onFail, nil
	}

	host, err := os.Hostname()
	if err != nil {
		rs.Err = fmt.Errorf("hostname: %w", err)
		return s.onFail, nil
	}
	lockID := fmt.Sprintf("%s%s%d", host, config.LockIDSeparator, now.UnixNano())
	applyLock(local, lockID, now)
	applyLock(remote, lockID, now)

	if err := s.local.Save(ctx, local); err != nil {
		rs.Err = fmt.Errorf("save local: %w", err)
		return s.onFail, nil
	}
	if err := s.remote.Save(ctx, remote); err != nil {
		rs.LockID = lockID
		rs.Err = fmt.Errorf("save remote: %w", err)
		return s.onRollback, nil
	}
	rs.LockID = lockID
	rs.LocalBefore = local
	rs.RemoteBefore = remote
	publish(rs.Bus, ports.FinishInfo{Operation: "acquire"})
	publish(rs.Bus, ports.LockAcquiredInfo{
		RunID:    rs.RunID,
		LockID:   lockID,
		Interval: time.Duration(remote.Lease.HeartbeatInterval),
	})
	return s.onOK, nil
}

func applyLock(m *domain.Manifest, lockID string, now time.Time) {
	m.Lock(lockID)
	m.HeartbeatAt = now
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
