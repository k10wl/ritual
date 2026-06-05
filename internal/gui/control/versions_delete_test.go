package control_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/gui/control"
)

// Backend contract for the 045 post-ship remote-delete extension (user
// direction 2026-06-05: "allow me to delete anything"). DeleteRemoteVersion
// mirrors DeleteLocalVersion's validation + wiring semantics on the remote
// store, intentionally does NOT touch LoadedRefID (workdir is local-only),
// and does NOT invalidate the local stats cache.

func TestDeleteRemoteVersion_RejectsEmptyAndMalformedID(t *testing.T) {
	bus := adapters.NewEventBus(16)
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)
	svc.SetRemoteVersionDeleter(func(_ context.Context, _ domain.RefID) error {
		t.Fatal("deleter must not be called on an invalid id")
		return nil
	})

	require.Error(t, svc.DeleteRemoteVersion(""), "empty id must be rejected before any storage touch")
	require.Error(t, svc.DeleteRemoteVersion("not-a-timestamp"), "a malformed id must never reach storage")
}

func TestDeleteRemoteVersion_NoDeleterWired_ReturnsExplicitError(t *testing.T) {
	bus := adapters.NewEventBus(16)
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)
	// Intentionally do NOT call SetRemoteVersionDeleter — the method must
	// fail loud rather than silently no-op (mirrors DeleteLocalVersion).
	err := svc.DeleteRemoteVersion("2026-05-20T09-30-00.000Z")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not wired")
}

func TestDeleteRemoteVersion_ValidID_InvokesRemoteDeleter(t *testing.T) {
	bus := adapters.NewEventBus(16)
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)
	var seen domain.RefID
	svc.SetRemoteVersionDeleter(func(_ context.Context, id domain.RefID) error {
		seen = id
		return nil
	})

	require.NoError(t, svc.DeleteRemoteVersion("2026-05-20T09-30-00.000Z"))
	assert.Equal(t, domain.RefID("2026-05-20T09-30-00.000Z"), seen)
}

func TestDeleteRemoteVersion_DeleterError_Wrapped(t *testing.T) {
	bus := adapters.NewEventBus(16)
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)
	svc.SetRemoteVersionDeleter(func(_ context.Context, _ domain.RefID) error {
		return errors.New("r2 offline")
	})

	err := svc.DeleteRemoteVersion("2026-05-20T09-30-00.000Z")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "r2 offline", "underlying error must surface so the UI can show it")
}
