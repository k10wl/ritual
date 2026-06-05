package retention

import (
	"ritual/internal/core/domain"
	"testing"
	"time"
)

// Real refs use domain.RefIDFormat ("2006-01-02T15-04-05.000Z"), not the
// dense log-filename format. Pre-2026-06-05 these tests used a format the
// rest of the system never produces, so they passed while production parses
// silently failed for every real ref key (see design-log/045 §Bug3 follow-up).
func TestRefsRetention_ParseTime_AcceptsJSONKey(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/2026-04-14T16-00-00.000Z.json")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("valid refs key must parse: got %v, want %v", got, want)
	}
}

func TestRefsRetention_ParseTime_RejectsNonJSONExtension(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/2026-04-14T16-00-00.000Z.tar")
	if !got.IsZero() {
		t.Errorf("non-json extension must be zero-time (skipped by mark); got %v", got)
	}
}

func TestRefsRetention_ParseTime_RejectsMalformedTimestamp(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/garbage.json")
	if !got.IsZero() {
		t.Errorf("malformed stem must be zero-time; got %v", got)
	}
}

func TestRefsRetention_ParseTime_RejectsEmptyKey(t *testing.T) {
	got := (&refsRetention{}).parseTime("")
	if !got.IsZero() {
		t.Errorf("empty key must be zero-time; got %v", got)
	}
}

// TestRefsRetention_ParseTime_RejectsLogFilenameFormat pins the bug fix: the
// legacy log-filename format must NOT parse as a refs timestamp. If this
// reverts to accepting both, the format-mismatch class of bug re-opens.
func TestRefsRetention_ParseTime_RejectsLogFilenameFormat(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/20260414160000.json")
	if !got.IsZero() {
		t.Errorf("log-filename format must not parse as a ref id; got %v", got)
	}
}

// TestMarkKeys_ActuallySelectsForDeletion is the missing end-to-end regression:
// with real-format ref keys (the only kind the system ever writes) and a
// tightening rule, markKeys must return keys to delete. Pre-fix this returned
// 0 (parseTime failed silently on every key — design-log/045 §Bug3).
func TestMarkKeys_ActuallySelectsForDeletion(t *testing.T) {
	r := &refsRetention{}
	keys := []string{
		"refs/2026-06-05T13-50-35.705Z.json",
		"refs/2026-06-05T12-48-30.393Z.json",
		"refs/2026-06-05T12-34-04.495Z.json",
		"refs/2026-05-25T20-21-00.838Z.json",
		"refs/2026-05-13T16-27-21.913Z.json",
	}
	// KeepLast=2 → protect the two newest, drop the rest.
	got := markKeys(keys, domain.RetentionRules{KeepLast: 2}, r.parseTime)
	if len(got) != 3 {
		t.Fatalf("KeepLast=2 over 5 refs should drop 3, got %d (%v)", len(got), got)
	}
}

func TestLogsRetention_ParseTime_AcceptsLogKey(t *testing.T) {
	got := (&logsRetention{}).parseTime("logs/20260414160000.log")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("valid logs key must parse: got %v, want %v", got, want)
	}
}

func TestLogsRetention_ParseTime_RejectsNonLogExtension(t *testing.T) {
	got := (&logsRetention{}).parseTime("logs/20260414160000.json")
	if !got.IsZero() {
		t.Errorf("non-log extension must be zero-time; got %v", got)
	}
}

func TestLogsRetention_ParseTime_RejectsNonTimestampStem(t *testing.T) {
	got := (&logsRetention{}).parseTime("logs/latest.log")
	if !got.IsZero() {
		t.Errorf("non-timestamp stem must be zero-time; got %v", got)
	}
}
