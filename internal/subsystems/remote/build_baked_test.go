package remote

import (
	"os"
	"testing"
)

// White-box (package remote) so it can set the unexported bakedRemoteMode
// ldflag target. Covers the design-log/048 precedence: env wins over baked,
// baked falls back when env is unset, default is ModeR2.
func TestResolveMode_BakedPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		baked string
		env   string // "" + envSet=false means unset
		set   bool
		want  Mode
	}{
		{"baked mock, env unset → mock", "mock", "", false, ModeMock},
		{"baked empty, env unset → R2", "", "", false, ModeR2},
		{"baked mock, env=r2 → env wins → R2", "mock", "r2", true, ModeR2},
		{"baked empty, env=mock → env wins → mock", "", "mock", true, ModeMock},
		{"baked mock, env empty-string → baked wins → mock", "mock", "", true, ModeMock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			restore := bakedRemoteMode
			bakedRemoteMode = tc.baked
			t.Cleanup(func() { bakedRemoteMode = restore })

			if tc.set {
				t.Setenv(EnvRemoteMode, tc.env)
			} else {
				old, had := os.LookupEnv(EnvRemoteMode)
				_ = os.Unsetenv(EnvRemoteMode)
				t.Cleanup(func() {
					if had {
						_ = os.Setenv(EnvRemoteMode, old)
					}
				})
			}

			if got := ResolveMode(); got != tc.want {
				t.Fatalf("ResolveMode() = %v, want %v (baked=%q env=%q set=%v)", got, tc.want, tc.baked, tc.env, tc.set)
			}
		})
	}
}
