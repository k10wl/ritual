package ports_test

import (
	"errors"
	"strings"
	"testing"

	"ritual/internal/core/ports"
)

func TestEventStrings(t *testing.T) {
	cases := []struct {
		name     string
		evt      ports.Event
		contains string
	}{
		{"start", ports.StartInfo{Operation: "backup"}, "start backup"},
		{"finish", ports.FinishInfo{Operation: "backup"}, "finish backup"},
		{"update plain", ports.UpdateInfo{Operation: "op", Message: "hi"}, "op: hi"},
		{"update percent", ports.UpdateInfo{Operation: "up", Message: "doing", Data: map[string]any{"percent": 42.5}}, "(42.5%)"},
		{"error", ports.ErrorInfo{Operation: "op", Err: errors.New("boom")}, "error op: boom"},
		{"state-change", ports.StateChangedInfo{From: "A", To: "B"}, "A → B"},
		{"state-failed", ports.StateFailedInfo{State: "Preparing", Err: errors.New("bad")}, "failed in Preparing"},
		{"retry with key", ports.RetryAttemptInfo{Operation: "r2.Get", Key: "manifest.json", Attempt: 2, Err: errors.New("flaky")}, "key=manifest.json"},
		{"retry no key", ports.RetryAttemptInfo{Operation: "r2.List", Attempt: 3, Err: errors.New("flaky")}, "retry r2.List attempt=3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.evt.String()
			if !strings.Contains(got, tc.contains) {
				t.Errorf("%T.String() = %q, want substring %q", tc.evt, got, tc.contains)
			}
		})
	}
}
