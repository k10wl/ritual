package retention_test

import (
	"context"
	"errors"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/retention"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeRules isolates config.RootPath to a temp dir and persists the given
// local/remote retention rules there, so refsRetention.Select reads them at
// prune time (design-log/039) deterministically — not the host's real settings.
func writeRules(t *testing.T, local, remote domain.RetentionRules) {
	t.Helper()
	orig := config.RootPath
	config.RootPath = t.TempDir()
	t.Cleanup(func() { config.RootPath = orig })
	s := domain.DefaultSettings()
	s.LocalRetention = local
	s.RemoteRetention = remote
	require.NoError(t, s.Save(), "seed settings.json for the prune-time read")
}

// Refs use domain.RefIDFormat ("2006-01-02T15-04-05.000Z"), not the dense
// log-filename format. Earlier fixtures used the wrong format and passed only
// because the production parseTime had the same bug — fixed 2026-06-05 per
// design-log/045 §Bug3 follow-up.
func threeRefs() *mocks.MockStorageRepository {
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return []string{
			"refs/2026-04-14T16-00-00.000Z.json",
			"refs/2026-04-13T16-00-00.000Z.json",
			"refs/2026-04-12T16-00-00.000Z.json",
		}, nil
	}
	return storage
}

func TestRefsRetention_Select_ListsRefsAndReturnsMarkedDrops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	writeRules(t, domain.RetentionRules{KeepLast: 2}, domain.RetentionRules{})
	storage := threeRefs()
	storage.ListFunc = func(_ context.Context, prefix string) ([]string, error) {
		if prefix != "refs/" {
			t.Errorf("refs retention must list the refs/ keyspace, got prefix %q", prefix)
		}
		return []string{
			"refs/2026-04-14T16-00-00.000Z.json",
			"refs/2026-04-13T16-00-00.000Z.json",
			"refs/2026-04-12T16-00-00.000Z.json",
		}, nil
	}

	r := retention.NewRefsRetention(storage, retention.ScopeLocal)
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 1 || got[0] != "refs/2026-04-12T16-00-00.000Z.json" {
		t.Errorf("KeepLast:2 must drop only the oldest; got %v", got)
	}
}

func TestRefsRetention_Select_ReadsRulesFreshEachCall_NoCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// The core 039 invariant: rules are read at prune time, never captured at
	// construction. Build the engine once, change the on-disk rules between two
	// Selects, and the second prune must reflect the new rules — proving a GUI
	// edit takes effect on the next sync without an app restart.
	writeRules(t, domain.RetentionRules{KeepLast: 2}, domain.RetentionRules{})
	storage := threeRefs()
	r := retention.NewRefsRetention(storage, retention.ScopeLocal)

	first, err := r.Select(ctx)
	require.NoError(t, err)
	require.Len(t, first, 1, "KeepLast:2 over three refs drops only the oldest")

	// Tighten the policy on disk (as the GUI's SetRetentionRules would).
	s := domain.DefaultSettings()
	s.LocalRetention = domain.RetentionRules{KeepLast: 1}
	require.NoError(t, s.Save())

	second, err := r.Select(ctx)
	require.NoError(t, err)
	require.Len(t, second, 2,
		"the SAME engine must drop two refs after KeepLast tightens to 1 — if it captured the old rules at construction it would still drop only one")
}

func TestRefsRetention_Select_ScopeRemote_ReadsRemoteRulesField(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// Local keeps everything, remote keeps one. A ScopeRemote engine must read
	// the remote field — not local — so the two sides have independent policies.
	writeRules(t, domain.RetentionRules{KeepLast: 999}, domain.RetentionRules{KeepLast: 1})
	storage := threeRefs()

	got, err := retention.NewRefsRetention(storage, retention.ScopeRemote).Select(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2, "ScopeRemote must apply RemoteRetention (KeepLast:1 ⇒ drop two), not LocalRetention (KeepLast:999 ⇒ drop none)")
}

func TestRefsRetention_Select_ZeroRules_FallsBackToDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// An unconfigured (zero-value) rule set must fall back to defaults
	// (KeepLast:2), preserving pre-039 behaviour rather than deleting everything.
	writeRules(t, domain.RetentionRules{}, domain.RetentionRules{})
	storage := threeRefs()

	got, err := retention.NewRefsRetention(storage, retention.ScopeLocal).Select(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1, "zero-value local rules must default to KeepLast:2 (drop only the oldest), not prune everything")
}

func TestRefsRetention_Select_ListError_Propagates(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	writeRules(t, domain.RetentionRules{KeepLast: 2}, domain.RetentionRules{})
	boom := errors.New("list boom")
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return nil, boom
	}

	r := retention.NewRefsRetention(storage, retention.ScopeLocal)
	_, err := r.Select(ctx)

	if !errors.Is(err, boom) {
		t.Errorf("List error must rise to Select caller; got %v", err)
	}
}

func TestRefsRetention_Select_EmptyList_NoDrops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	writeRules(t, domain.RetentionRules{KeepLast: 2}, domain.RetentionRules{})
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) { return nil, nil }

	r := retention.NewRefsRetention(storage, retention.ScopeLocal)
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("empty list must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty input must yield no drops; got %v", got)
	}
}

func TestLogsRetention_Select_ListsLogsAndTrimsByKeepLast(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, prefix string) ([]string, error) {
		if prefix != "logs" {
			t.Errorf("logs retention must list the logs keyspace, got prefix %q", prefix)
		}
		return []string{
			"logs/20260414160000.log",
			"logs/20260413160000.log",
			"logs/20260412160000.log",
			"logs/20260411160000.log",
		}, nil
	}

	r := retention.NewLogsRetention(storage, domain.RetentionRules{KeepLast: 2})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("KeepLast:2 across 4 logs must drop 2; got %v", got)
	}
}

func TestLogsRetention_Select_UnknownFile_IsPreserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return []string{
			"logs/20260414160000.log",
			"logs/latest.log",
		}, nil
	}

	r := retention.NewLogsRetention(storage, domain.RetentionRules{KeepLast: 1})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("non-timestamp filename must be left alone; got %v", got)
	}
}
