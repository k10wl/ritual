package control_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/gui/control"
)

// fakeScope builds a VersionScope from an in-memory set of refs keyed by id.
func fakeScope(refs map[domain.RefID]*domain.Ref, listErr error) control.VersionScope {
	return control.VersionScope{
		List: func(_ context.Context, _ string) ([]string, error) {
			if listErr != nil {
				return nil, listErr
			}
			keys := make([]string, 0, len(refs))
			for id := range refs {
				keys = append(keys, "refs/"+string(id)+".json")
			}
			return keys, nil
		},
		ReadRef: func(_ context.Context, id domain.RefID) (*domain.Ref, error) {
			r, ok := refs[id]
			if !ok {
				return nil, errors.New("not found")
			}
			return r, nil
		},
	}
}

func ref(parent domain.RefID, objects map[string]domain.Object) *domain.Ref {
	return &domain.Ref{Parent: parent, Objects: objects}
}

func TestVersionLister_NewestFirst_FlagsHeadAndSumsMetadata(t *testing.T) {
	local := map[domain.RefID]*domain.Ref{
		"2026-03-01T08-00-00.000Z": ref("", map[string]domain.Object{"a": {Hash: "h1", Size: 100}}),
		"2026-04-15T10-00-00.000Z": ref("2026-03-01T08-00-00.000Z", map[string]domain.Object{"a": {Size: 100}, "b": {Size: 200}}),
		"2026-05-20T09-30-00.000Z": ref("2026-04-15T10-00-00.000Z", map[string]domain.Object{"a": {Size: 100}, "b": {Size: 200}, "c": {Size: 50}}),
	}
	lister := control.NewVersionLister(fakeScope(local, nil), control.VersionScope{}, nil)

	vs, err := lister(t.Context(), "local")
	require.NoError(t, err)
	require.Len(t, vs, 3)

	assert.Equal(t, "2026-05-20T09-30-00.000Z", vs[0].ID, "versions must be newest-first")
	assert.True(t, vs[0].IsHead, "the newest ref is HEAD")
	assert.False(t, vs[1].IsHead)
	assert.Equal(t, 3, vs[0].Files, "Files must be len(Objects)")
	assert.Equal(t, int64(350), vs[0].SizeBytes, "SizeBytes must sum Object.Size")
	assert.Equal(t, "2026-04-15T10-00-00.000Z", vs[0].Parent)
	assert.Equal(t, "local", vs[0].Source)
	assert.Greater(t, vs[0].UnixMs, int64(0), "UnixMs must be the parsed RefID timestamp")
}

func TestVersionLister_FlagsIsLoadedFromSettingsHook(t *testing.T) {
	local := map[domain.RefID]*domain.Ref{
		"2026-03-01T08-00-00.000Z": ref("", map[string]domain.Object{"a": {Size: 1}}),
		"2026-04-15T10-00-00.000Z": ref("2026-03-01T08-00-00.000Z", map[string]domain.Object{"a": {Size: 1}}),
		"2026-05-20T09-30-00.000Z": ref("2026-04-15T10-00-00.000Z", map[string]domain.Object{"a": {Size: 1}}),
	}
	// LoadedRefID points at an older ref — the workdir reflects it after a
	// Restore (design-log/044). IsLoaded must sit on that row, IsHead stays
	// on the newest.
	loaded := domain.RefID("2026-04-15T10-00-00.000Z")
	lister := control.NewVersionLister(
		fakeScope(local, nil),
		control.VersionScope{},
		func() domain.RefID { return loaded },
	)
	vs, err := lister(t.Context(), "local")
	require.NoError(t, err)
	require.Len(t, vs, 3)
	assert.True(t, vs[0].IsHead, "newest is HEAD")
	assert.False(t, vs[0].IsLoaded, "HEAD is not what the workdir reflects after a Restore")
	assert.True(t, vs[1].IsLoaded, "the restored ref is what the workdir reflects")
	assert.False(t, vs[1].IsHead)
}

func TestVersionLister_EmptyLoadedID_FallsBackToHead(t *testing.T) {
	local := map[domain.RefID]*domain.Ref{
		"2026-03-01T08-00-00.000Z": ref("", map[string]domain.Object{"a": {Size: 1}}),
		"2026-05-20T09-30-00.000Z": ref("2026-03-01T08-00-00.000Z", map[string]domain.Object{"a": {Size: 1}}),
	}
	// Empty LoadedRefID (never-restored fresh install): IsLoaded falls back to
	// IsHead so the "current" badge is never silent on a clean store.
	lister := control.NewVersionLister(
		fakeScope(local, nil),
		control.VersionScope{},
		func() domain.RefID { return "" },
	)
	vs, err := lister(t.Context(), "local")
	require.NoError(t, err)
	require.Len(t, vs, 2)
	assert.True(t, vs[0].IsLoaded, "fresh install: IsLoaded follows IsHead")
	assert.False(t, vs[1].IsLoaded)
}

func TestVersionLister_RemoteListError_DegradesToLocal(t *testing.T) {
	local := map[domain.RefID]*domain.Ref{
		"2026-05-20T09-30-00.000Z": ref("", map[string]domain.Object{"a": {Size: 1}}),
	}
	lister := control.NewVersionLister(
		fakeScope(local, nil),
		fakeScope(nil, errors.New("r2 offline")), // remote List fails
		nil,
	)

	vs, err := lister(t.Context(), "remote")
	require.NoError(t, err, "a remote failure must degrade to local, not error out")
	require.Len(t, vs, 1)
	assert.Equal(t, "local", vs[0].Source, "degraded result must be labelled local so the UI tells the truth about which store it read")
}

func TestVersionLister_EmptyStore_ReturnsNil(t *testing.T) {
	lister := control.NewVersionLister(fakeScope(map[domain.RefID]*domain.Ref{}, nil), control.VersionScope{}, nil)
	vs, err := lister(t.Context(), "local")
	require.NoError(t, err)
	assert.Empty(t, vs)
}

func TestVersionLister_SkipsNonRefKeys(t *testing.T) {
	scope := control.VersionScope{
		List: func(_ context.Context, _ string) ([]string, error) {
			return []string{
				"refs/2026-05-20T09-30-00.000Z.json", // valid
				"refs/",                               // directory marker
				"refs/notatimestamp.json",             // unparseable
				"objects/deadbeef",                    // wrong keyspace
			}, nil
		},
		ReadRef: func(_ context.Context, _ domain.RefID) (*domain.Ref, error) {
			return ref("", map[string]domain.Object{"a": {Size: 1}}), nil
		},
	}
	vs, err := control.NewVersionLister(scope, control.VersionScope{}, nil)(t.Context(), "local")
	require.NoError(t, err)
	require.Len(t, vs, 1, "only the one well-formed ref key must survive parsing")
	assert.Equal(t, "2026-05-20T09-30-00.000Z", vs[0].ID)
}

func TestListVersions_NilLister_ReturnsNil(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	assert.Nil(t, svc.ListVersions("remote"), "no lister wired ⇒ empty list, never a panic")
}

func TestRestore_ValidID_PublishesRestoreRequested(t *testing.T) {
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)

	require.NoError(t, svc.Restore("2026-05-20T09-30-00.000Z"))

	select {
	case e := <-ch:
		rr, ok := e.(ritual.RestoreRequested)
		require.True(t, ok, "Restore must publish a RestoreRequested command")
		assert.Equal(t, domain.RefID("2026-05-20T09-30-00.000Z"), rr.RefID)
	case <-time.After(time.Second):
		t.Fatal("Restore did not publish RestoreRequested within 1s")
	}
}

func TestRestore_RejectsEmptyAndMalformedID(t *testing.T) {
	bus := adapters.NewEventBus(16)
	svc := control.NewControlService(bus, nil, nil, nil, nil, nil)

	require.Error(t, svc.Restore(""), "empty id must be rejected before any publish")
	require.Error(t, svc.Restore("not-a-timestamp"), "a malformed id must never reach the FSM")
}

var _ ports.EventBus = adapters.NewEventBus(1)
