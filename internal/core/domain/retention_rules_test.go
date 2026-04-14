package domain_test

import (
	"encoding/json"
	"testing"

	"ritual/internal/core/domain"
)

func TestDefaultRetentionRules_KeepLast2(t *testing.T) {
	r := domain.DefaultRetentionRules()
	if r.KeepLast != 2 {
		t.Errorf("KeepLast = %d, want 2", r.KeepLast)
	}
	if r.KeepDaily != 0 || r.KeepWeekly != 0 || r.KeepMonthly != 0 {
		t.Errorf("non-last tiers = %+v, want all zero", r)
	}
}

func TestRetentionRules_JSONRoundTrip(t *testing.T) {
	original := domain.RetentionRules{KeepLast: 3, KeepDaily: 1, KeepWeekly: 2, KeepMonthly: 4}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.RetentionRules
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestRetentionRules_JSONFields(t *testing.T) {
	r := domain.RetentionRules{KeepLast: 2}
	data, _ := json.Marshal(r)
	got := string(data)
	want := `{"keep_last":2,"keep_daily":0,"keep_weekly":0,"keep_monthly":0}`
	if got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
