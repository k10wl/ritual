// Package pulling_test — HeadResolver story:
//
// NewHeadResolver lists refs/ on a ports.StorageRepository, strips the
// "refs/" prefix and ".json" suffix, and returns the lexicographically
// greatest remaining name as the RefID. The resolver is origin-agnostic
// — composition root decides whether the storage is the local FS, the
// remote, a mock, or any decorator stack thereof.
//
// First-ref bootstrap regression (POC session 2026-04-25, fix #2):
// before this resolver existed the GUI composition root inlined a copy
// that returned errors.New("no refs on remote") on empty storage. The
// pulling stage's onFail branch then blocked every fresh-remote first
// run. Sentinel ErrNoHead replaces the magic error so the stage's
// onOK short-circuit is type-level explicit and impossible to miswire.
//
// Rules for writing tests in this file (per ritual_integration_test.go):
//
//   - No comments in test bodies. Self-documenting names only.
//   - Verbose assertion messages — scenario + expectation + why.
//   - Flat AAA visible in one scroll.
//   - No table-driven tests. Each scenario is its own function.
package pulling_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/pulling"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadResolver_OnStorageWithNoRefs_ReturnsErrNoHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := newHeadResolverStorage(t)

	id, err := pulling.NewHeadResolver(storage)(ctx)

	require.Truef(t, errors.Is(err, pulling.ErrNoHead),
		"first-ref bootstrap regression (POC fix #2): NewHeadResolver against an empty refs/ listing must surface ErrNoHead so the pulling stage routes to onOK; returning a generic error blocks every empty-storage first run with `no refs on remote` — got %v", err)
	assert.Equal(t, domain.RefID(""), id,
		"empty storage must return zero-value RefID alongside ErrNoHead — callers rely on errors.Is, not the value, to detect the bootstrap case")
}

func TestHeadResolver_OnPopulatedStorage_ReturnsLexicographicMaxRefID(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := newHeadResolverStorage(t)
	seedRefKey(t, storage, "2026-04-22T10-00-00.000Z")
	seedRefKey(t, storage, "2026-04-23T11-30-00.000Z")
	seedRefKey(t, storage, "2026-04-22T10-00-00.500Z")

	id, err := pulling.NewHeadResolver(storage)(ctx)

	require.NoError(t, err,
		"populated storage must resolve cleanly — listing succeeded and at least one well-formed ref key exists, so neither ErrNoHead nor a wrapped list-failure is permitted")
	assert.Equal(t, domain.RefID("2026-04-23T11-30-00.000Z"), id,
		"NewHeadResolver must return the lexicographically greatest refs/{id}.json name; timestamps sort as strings, so the latest dash-separated UTC stamp is HEAD")
}

func newHeadResolverStorage(t *testing.T) ports.StorageRepository {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err,
		"head_resolver fixture: os.OpenRoot over t.TempDir must succeed — setup failure before any test logic runs")
	t.Cleanup(func() { _ = root.Close() })
	repo, err := adapters.NewFSRepository(root)
	require.NoError(t, err,
		"head_resolver fixture: adapters.NewFSRepository must accept the opened root — wiring failure hides test intent")
	return repo
}

func seedRefKey(t *testing.T, storage ports.StorageRepository, id string) {
	t.Helper()
	err := storage.PutStream(context.Background(), "refs/"+id+".json", bytes.NewReader([]byte("{}")))
	require.NoErrorf(t, err,
		"head_resolver fixture: seeding refs/%s.json must succeed before the resolver runs", id)
}
