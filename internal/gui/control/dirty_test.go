package control_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/domain"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/gui/control"
)

const dirtyValidHead = domain.RefID("2026-05-29T10-00-00.000Z")

// headResolving fixes the local HEAD a dirty probe sees.
func headResolving(id domain.RefID, err error) pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return id, err }
}

// refWith serves a single ref carrying the given Objects under "world".
func refWith(objects map[string]domain.Object) control.RefReader {
	return func(_ context.Context, id domain.RefID) (*domain.Ref, error) {
		return &domain.Ref{Timestamp: id, Targets: []string{"world"}, Objects: objects}, nil
	}
}

// scanReturning serves a fixed workdir scan result.
func scanReturning(entries map[string]domain.FileEntry, err error) control.WorkdirScan {
	return func(_ context.Context, _ time.Time, _ map[string]domain.FileEntry, _ []string) (map[string]domain.FileEntry, error) {
		return entries, nil
	}
}

func TestLocalDirtyProber_HeadPresent(t *testing.T) {
	tests := []struct {
		name    string
		objects map[string]domain.Object
		scanned map[string]domain.FileEntry
		want    bool
	}{
		{
			name:    "clean — workdir matches ref",
			objects: map[string]domain.Object{"world/a": {Hash: "h1"}},
			scanned: map[string]domain.FileEntry{"world/a": {Hash: "h1"}},
			want:    false,
		},
		{
			name:    "edited — hash changed",
			objects: map[string]domain.Object{"world/a": {Hash: "h1"}},
			scanned: map[string]domain.FileEntry{"world/a": {Hash: "h2"}},
			want:    true,
		},
		{
			name:    "added file — new path on disk",
			objects: map[string]domain.Object{"world/a": {Hash: "h1"}},
			scanned: map[string]domain.FileEntry{"world/a": {Hash: "h1"}, "world/b": {Hash: "h9"}},
			want:    true,
		},
		{
			name:    "removed file — path gone from disk",
			objects: map[string]domain.Object{"world/a": {Hash: "h1"}, "world/b": {Hash: "h2"}},
			scanned: map[string]domain.FileEntry{"world/a": {Hash: "h1"}},
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prober := control.NewLocalDirtyProber(
				headResolving(dirtyValidHead, nil),
				refWith(tc.objects),
				scanReturning(tc.scanned, nil),
				nil,
			)
			dirty, err := prober(t.Context())

			require.NoError(t, err)
			assert.Equal(t, tc.want, dirty)
		})
	}
}

func TestLocalDirtyProber_NoHead_FilesOnDisk_IsDirty(t *testing.T) {
	readRef := func(_ context.Context, _ domain.RefID) (*domain.Ref, error) {
		t.Fatal("readRef must not be called when there is no local HEAD — the seed path scans disk directly")
		return nil, nil
	}
	prober := control.NewLocalDirtyProber(
		headResolving("", pulling.ErrNoHead),
		readRef,
		scanReturning(map[string]domain.FileEntry{"world/a": {Hash: "h1"}}, nil),
		[]string{"world"},
	)
	dirty, err := prober(t.Context())

	require.NoError(t, err)
	assert.True(t, dirty, "no ref yet but in-scope files on disk must read as dirty — the seed case (design-log/035 §Q5)")
}

func TestLocalDirtyProber_NoHead_EmptyDisk_IsClean(t *testing.T) {
	readRef := func(_ context.Context, _ domain.RefID) (*domain.Ref, error) {
		t.Fatal("readRef must not be called when there is no local HEAD")
		return nil, nil
	}
	prober := control.NewLocalDirtyProber(
		headResolving("", pulling.ErrNoHead),
		readRef,
		scanReturning(map[string]domain.FileEntry{}, nil),
		[]string{"world"},
	)
	dirty, err := prober(t.Context())

	require.NoError(t, err)
	assert.False(t, dirty, "no ref and nothing on disk is a fresh-clean workdir — not dirty")
}

func TestLocalDirtyProber_ReadRefError_Surfaces(t *testing.T) {
	boom := errors.New("read ref: not found")
	readRef := func(_ context.Context, _ domain.RefID) (*domain.Ref, error) { return nil, boom }
	scan := scanReturning(map[string]domain.FileEntry{"world/a": {Hash: "h1"}}, nil)

	prober := control.NewLocalDirtyProber(headResolving(dirtyValidHead, nil), readRef, scan, nil)
	dirty, err := prober(t.Context())

	require.Error(t, err, "a ref-read failure must surface from the prober (GetSyncStatus swallows it separately)")
	assert.False(t, dirty, "an errored probe must not claim dirty")
}

func TestLocalDirtyProber_ScanError_Surfaces(t *testing.T) {
	boom := errors.New("scan: permission denied")
	scan := func(_ context.Context, _ time.Time, _ map[string]domain.FileEntry, _ []string) (map[string]domain.FileEntry, error) {
		return nil, boom
	}
	ref := refWith(map[string]domain.Object{"world/a": {Hash: "h1"}})

	prober := control.NewLocalDirtyProber(headResolving(dirtyValidHead, nil), ref, scan, nil)
	dirty, err := prober(t.Context())

	require.Error(t, err, "a scan failure must surface from the prober")
	assert.False(t, dirty, "an errored probe must not claim dirty")
}

// dirtyReturning fixes the dirty verdict GetSyncStatus merges.
func dirtyReturning(dirty bool, err error) control.LocalDirtyProber {
	return func(_ context.Context) (bool, error) { return dirty, err }
}

// syncReturning fixes the head-compare half GetSyncStatus merges.
func syncReturning(s control.SyncStatus, err error) control.SyncProber {
	return func(_ context.Context) (control.SyncStatus, error) { return s, err }
}

func TestGetSyncStatus_DirtySurfaces(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, dirtyReturning(true, nil), nil, nil)
	assert.True(t, svc.GetSyncStatus().Dirty,
		"a dirty workdir must surface as SyncStatus.Dirty even with no head-compare prober wired")
}

func TestGetSyncStatus_DirtyError_Degrades(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, dirtyReturning(false, errors.New("scan boom")), nil, nil)
	assert.False(t, svc.GetSyncStatus().Dirty,
		"a dirty-probe error must degrade Dirty to false, never surface — design-log/035 §L1")
}

func TestGetSyncStatus_HeadCompareAndDirty_Merge(t *testing.T) {
	sync := syncReturning(control.SyncStatus{Behind: true, Unpushed: false, LocalHead: "x"}, nil)
	svc := control.NewControlService(nil, nil, sync, dirtyReturning(true, nil), nil, nil)

	got := svc.GetSyncStatus()

	assert.True(t, got.Behind, "the head-compare half must pass through")
	assert.False(t, got.Unpushed, "the head-compare half must pass through")
	assert.Equal(t, "x", got.LocalHead, "the head-compare half must pass through")
	assert.True(t, got.Dirty, "the dirty half must merge on top of the head-compare verdict")
}
