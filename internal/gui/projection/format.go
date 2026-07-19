package projection

import (
	"fmt"
	"math"
)

// Size/speed unit formatting, ported 1:1 from the frontend's former
// telemetry-format.ts formatSize/formatSpeed (design-log/050): converting a
// byte count or rate into "58.8 MB"/"12.3 MB/s" is single-source-of-truth
// data shaping, computed once here instead of duplicated (and once wrongly)
// on the frontend. Percentage/ratio math and ETA copy stay frontend
// concerns — only unit conversion moves.

const (
	kb = 1024
	mb = kb * 1024
	gb = mb * 1024
)

// mbpsToBps converts a decimal-megabit rate (SpeedMbps/LogicalMbps, as
// carried on the wire) to bytes/second for size-style unit formatting.
// Mirrors the frontend's former MBPS_TO_BPS = 1_000_000 / 8.
func mbpsToBps(mbps float64) float64 { return mbps * 1_000_000 / 8 }

type unit struct {
	div      float64
	suffix   string
	decimals int
}

func pickUnit(n float64) unit {
	switch {
	case n >= gb:
		return unit{gb, "GB", 2}
	case n >= mb:
		return unit{mb, "MB", 1}
	case n >= kb:
		return unit{kb, "KB", 1}
	default:
		return unit{1, "B", 0}
	}
}

func fmtNum(n float64, decimals int) string {
	if decimals == 0 {
		return fmt.Sprintf("%.0f", math.Round(n))
	}
	return fmt.Sprintf("%.*f", decimals, n)
}

// formatSpeed mirrors telemetry-format.ts's formatSpeed(bps). Returns
// ("0", "B/s") for a non-positive or non-finite rate.
func formatSpeed(bps float64) (value, unitSuffix string) {
	if math.IsNaN(bps) || math.IsInf(bps, 0) || bps <= 0 {
		return "0", "B/s"
	}
	u := pickUnit(bps)
	return fmtNum(bps/u.div, u.decimals), u.suffix + "/s"
}

// formatSize mirrors telemetry-format.ts's formatSize(done, total). When
// total <= 0, the unit is picked from done alone and total renders empty.
func formatSize(done, total int64) (doneText, totalText, unitSuffix string) {
	if total <= 0 {
		u := pickUnit(float64(done))
		return fmtNum(float64(done)/u.div, u.decimals), "", u.suffix
	}
	u := pickUnit(float64(total))
	return fmtNum(float64(done)/u.div, u.decimals), fmtNum(float64(total)/u.div, u.decimals), u.suffix
}
