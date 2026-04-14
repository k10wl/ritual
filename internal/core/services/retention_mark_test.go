package services_test

import (
	"reflect"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestMark_EmptyList(t *testing.T) {
	got := services.Mark(nil, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("empty input: got %v, want empty", got)
	}
}

func TestMark_AllTiersZero_DeletesAll(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000/",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{}, services.ParseTimestampDir)
	if len(got) != 3 {
		t.Errorf("got %d deletions, want 3. got=%v", len(got), got)
	}
}

func TestMark_UnparseableKeysDeleted(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/garbage.txt",
		"backups/manual.tar",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if !reflect.DeepEqual(got, []string{"backups/garbage.txt", "backups/manual.tar"}) {
		t.Errorf("got %v, want unparseables only", got)
	}
}

func TestMark_KeepLast_KeepsNewest(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000/",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 2}, services.ParseTimestampDir)
	want := []string{"backups/20260412160000/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMark_KeepMonthly_OnePerMonth(t *testing.T) {
	// 3 months, 2 entries per month — monthly:3 should keep the newest per month
	keys := []string{
		"backups/20260415160000/", // April - newer
		"backups/20260414100000/", // April - older
		"backups/20260315160000/", // March - newer
		"backups/20260314100000/", // March - older
		"backups/20260215160000/", // Feb - newer
		"backups/20260214100000/", // Feb - older
	}
	got := services.Mark(keys, domain.RetentionRules{KeepMonthly: 3}, services.ParseTimestampDir)
	want := []string{
		"backups/20260414100000/",
		"backups/20260314100000/",
		"backups/20260214100000/",
	}
	slicesEqual(t, got, want)
}

func TestMark_OverlappingTiers_Union(t *testing.T) {
	keys := []string{"backups/20260414160000/"}
	got := services.Mark(keys,
		domain.RetentionRules{KeepLast: 1, KeepDaily: 1, KeepWeekly: 1, KeepMonthly: 1},
		services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("got %v, want no deletions (overlap protects)", got)
	}
}

func TestMark_SameSecondTimestamps_DeterministicOrder(t *testing.T) {
	keys := []string{
		"backups/20260414160000/b",
		"backups/20260414160000/a",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 1}, services.ParseTimestampDir)
	got2 := services.Mark(keys, domain.RetentionRules{KeepLast: 1}, services.ParseTimestampDir)
	if !reflect.DeepEqual(got, got2) {
		t.Errorf("non-deterministic: %v vs %v", got, got2)
	}
}

func TestMark_MixedFormats_BothParsed(t *testing.T) {
	chain := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000.tar",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 2}, chain)
	if len(got) != 1 || got[0] != "backups/20260412160000/" {
		t.Errorf("got %v, want [backups/20260412160000/]", got)
	}
}

// slicesEqual compares two string slices regardless of order
func slicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("len(got)=%d, len(want)=%d. got=%v, want=%v", len(got), len(want), got, want)
		return
	}
	gotMap := map[string]bool{}
	for _, g := range got {
		gotMap[g] = true
	}
	for _, w := range want {
		if !gotMap[w] {
			t.Errorf("missing %q in got=%v", w, got)
		}
	}
}
