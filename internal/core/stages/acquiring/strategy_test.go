package acquiring_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubManifestStore struct {
	get  func(ctx context.Context) (*domain.Manifest, error)
	save func(ctx context.Context, m *domain.Manifest) error
}

func (s stubManifestStore) Get(ctx context.Context) (*domain.Manifest, error) {
	return s.get(ctx)
}

func (s stubManifestStore) Save(ctx context.Context, m *domain.Manifest) error {
	return s.save(ctx, m)
}

type stubStrategy struct{ tag string }

func (s *stubStrategy) Run(context.Context, *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil
}

func drainBus(t *testing.T, ch <-chan ports.Event, quiet time.Duration) []ports.Event {
	t.Helper()
	var events []ports.Event
	timeout := time.NewTimer(quiet)
	defer timeout.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
			timeout.Reset(quiet)
		case <-timeout.C:
			return events
		}
	}
}

func TestAcquiring_LeaseActive_PublishesLockHeldInfoWithHolder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	holder := "some-other-host|123"
	remote := &domain.Manifest{
		LockedBy:    holder,
		HeartbeatAt: time.Now().UTC(),
	}
	remote.ApplyDefaults()
	local := &domain.Manifest{}
	local.ApplyDefaults()

	localStore := stubManifestStore{
		get:  func(context.Context) (*domain.Manifest, error) { return local, nil },
		save: func(context.Context, *domain.Manifest) error { return nil },
	}
	remoteStore := stubManifestStore{
		get:  func(context.Context) (*domain.Manifest, error) { return remote, nil },
		save: func(context.Context, *domain.Manifest) error { return nil },
	}

	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	onFail := &stubStrategy{tag: "failed"}
	onOK := &stubStrategy{tag: "run"}
	onRollback := &stubStrategy{tag: "rollback"}
	strategy := acquiring.New(localStore, remoteStore, onOK, onFail, onRollback)

	rs := &ritual.RunState{RunID: "test-run", Bus: bus}
	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "acquiring Run must not surface transport errors when lease is simply active — it routes via onFail and records rs.Err instead")
	require.NotNil(t, next, "acquiring Run must return a next strategy on lease-active (failed route), not nil")
	assert.Same(t, onFail, next, "lease-active must route to onFail so the pipeline can short-circuit to the Failed stage")
	require.Error(t, rs.Err, "acquiring must record an error on RunState describing the lease collision")
	assert.Contains(t, rs.Err.Error(), holder, "recorded RunState error must name the holder so downstream consumers can surface it")

	events := drainBus(t, ch, 50*time.Millisecond)
	var held *acquiring.LockHeldInfo
	for i := range events {
		if e, ok := events[i].(acquiring.LockHeldInfo); ok {
			held = &e
			break
		}
	}
	require.NotNil(t, held, "acquiring must publish LockHeldInfo when the remote lease is active — the GUI projection depends on this event to render the stage-locked screen with the holder identity")
	assert.Equal(t, holder, held.Holder, "LockHeldInfo.Holder must equal the remote LockedBy value verbatim so the UI can show the actual host name")
}
