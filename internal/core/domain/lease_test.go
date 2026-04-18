package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDurationJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"1m", time.Minute, `"1m0s"`},
		{"10m", 10 * time.Minute, `"10m0s"`},
		{"1h30m", 90 * time.Minute, `"1h30m0s"`},
		{"zero", 0, `"0s"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Duration(tc.in)
			data, err := json.Marshal(d)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Fatalf("marshal got %s want %s", data, tc.want)
			}
			var back Duration
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back != d {
				t.Fatalf("roundtrip got %v want %v", back, d)
			}
		})
	}
}

func TestDurationUnmarshalEmpty(t *testing.T) {
	d := Duration(time.Hour)
	if err := json.Unmarshal([]byte(`""`), &d); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if d != 0 {
		t.Fatalf("empty should decode to 0, got %v", d)
	}
}

func TestDurationUnmarshalInvalid(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"not-a-duration"`), &d); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestLeaseSettingsValidate(t *testing.T) {
	cases := []struct {
		name string
		ls   LeaseSettings
		want error
	}{
		{
			name: "defaults valid (1m/10m)",
			ls:   LeaseSettings{HeartbeatInterval: Duration(time.Minute), TTL: Duration(10 * time.Minute)},
			want: nil,
		},
		{
			name: "interval 30s exact boundary",
			ls:   LeaseSettings{HeartbeatInterval: Duration(30 * time.Second), TTL: Duration(90 * time.Second)},
			want: nil,
		},
		{
			name: "interval too small",
			ls:   LeaseSettings{HeartbeatInterval: Duration(29 * time.Second), TTL: Duration(10 * time.Minute)},
			want: ErrLeaseIntervalTooSmall,
		},
		{
			name: "ttl less than 3x interval",
			ls:   LeaseSettings{HeartbeatInterval: Duration(time.Minute), TTL: Duration(2 * time.Minute)},
			want: ErrLeaseTTLTooSmall,
		},
		{
			name: "ttl exceeds 24h",
			ls:   LeaseSettings{HeartbeatInterval: Duration(time.Minute), TTL: Duration(25 * time.Hour)},
			want: ErrLeaseTTLTooLarge,
		},
		{
			name: "ttl exactly 24h",
			ls:   LeaseSettings{HeartbeatInterval: Duration(time.Minute), TTL: Duration(24 * time.Hour)},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.ls.Validate()
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestManifestApplyDefaultsAppliesLease(t *testing.T) {
	var m Manifest
	m.ApplyDefaults()
	if m.Lease.HeartbeatInterval == 0 {
		t.Fatal("HeartbeatInterval not defaulted")
	}
	if m.Lease.TTL == 0 {
		t.Fatal("TTL not defaulted")
	}
	if err := m.Lease.Validate(); err != nil {
		t.Fatalf("defaulted lease invalid: %v", err)
	}
}

func TestManifestApplyDefaultsPreservesExistingLease(t *testing.T) {
	m := Manifest{
		Lease: LeaseSettings{
			HeartbeatInterval: Duration(2 * time.Minute),
			TTL:               Duration(30 * time.Minute),
		},
	}
	m.ApplyDefaults()
	if m.Lease.HeartbeatInterval != Duration(2*time.Minute) {
		t.Fatalf("interval overwritten: %v", m.Lease.HeartbeatInterval)
	}
	if m.Lease.TTL != Duration(30*time.Minute) {
		t.Fatalf("ttl overwritten: %v", m.Lease.TTL)
	}
}

func TestManifestUnmarshalAppliesLeaseDefaults(t *testing.T) {
	// Manifest with no lease block — decode should fill defaults.
	raw := `{"manifest_version":"2.0.0"}`
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := m.Lease.Validate(); err != nil {
		t.Fatalf("defaulted lease invalid: %v", err)
	}
}

func TestManifestUnmarshalPreservesLeaseOverride(t *testing.T) {
	raw := `{"manifest_version":"2.0.0","lease":{"heartbeat_interval":"2m","ttl":"20m"}}`
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Lease.HeartbeatInterval != Duration(2*time.Minute) {
		t.Fatalf("interval: %v", m.Lease.HeartbeatInterval)
	}
	if m.Lease.TTL != Duration(20*time.Minute) {
		t.Fatalf("ttl: %v", m.Lease.TTL)
	}
}

func TestIsLeaseActive(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		m    Manifest
		want bool
	}{
		{
			name: "unlocked",
			m:    Manifest{Lease: LeaseSettings{TTL: Duration(10 * time.Minute)}},
			want: false,
		},
		{
			name: "locked fresh heartbeat",
			m: Manifest{
				LockedBy:    "run-1",
				HeartbeatAt: now.Add(-1 * time.Minute),
				Lease:       LeaseSettings{TTL: Duration(10 * time.Minute)},
			},
			want: true,
		},
		{
			name: "locked stale heartbeat",
			m: Manifest{
				LockedBy:    "run-1",
				HeartbeatAt: now.Add(-15 * time.Minute),
				Lease:       LeaseSettings{TTL: Duration(10 * time.Minute)},
			},
			want: false,
		},
		{
			name: "locked heartbeat exactly at TTL boundary (stale)",
			m: Manifest{
				LockedBy:    "run-1",
				HeartbeatAt: now.Add(-10 * time.Minute),
				Lease:       LeaseSettings{TTL: Duration(10 * time.Minute)},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.IsLeaseActive(now)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
