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
	// With KeepLast:1, the alphabetically-first key ("a") is protected because
	// our sort uses `a.key < b.key` returning -1 (a sorts before b in ties).
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 1}, services.ParseTimestampDir)
	want := []string{"backups/20260414160000/b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deterministic tiebreaker: got %v, want %v (alphabetical winner survives)", got, want)
	}

	// Idempotency check: same input → same output.
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

func TestMark_DailyBoundary_UTC(t *testing.T) {
	// 23:59 and 00:01 same UTC day? No — 23:59 on 13th is different day from 00:01 on 14th.
	// keep_daily:2 → both newest-per-day survive.
	keys := []string{
		"backups/20260414000100/", // Apr 14, 00:01 UTC
		"backups/20260413235900/", // Apr 13, 23:59 UTC
	}
	got := services.Mark(keys, domain.RetentionRules{KeepDaily: 2}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("two distinct UTC days, KeepDaily:2 → expected no deletions, got %v", got)
	}
}

func TestMark_DailyBoundary_SameDay(t *testing.T) {
	// Two entries same UTC day, KeepDaily:1 → only newest survives.
	keys := []string{
		"backups/20260414235900/",
		"backups/20260414000100/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepDaily: 1}, services.ParseTimestampDir)
	want := []string{"backups/20260414000100/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same UTC day: got %v, want %v", got, want)
	}
}

func TestMark_WeeklyBoundary_ISOWeek(t *testing.T) {
	// 2026-01-04 is Sunday — ISO week 1 includes Mon 2025-12-29 through Sun 2026-01-04.
	// 2026-01-05 is Monday — start of ISO week 2.
	// keep_weekly:2 → one per ISO week survives.
	keys := []string{
		"backups/20260105120000/", // ISO 2026-W02 Monday
		"backups/20260104120000/", // ISO 2026-W01 Sunday
	}
	got := services.Mark(keys, domain.RetentionRules{KeepWeekly: 2}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("ISO week boundary: expected no deletions, got %v", got)
	}
}

func TestMark_WeeklyBoundary_SameWeek(t *testing.T) {
	// Both entries in ISO 2026-W02 (Mon Jan 5 through Sun Jan 11).
	// KeepWeekly:1 → only newest per week survives.
	keys := []string{
		"backups/20260111180000/", // Sunday W02
		"backups/20260105120000/", // Monday W02
	}
	got := services.Mark(keys, domain.RetentionRules{KeepWeekly: 1}, services.ParseTimestampDir)
	want := []string{"backups/20260105120000/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same ISO week: got %v, want %v", got, want)
	}
}

func TestMark_MonthlyBoundary_UTC(t *testing.T) {
	// Jan 31 and Feb 1 are different UTC months.
	// keep_monthly:2 → both survive.
	keys := []string{
		"backups/20260201000100/", // Feb 1
		"backups/20260131235900/", // Jan 31
	}
	got := services.Mark(keys, domain.RetentionRules{KeepMonthly: 2}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("month boundary: expected no deletions, got %v", got)
	}
}

func TestMark_MonthlyBoundary_SameMonth(t *testing.T) {
	// Both in January 2026. KeepMonthly:1 → only newest survives.
	keys := []string{
		"backups/20260131180000/",
		"backups/20260101120000/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepMonthly: 1}, services.ParseTimestampDir)
	want := []string{"backups/20260101120000/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("same month: got %v, want %v", got, want)
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
