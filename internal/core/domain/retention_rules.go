package domain

// RetentionRules controls how many backups are kept per tier.
// Each tier protects backups independently — union logic, never conflicts.
type RetentionRules struct {
	KeepLast    int `json:"keep_last"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
}

// DefaultRetentionRules returns safe defaults: keep 2 latest, no tier rotation.
// Matches previous R2MaxBackups / LocalMaxBackups constants.
func DefaultRetentionRules() RetentionRules {
	return RetentionRules{KeepLast: 2}
}
