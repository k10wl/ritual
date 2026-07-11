package lock_test

import (
	"context"
	"errors"
	"ritual/internal/core/lock"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSide struct {
	name         string
	acquireSID   string
	acquireErr   error
	inspect      *lock.Holder
	inspectErr   error
	releaseErr   error
	heartbeatErr error

	acquireCalls   int
	releaseCalls   int
	inspectCalls   int
	heartbeatCalls int
	lastReleaseSID string
	lastHeartSID   string
}

func (f *fakeSide) Acquire(_ context.Context) (string, error) {
	f.acquireCalls++
	return f.acquireSID, f.acquireErr
}

func (f *fakeSide) Release(_ context.Context, sid string) error {
	f.releaseCalls++
	f.lastReleaseSID = sid
	return f.releaseErr
}

func (f *fakeSide) Inspect(_ context.Context) (*lock.Holder, error) {
	f.inspectCalls++
	return f.inspect, f.inspectErr
}

func (f *fakeSide) Heartbeat(_ context.Context, sid string) error {
	f.heartbeatCalls++
	f.lastHeartSID = sid
	return f.heartbeatErr
}

func TestBoth_Acquire_LocalLocked_ShortCircuitsBeforeTouchingRemote(t *testing.T) {
	local := &fakeSide{acquireErr: lock.ErrLocked}
	remote := &fakeSide{acquireSID: "remote-sid"}
	b := lock.NewBoth(local, remote)

	_, err := b.Acquire(context.Background())

	assert.ErrorIs(t, err, lock.ErrLocked, "Acquire must surface ErrLocked from the local side so the user sees the friendly 'another instance is running on this machine' screen instead of a generic acquire failure")
	assert.Equal(t, 1, local.acquireCalls, "local Acquire must run exactly once before short-circuiting — that's the whole point of the fix, save a network round-trip when the local lock is the blocker")
	assert.Equal(t, 0, remote.acquireCalls, "remote Acquire must not run when the local side is held — a stranded local PID must not pin a remote lease that no live process can release")
}

func TestBoth_Acquire_RemoteLockedAfterLocalAcquired_RollsBackLocal(t *testing.T) {
	local := &fakeSide{acquireSID: "local-sid"}
	remote := &fakeSide{acquireErr: lock.ErrLocked}
	b := lock.NewBoth(local, remote)

	_, err := b.Acquire(context.Background())

	assert.ErrorIs(t, err, lock.ErrLocked, "Acquire must surface ErrLocked from the remote side so the user sees the cross-machine holder screen with their teammate's hostname")
	assert.Equal(t, 1, local.releaseCalls, "local Release must run exactly once to roll back the lease this run took before the remote denial — leaving local held would block subsequent runs on this machine for no reason")
	assert.Equal(t, "local-sid", local.lastReleaseSID, "rollback must use the SID local Acquire just minted — releasing under a wrong SID would no-op against a foreign holder, leaving the file orphan")
}

func TestBoth_Release_SplitsCompositeSessionID_AndCallsBothSides(t *testing.T) {
	local := &fakeSide{acquireSID: "L"}
	remote := &fakeSide{acquireSID: "R"}
	b := lock.NewBoth(local, remote)

	sid, err := b.Acquire(context.Background())
	require.NoError(t, err, "Acquire must succeed when both sides are free so we can exercise Release")
	require.NoError(t, b.Release(context.Background(), sid), "Release must succeed when both inner Releases succeed — Release errors only on inner failures")

	assert.Equal(t, "L", local.lastReleaseSID, "local Release must receive the local SID extracted from the composite — feeding the wrong half would silently no-op against a foreign owner, leaking the lease")
	assert.Equal(t, "R", remote.lastReleaseSID, "remote Release must receive the remote SID extracted from the composite — feeding the wrong half would silently no-op against a foreign owner, leaking the lease")
}

func TestBoth_Heartbeat_SplitsCompositeSessionID_AndCallsBothSides(t *testing.T) {
	local := &fakeSide{acquireSID: "L"}
	remote := &fakeSide{acquireSID: "R"}
	b := lock.NewBoth(local, remote)

	sid, err := b.Acquire(context.Background())
	require.NoError(t, err, "Acquire must succeed when both sides are free so we can exercise Heartbeat")
	require.NoError(t, b.Heartbeat(context.Background(), sid), "Heartbeat must succeed when both inner Heartbeats succeed — both lease files must be refreshed every interval or the TTL expires")

	assert.Equal(t, "L", local.lastHeartSID, "local Heartbeat must receive the local SID extracted from the composite — heartbeating under the wrong SID would surface ErrLeaseTakenOver and panic the run")
	assert.Equal(t, "R", remote.lastHeartSID, "remote Heartbeat must receive the remote SID extracted from the composite — heartbeating under the wrong SID would surface ErrLeaseTakenOver and panic the run")
}

func TestBoth_Inspect_LocalHeld_ReturnsLocalHolderAndSkipsRemoteProbe(t *testing.T) {
	local := &fakeSide{inspect: &lock.Holder{Owner: "local-host"}}
	remote := &fakeSide{inspect: &lock.Holder{Owner: "remote-host"}}
	b := lock.NewBoth(local, remote)

	h, err := b.Inspect(context.Background())

	require.NoError(t, err, "Inspect must propagate no error when the local side answers cleanly")
	require.NotNil(t, h, "Inspect must return the holder snapshot so the GUI can render the locked screen")
	assert.Equal(t, "local-host", h.Owner, "local-side holder wins because it's the side that blocked Acquire — surfacing the remote holder when the local one is the actual blocker would mislead the user")
	assert.Equal(t, 0, remote.inspectCalls, "remote Inspect must not run when the local side reports a holder — saves a network round-trip on the rejection path")
}

func TestBoth_Inspect_LocalFree_FallsThroughToRemote(t *testing.T) {
	local := &fakeSide{inspect: nil}
	remote := &fakeSide{inspect: &lock.Holder{Owner: "remote-host"}}
	b := lock.NewBoth(local, remote)

	h, err := b.Inspect(context.Background())

	require.NoError(t, err, "Inspect must propagate no error when local is free and remote answers cleanly")
	require.NotNil(t, h, "Inspect must return the remote holder when the local slot is empty — that's the cross-machine 'someone else is playing' case")
	assert.Equal(t, "remote-host", h.Owner, "remote-side holder must surface when local is free so the user sees the actual blocker's hostname instead of a confusing nil-holder screen")
}

func TestBoth_Release_UnknownSID_NoOpsBothSides(t *testing.T) {
	local := &fakeSide{releaseErr: errors.New("must not be called")}
	remote := &fakeSide{releaseErr: errors.New("must not be called")}
	b := lock.NewBoth(local, remote)

	require.NoError(t, b.Release(context.Background(), ""), "an empty composite SID must be a silent no-op rather than an error — the unlocking stage runs on every termination path including ones where Acquire never succeeded")
	assert.Equal(t, 0, local.releaseCalls, "local Release must be skipped on an empty composite SID — calling with empty would surface 'foreign owner' inside the inner Locker and leak a stale file")
	assert.Equal(t, 0, remote.releaseCalls, "remote Release must be skipped on an empty composite SID — calling with empty would surface 'foreign owner' inside the inner Locker and leak a stale file")
}
