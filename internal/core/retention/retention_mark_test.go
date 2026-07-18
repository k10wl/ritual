package retention

import (
	"reflect"
	"ritual/internal/core/domain"
	"testing"
	"time"
)

// markKeys is unexported; tests live in-package to exercise it directly
// with a test-only parser. Keeps the engine pure and the contract explicit.

func parseTS(key string) time.Time {
	// Strip "refs/" prefix and ".json" suffix, treat the rest as a 14-char ts.
	if len(key) < len("refs/")+len("20060102150405")+len(".json") {
		return time.Time{}
	}
	stem := key[len("refs/") : len(key)-len(".json")]
	t, err := time.ParseInLocation("20060102150405", stem, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

func TestMarkKeys_EmptyList_ReturnsNothing(t *testing.T) {
	got := markKeys(nil, domain.RetentionRules{KeepLast: 5}, parseTS)
	if len(got) != 0 {
		t.Errorf("empty input: got %v, want empty", got)
	}
}

func TestMarkKeys_AllTiersZero_DropsEveryParseable(t *testing.T) {
	keys := []string{
		"refs/20260414160000.json",
		"refs/20260413160000.json",
		"refs/20260412160000.json",
	}
	got := markKeys(keys, domain.RetentionRules{}, parseTS)
	if len(got) != 3 {
		t.Errorf("got %d deletions, want 3. got=%v", len(got), got)
	}
}

func TestMarkKeys_UnparseableKeys_Skipped_NotDropped(t *testing.T) {
	keys := []string{
		"refs/20260414160000.json",
		"refs/garbage.txt",
		"refs/manual.tar",
	}
	got := markKeys(keys, domain.RetentionRules{KeepLast: 5}, parseTS)
	if len(got) != 0 {
		t.Errorf("unparseable keys must not be dropped — engine is non-destructive on unknown keys. got=%v", got)
	}
}

func TestMarkKeys_KeepLast_KeepsNewest(t *testing.T) {
	keys := []string{
		"refs/20260414160000.json",
		"refs/20260413160000.json",
		"refs/20260412160000.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepLast: 2}, parseTS)
	want := []string{"refs/20260412160000.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMarkKeys_KeepMonthly_OnePerMonth(t *testing.T) {
	keys := []string{
		"refs/20260415160000.json",
		"refs/20260414100000.json",
		"refs/20260315160000.json",
		"refs/20260314100000.json",
		"refs/20260215160000.json",
		"refs/20260214100000.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepMonthly: 3}, parseTS)
	want := []string{
		"refs/20260414100000.json",
		"refs/20260314100000.json",
		"refs/20260214100000.json",
	}
	assertSameKeys(t, got, want)
}

func TestMarkKeys_OverlappingTiers_UnionProtects(t *testing.T) {
	keys := []string{"refs/20260414160000.json"}
	got := markKeys(keys,
		domain.RetentionRules{KeepLast: 1, KeepDaily: 1, KeepWeekly: 1, KeepMonthly: 1},
		parseTS)
	if len(got) != 0 {
		t.Errorf("overlapping tiers must protect: got %v, want no deletions", got)
	}
}

func TestMarkKeys_DailyBoundary_DistinctUTCDays(t *testing.T) {
	keys := []string{
		"refs/20260414000100.json",
		"refs/20260413235900.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepDaily: 2}, parseTS)
	if len(got) != 0 {
		t.Errorf("two distinct UTC days, KeepDaily:2 must protect both. got %v", got)
	}
}

func TestMarkKeys_DailyBoundary_SameDay_KeepsNewest(t *testing.T) {
	keys := []string{
		"refs/20260414235900.json",
		"refs/20260414000100.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepDaily: 1}, parseTS)
	want := []string{"refs/20260414000100.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same UTC day: got %v, want %v (newest survives)", got, want)
	}
}

func TestMarkKeys_WeeklyBoundary_DistinctISOWeeks(t *testing.T) {
	keys := []string{
		"refs/20260105120000.json",
		"refs/20260104120000.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepWeekly: 2}, parseTS)
	if len(got) != 0 {
		t.Errorf("two ISO weeks, KeepWeekly:2 must protect both. got %v", got)
	}
}

func TestMarkKeys_WeeklyBoundary_SameISOWeek_KeepsNewest(t *testing.T) {
	keys := []string{
		"refs/20260111180000.json",
		"refs/20260105120000.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepWeekly: 1}, parseTS)
	want := []string{"refs/20260105120000.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same ISO week: got %v, want %v", got, want)
	}
}

func TestMarkKeys_MonthlyBoundary_DistinctMonths(t *testing.T) {
	keys := []string{
		"refs/20260201000100.json",
		"refs/20260131235900.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepMonthly: 2}, parseTS)
	if len(got) != 0 {
		t.Errorf("month boundary: expected no deletions, got %v", got)
	}
}

func TestMarkKeys_MonthlyBoundary_SameMonth_KeepsNewest(t *testing.T) {
	keys := []string{
		"refs/20260131180000.json",
		"refs/20260101120000.json",
	}
	got := markKeys(keys, domain.RetentionRules{KeepMonthly: 1}, parseTS)
	want := []string{"refs/20260101120000.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same month: got %v, want %v", got, want)
	}
}

func assertSameKeys(t *testing.T, got, want []string) {
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
