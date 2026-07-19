package selfupdate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsVersionOlder(t *testing.T) {
	cases := []struct {
		name          string
		local, remote string
		want          bool
	}{
		{"patch behind", "2.0.0", "2.0.1", true},
		{"minor behind", "2.0.5", "2.1.0", true},
		{"major behind", "1.9.9", "2.0.0", true},
		{"equal", "2.1.0", "2.1.0", false},
		{"ahead", "2.1.0", "2.0.9", false},
		// The trap a lexical string compare gets wrong: "2.10.0" < "2.9.0"
		// lexically, but 2.10 is the newer release.
		{"semver not lexical — 2.10 newer than 2.9", "2.9.0", "2.10.0", true},
		{"semver not lexical — 2.10 not older than 2.9", "2.10.0", "2.9.0", false},
		// Shorter-is-older when shared parts tie.
		{"shorter is older", "1.0", "1.0.1", true},
		{"longer is newer reversed", "1.0.1", "1.0", false},
		// Malformed parts read as 0 — never panic.
		{"malformed remote stays low", "2.0.0", "garbage", false},
		{"malformed local is oldest", "garbage", "2.0.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsVersionOlder(tc.local, tc.remote))
		})
	}
}
