package progress_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/adapters/progress"
)

// 50 Mbps over 1s == 6_250_000 bytes.
const mbpsBytesPerSec = 1_000_000 / 8 // bytes per Mbps per second

// driveTicker walks the ticker through a deterministic Mbps series. Each
// step adds `instantMbps * 1s` worth of bytes to BytesOut and snapshots at a
// monotonically advancing wall clock spaced one second apart. Returns the
// emitted Tick series.
func driveTicker(t *testing.T, ticker *progress.Ticker, counters *adapters.StorageCounters, series []float64) []progress.Tick {
	t.Helper()
	base := time.Now()
	out := make([]progress.Tick, 0, len(series))
	for i, mbps := range series {
		bytes := int64(mbps * float64(mbpsBytesPerSec))
		counters.BytesOut.Add(bytes)
		out = append(out, ticker.Snapshot(base.Add(time.Duration(i)*time.Second)))
	}
	return out
}

// TestTicker_SmoothedDampensCompletionSpike is the spike user story from
// design-log/001-progress-projection.md: ten parallel workers' decoder
// streams clump within a single tick window, producing a >100 Mbps raw
// spike that the unsmoothed label flashed to the user. The smoothed series
// must absorb the burst — never overshoot meaningfully above the steady-
// state baseline — so the GUI label walks instead of jumping.
//
// Input: [50, 50, 200, 50, 50] Mbps. With α=0.2 the smoothed series stays
// at 50 through the first two samples, climbs to 80 on the spike, and
// decays back. Max smoothed must stay well under raw 200 — that's the
// regression for the user-reported flicker.
func TestTicker_SmoothedDampensCompletionSpike(t *testing.T) {
	counters := &adapters.StorageCounters{}
	ticker := progress.NewTicker(singleSide(counters), singleSide(counters), nil, time.Second)

	ticks := driveTicker(t, ticker, counters, []float64{50, 50, 200, 50, 50})
	require.Len(t, ticks, 5, "driver must produce one tick per Snapshot call so the series is testable")

	var maxSmoothed float64
	for _, tk := range ticks {
		if tk.Remote.Up.Smoothed > maxSmoothed {
			maxSmoothed = tk.Remote.Up.Smoothed
		}
	}
	assert.Less(t, maxSmoothed, 90.0,
		"smoothed series must absorb the raw 200 Mbps spike — α=0.2 EWMA caps the peak below 90 so the GUI label never flashes >100 Mbps the way the user reported")
	assert.InDelta(t, 200.0, ticks[2].Remote.Up.Instant, 0.5,
		"raw Instant must still reflect the 200 Mbps spike in the log line — the diagnostic is intact, only the user-facing smoothed value is damped")
	assert.InDelta(t, 50.0, ticks[1].Remote.Up.Smoothed, 0.5,
		"before the spike, smoothed must equal the steady 50 Mbps baseline — the smoother seeds on the first sample and doesn't drift on stable input")
}

// TestTicker_SmoothedTracksSlowdown bounds the other end of the trade-off:
// smoothing must NOT hide a real change in throughput. After a sustained
// slowdown the smoothed series has to descend — otherwise a stalled
// upload would keep the GUI on a hopeful "42 Mbps" caption while no bytes
// actually move.
//
// Input: [100, 100, 100, 20, 20, 20, 20, 20, 20] Mbps. By the 9th sample
// (6 ticks after the change), smoothed must be below 50 — well into the
// new regime — proving the smoother tracks reality.
func TestTicker_SmoothedTracksSlowdown(t *testing.T) {
	counters := &adapters.StorageCounters{}
	ticker := progress.NewTicker(singleSide(counters), singleSide(counters), nil, time.Second)

	ticks := driveTicker(t, ticker, counters,
		[]float64{100, 100, 100, 20, 20, 20, 20, 20, 20})
	require.Len(t, ticks, 9)

	assert.InDelta(t, 100.0, ticks[2].Remote.Up.Smoothed, 0.5,
		"during the steady-state phase smoothed must equal the baseline 100 Mbps — the smoother does not drift when input is flat")

	final := ticks[len(ticks)-1].Remote.Up.Smoothed
	assert.Less(t, final, 50.0,
		"six ticks after the slowdown smoothed must descend below 50 Mbps — otherwise a stalled upload would leave the GUI showing a stale fast label and the user would think bytes were still moving")

	for i := 3; i < len(ticks); i++ {
		assert.LessOrEqual(t, ticks[i].Remote.Up.Smoothed, ticks[i-1].Remote.Up.Smoothed,
			"after the slowdown begins (sample %d) smoothed must monotonically descend — any rise here would mean the smoother is amplifying noise, not tracking reality", i)
	}
}

// TestTicker_AverageWindowFallsBackToInstantBeforeFull guards the partial-
// window branch: until WindowN samples accumulate, Average must equal
// Instant rather than printing a misleading zero. Otherwise the first
// (WindowN - 1) ticks of a transfer would show a flat "0 Mbps avg" while
// Instant correctly climbed — operators staring at log lines would chase
// a non-bug.
func TestTicker_AverageWindowFallsBackToInstantBeforeFull(t *testing.T) {
	counters := &adapters.StorageCounters{}
	ticker := progress.NewTicker(singleSide(counters), singleSide(counters), nil, time.Second)

	ticks := driveTicker(t, ticker, counters, []float64{40, 40})
	require.Len(t, ticks, 2)

	assert.InDelta(t, ticks[0].Remote.Up.Instant, ticks[0].Remote.Up.Average, 0.5,
		"first tick: Average must equal Instant because the rolling window has no oldest sample yet — log lines must not lie with a 0.0Mbps avg when bytes are clearly moving")
	assert.Greater(t, ticks[1].Remote.Up.Average, 0.0,
		"second tick: Average must be positive once the window has at least one prior sample — bytes-since-oldest over wall-time-since-oldest")
}

// TestTicker_LocalAndRemoteCountersAdvanceIndependently is the local-ticker
// wiring regression. A bug that wired both sides to the same counter pair
// (or that swapped them) would silently make every Tick mirror Remote into
// Local. This test drives ONLY the local counter and asserts Local.* moves
// while Remote.* stays at zero — proving the side fields are independently
// sourced from their own CounterSide inputs.
func TestTicker_LocalAndRemoteCountersAdvanceIndependently(t *testing.T) {
	remoteCounters := &adapters.StorageCounters{}
	localCounters := &adapters.StorageCounters{}
	ticker := progress.NewTicker(singleSide(remoteCounters), singleSide(localCounters), nil, time.Second)

	base := time.Now()
	const mbps = 80.0
	step := int64(mbps * float64(mbpsBytesPerSec))

	var ticks []progress.Tick
	for i := 0; i < 5; i++ {
		localCounters.BytesIn.Add(step) // simulate Apply reading from local objects/
		ticks = append(ticks, ticker.Snapshot(base.Add(time.Duration(i)*time.Second)))
	}

	final := ticks[len(ticks)-1]
	assert.InDelta(t, mbps, final.Local.Down.Average, 1.0,
		"Local.Down.Average must reflect the local counter's BytesIn rate — Apply reads from local objects/ into workdir, that traffic surfaces here and nowhere else")
	assert.Equal(t, int64(0), final.Remote.Down.Data,
		"Remote.Down.Data must stay at zero when only the local counter moved — a wiring regression that aliased Local to Remote would surface here as a non-zero value")
	assert.Equal(t, 0.0, final.Remote.Down.Average,
		"Remote.Down.Average must stay at zero when only the local counter moved — proves the per-side windowStates are sourced from independent counters")
}

// TestTicker_DataAverageTracksLogicalIndependentlyOfWire is the dual-chart
// regression: the logical (Stream.DataAverage) and wire (Stream.Average)
// rolling-window rates must derive from independent counter pairs. On a
// compressible payload the logical rate is higher than the wire rate (caller
// hands in more bytes than zstd lets through); the two numbers feeding the
// chart's green and blue lines therefore have a measurable gap. A wiring
// regression that collapsed them to the same counter would silently make
// the dual chart show one line twice.
func TestTicker_DataAverageTracksLogicalIndependentlyOfWire(t *testing.T) {
	logical := &adapters.StorageCounters{}
	wire := &adapters.StorageCounters{}
	remote := progress.CounterSide{Logical: logical, Wire: wire}
	idle := progress.CounterSide{Logical: &adapters.StorageCounters{}, Wire: &adapters.StorageCounters{}}
	ticker := progress.NewTicker(remote, idle, nil, time.Second)

	base := time.Now()
	const wireMbps, logicalMbps = 40.0, 100.0
	wireStep := int64(wireMbps * float64(mbpsBytesPerSec))
	logicalStep := int64(logicalMbps * float64(mbpsBytesPerSec))

	var ticks []progress.Tick
	for i := 0; i < 5; i++ {
		wire.BytesOut.Add(wireStep)
		logical.BytesOut.Add(logicalStep)
		ticks = append(ticks, ticker.Snapshot(base.Add(time.Duration(i)*time.Second)))
	}

	final := ticks[len(ticks)-1]
	assert.InDelta(t, wireMbps, final.Remote.Up.Average, 1.0,
		"Up.Average must track the WIRE counter — the rolling-window rate of the bytes that physically crossed the backend boundary, which is what the speed label shows")
	assert.InDelta(t, logicalMbps, final.Remote.Up.DataAverage, 1.0,
		"Up.DataAverage must track the LOGICAL counter — the decompress/install rate, distinct from wire, drives the chart's second series. A bug that wired both windows to the same counter would silently make the dual chart redundant")
	assert.Greater(t, final.Remote.Up.DataAverage, final.Remote.Up.Average,
		"on this fixture the logical counter advances faster than the wire counter (simulating compressible payload). DataAverage > Average must hold so the dual chart visibly separates the two lines")
}
