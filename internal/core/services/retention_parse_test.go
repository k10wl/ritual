package services_test

import (
	"testing"
	"time"

	"ritual/internal/core/services"
)

func TestParseTimestampDir_Valid(t *testing.T) {
	got := services.ParseTimestampDir("backups/20260414160000/")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampDir = %v, want %v", got, want)
	}
}

func TestParseTimestampDir_WithoutTrailingSlash(t *testing.T) {
	got := services.ParseTimestampDir("backups/20260414160000")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampDir = %v, want %v", got, want)
	}
}

func TestParseTimestampDir_NestedKey(t *testing.T) {
	got := services.ParseTimestampDir("backups/20260414160000/worlds/world/region/r.0.0.mca")
	if got.IsZero() {
		t.Errorf("expected timestamp for nested key, got zero")
	}
}

func TestParseTimestampDir_Invalid(t *testing.T) {
	cases := []string{
		"",
		"backups/",
		"backups/not-a-timestamp/",
		"backups/abc/",
		"backups/20260414160000.tar",
	}
	for _, k := range cases {
		if got := services.ParseTimestampDir(k); !got.IsZero() {
			t.Errorf("ParseTimestampDir(%q) = %v, want zero", k, got)
		}
	}
}
