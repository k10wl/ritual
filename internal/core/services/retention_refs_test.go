package services

import (
	"testing"
	"time"
)

func TestRefsRetention_ParseTime_AcceptsJSONKey(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/20260414160000.json")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("valid refs key must parse: got %v, want %v", got, want)
	}
}

func TestRefsRetention_ParseTime_RejectsNonJSONExtension(t *testing.T) {
	got := (&refsRetention{}).parseTime("refs/20260414160000.tar")
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
