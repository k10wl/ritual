package control_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/gui/control"
)

func isolateSettings(t *testing.T) {
	t.Helper()
	orig := config.RootPath
	config.RootPath = t.TempDir()
	t.Cleanup(func() { config.RootPath = orig })
}

func TestSetThenGetRetentionRules_RoundTrips(t *testing.T) {
	isolateSettings(t)
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)

	local := domain.RetentionRules{KeepLast: 3, KeepDaily: 2}
	remote := domain.RetentionRules{KeepLast: 1, KeepMonthly: 4}
	require.NoError(t, svc.SetRetentionRules(local, remote))

	got := svc.GetRetentionRules()
	assert.Equal(t, local, got.Local, "GetRetentionRules must return the persisted local rules")
	assert.Equal(t, remote, got.Remote, "GetRetentionRules must return the persisted remote rules")
}

func TestSetRetentionRules_TakesEffectForRefsRetention(t *testing.T) {
	// End-to-end of the 039 promise: a SetRetentionRules write is what the prune
	// engine reads at Select time. Proven here through the public API: set, then
	// read back the effective rules. (The Select-reads-fresh behaviour itself is
	// covered in internal/core/retention.)
	isolateSettings(t)
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.SetRetentionRules(
		domain.RetentionRules{KeepLast: 5, KeepWeekly: 3},
		domain.RetentionRules{KeepLast: 2},
	))
	got := svc.GetRetentionRules()
	assert.Equal(t, 5, got.Local.KeepLast)
	assert.Equal(t, 3, got.Local.KeepWeekly)
}

func TestGetRetentionRules_NoSettingsFile_ReturnsDefaults(t *testing.T) {
	isolateSettings(t) // temp dir, no settings.json written
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	got := svc.GetRetentionRules()
	assert.Equal(t, domain.DefaultRetentionRules(), got.Local,
		"a missing settings file must yield default rules so the section always renders effective values")
	assert.Equal(t, domain.DefaultRetentionRules(), got.Remote)
}

func TestGetRetentionRules_ZeroValueSide_NormalisesToDefaults(t *testing.T) {
	isolateSettings(t)
	// Persist a settings file whose retention is left zero-value.
	s := domain.DefaultSettings()
	s.LocalRetention = domain.RetentionRules{}
	s.RemoteRetention = domain.RetentionRules{}
	require.NoError(t, s.Save())

	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	got := svc.GetRetentionRules()
	assert.Equal(t, domain.DefaultRetentionRules(), got.Local,
		"a zero-value side must show the effective default, mirroring what the prune engine applies")
}

func TestSetRetentionRules_KeepLastZeroAllowed(t *testing.T) {
	isolateSettings(t)
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.SetRetentionRules(
		domain.RetentionRules{KeepLast: 0, KeepDaily: 1},
		domain.DefaultRetentionRules(),
	), "keep_last:0 is the spec's documented edge case — allowed (cautioned in UI), never blocked")
}

func TestSetRetentionRules_RejectsNegative(t *testing.T) {
	// No upper cap (design-log/033 supersession §3 — the stepper is uncapped),
	// so large counts must round-trip. Negatives are still nonsense and stay
	// rejected before any write.
	isolateSettings(t)
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.SetRetentionRules(
		domain.RetentionRules{KeepLast: 6, KeepMonthly: 99},
		domain.DefaultRetentionRules(),
	), "tiers above 5 must round-trip — the stepper is uncapped (design-log/033 supersession §3)")
	require.Error(t, svc.SetRetentionRules(
		domain.DefaultRetentionRules(),
		domain.RetentionRules{KeepWeekly: -1},
	), "a negative tier must be rejected")
}
