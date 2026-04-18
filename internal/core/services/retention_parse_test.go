package services_test

import (
	"ritual/internal/core/services"
	"testing"
	"time"
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

func TestParseTimestampTar_Valid(t *testing.T) {
	got := services.ParseTimestampTar("backups/20260414160000.tar")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampTar = %v, want %v", got, want)
	}
}

func TestParseTimestampTar_Invalid(t *testing.T) {
	cases := []string{
		"",
		"backups/20260414160000/",
		"backups/20260414160000",
		"backups/manual.tar",
		"backups/abc.tar",
	}
	for _, k := range cases {
		if got := services.ParseTimestampTar(k); !got.IsZero() {
			t.Errorf("ParseTimestampTar(%q) = %v, want zero", k, got)
		}
	}
}

func TestChainStrategies_FirstMatchWins(t *testing.T) {
	chain := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)

	got := chain("backups/20260414160000/")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("dir key: got %v, want %v", got, want)
	}

	got = chain("backups/20260414160000.tar")
	if !got.Equal(want) {
		t.Errorf("tar key: got %v, want %v", got, want)
	}

	if got := chain("backups/unknown"); !got.IsZero() {
		t.Errorf("unknown key: got %v, want zero", got)
	}
}

func TestChainStrategies_Empty(t *testing.T) {
	chain := services.ChainStrategies()
	if got := chain("backups/20260414160000/"); !got.IsZero() {
		t.Errorf("empty chain: got %v, want zero", got)
	}
}
