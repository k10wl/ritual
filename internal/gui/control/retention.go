package control

import (
	"fmt"

	"ritual/internal/core/domain"
)

// maxTier is the inclusive upper bound the GUI segmented control offers per tier
// (design-log/033 §Q1). keep_last:0 is allowed — it is the spec's documented
// "prune everything next session" edge case, surfaced with a caution in the UI,
// not blocked here.
const maxTier = 5

// RetentionConfig is the local + remote retention rule pair surfaced to the
// Retention section in Advanced (design-log/039). Each side is four 0..5 tier
// counts. The frontend edits one side at a time (scope switch) and persists both.
type RetentionConfig struct {
	Local  domain.RetentionRules `json:"local"`
	Remote domain.RetentionRules `json:"remote"`
}

// GetRetentionRules returns the persisted local + remote retention rules so the
// Retention section can render the current policy. Zero-value (unconfigured)
// sides normalise to defaults, so the UI shows the *effective* rules the prune
// will use. A load error collapses to defaults — the section always renders.
func (c *ControlService) GetRetentionRules() RetentionConfig {
	settings, err := domain.LoadSettings()
	if err != nil || settings == nil {
		return RetentionConfig{Local: domain.DefaultRetentionRules(), Remote: domain.DefaultRetentionRules()}
	}
	return RetentionConfig{
		Local:  normaliseRules(settings.LocalRetention),
		Remote: normaliseRules(settings.RemoteRetention),
	}
}

// SetRetentionRules validates and persists both sides' rules. The next prune
// reads the file fresh (design-log/039 §Q1), so the change takes effect on the
// next sync without a restart. Each tier must be 0..5; keep_last:0 is permitted
// (the spec edge case). Out-of-range values are rejected before any write so a
// malformed call can't corrupt the policy.
func (c *ControlService) SetRetentionRules(local, remote domain.RetentionRules) error {
	if err := validateRules("local", local); err != nil {
		return err
	}
	if err := validateRules("remote", remote); err != nil {
		return err
	}
	settings, err := domain.LoadSettings()
	if err != nil {
		return fmt.Errorf("set retention: load settings: %w", err)
	}
	settings.LocalRetention = local
	settings.RemoteRetention = remote
	if err := settings.Save(); err != nil {
		return fmt.Errorf("set retention: save settings: %w", err)
	}
	return nil
}

// normaliseRules maps a zero-value rule set to defaults, matching what the prune
// engine does at Select time (design-log/039) so the GUI shows effective rules.
func normaliseRules(r domain.RetentionRules) domain.RetentionRules {
	if r == (domain.RetentionRules{}) {
		return domain.DefaultRetentionRules()
	}
	return r
}

func validateRules(side string, r domain.RetentionRules) error {
	for _, tier := range []struct {
		name string
		n    int
	}{
		{"keep_last", r.KeepLast},
		{"keep_daily", r.KeepDaily},
		{"keep_weekly", r.KeepWeekly},
		{"keep_monthly", r.KeepMonthly},
	} {
		if tier.n < 0 || tier.n > maxTier {
			return fmt.Errorf("%s %s must be between 0 and %d, got %d", side, tier.name, maxTier, tier.n)
		}
	}
	return nil
}
