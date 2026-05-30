package control_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/domain"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/gui/control"
)

func headReturning(id domain.RefID, err error) pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return id, err }
}

func TestHeadSyncProber_RemoteNewerThanLocal_ReportsBehind(t *testing.T) {
	local := headReturning("2026-05-29T10-00-00.000Z", nil)
	remote := headReturning("2026-05-29T12-00-00.000Z", nil)

	status, err := control.NewHeadSyncProber(local, remote)(t.Context())

	require.NoError(t, err)
	assert.True(t, status.Behind, "remote HEAD newer than local HEAD must read as Behind so the IDLE caption shows 'Remote is newer'")
	assert.Equal(t, "2026-05-29T10-00-00.000Z", status.LocalHead)
	assert.Equal(t, "2026-05-29T12-00-00.000Z", status.RemoteHead)
}

func TestHeadSyncProber_HeadsEqual_NotBehind(t *testing.T) {
	id := domain.RefID("2026-05-29T12-00-00.000Z")
	status, err := control.NewHeadSyncProber(headReturning(id, nil), headReturning(id, nil))(t.Context())

	require.NoError(t, err)
	assert.False(t, status.Behind, "identical heads mean up-to-date — must not report Behind")
}

func TestHeadSyncProber_LocalAheadOfRemote_NotBehind(t *testing.T) {
	local := headReturning("2026-05-29T12-00-00.000Z", nil)
	remote := headReturning("2026-05-29T10-00-00.000Z", nil)

	status, err := control.NewHeadSyncProber(local, remote)(t.Context())

	require.NoError(t, err)
	assert.False(t, status.Behind, "a local HEAD newer than remote is not 'behind' — staleness is one-directional (design-log/031 boolean)")
}

func TestHeadSyncProber_EmptyRemote_NeverBehind(t *testing.T) {
	local := headReturning("2026-05-29T10-00-00.000Z", nil)
	remote := headReturning("", pulling.ErrNoHead)

	status, err := control.NewHeadSyncProber(local, remote)(t.Context())

	require.NoError(t, err, "ErrNoHead is an empty side, not a failure")
	assert.False(t, status.Behind, "nothing is behind an empty remote")
	assert.Equal(t, "", status.RemoteHead)
}

func TestHeadSyncProber_EmptyLocalNonEmptyRemote_Behind(t *testing.T) {
	local := headReturning("", pulling.ErrNoHead)
	remote := headReturning("2026-05-29T10-00-00.000Z", nil)

	status, err := control.NewHeadSyncProber(local, remote)(t.Context())

	require.NoError(t, err)
	assert.True(t, status.Behind, "a fresh local with no refs is behind any non-empty remote")
}

func TestHeadSyncProber_RemoteListError_Propagates(t *testing.T) {
	boom := errors.New("r2: list refs/: connection refused")
	local := headReturning("2026-05-29T10-00-00.000Z", nil)
	remote := headReturning("", boom)

	_, err := control.NewHeadSyncProber(local, remote)(t.Context())

	require.Error(t, err, "a real listing failure must propagate so GetSyncStatus can degrade silently — it must not masquerade as 'up to date'")
}

func TestGetSyncStatus_NilProber_ReturnsZeroStatus(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil)
	assert.Equal(t, control.SyncStatus{}, svc.GetSyncStatus(),
		"with no prober wired the IDLE screen must get a clean zero status, not a panic")
}

func TestGetSyncStatus_ProberError_DegradesToZeroStatus(t *testing.T) {
	failing := control.SyncProber(func(_ context.Context) (control.SyncStatus, error) {
		return control.SyncStatus{Behind: true}, errors.New("offline")
	})
	svc := control.NewControlService(nil, nil, failing, nil)
	assert.Equal(t, control.SyncStatus{}, svc.GetSyncStatus(),
		"an offline prober must degrade to zero status (Behind:false) so launch never surfaces an error — design-log/031 OQ3")
}
