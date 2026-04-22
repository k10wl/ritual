// Package progress snapshots StorageCounters at a UI cadence and emits one
// ProgressTick event per interval. Maps directly to the r2sim POC's 1-second
// ticker (docs/research/r2sim/main.go): cumulative averages for smooth ETA,
// per-interval deltas for live Mbps.
package progress

import (
	"context"
	"fmt"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"
)

// Tick is the snapshot emitted by the ticker. Bytes are cumulative totals
// taken from StorageCounters; Mbps values are derived per-interval and
// cumulative respectively. Mirrors the "now/avg" pair used in r2sim.
type Tick struct {
	Elapsed     time.Duration
	BytesIn     int64
	BytesOut    int64
	OpsComplete int64
	OpsFailed   int64
	NowMbpsIn   float64
	NowMbpsOut  float64
	AvgMbpsIn   float64
	AvgMbpsOut  float64
}

// String maps Tick to a one-line log string shaped like the r2sim progress
// line so the GUI and logs can consume the same format.
func (t Tick) String() string {
	return fmt.Sprintf(
		"progress t=%.1fs in=%dB out=%dB ops=%d fail=%d | in avg=%.2fMbps now=%.2fMbps | out avg=%.2fMbps now=%.2fMbps",
		t.Elapsed.Seconds(), t.BytesIn, t.BytesOut, t.OpsComplete, t.OpsFailed,
		t.AvgMbpsIn, t.NowMbpsIn, t.AvgMbpsOut, t.NowMbpsOut,
	)
}

// Ticker reads a set of counters at interval and publishes a Tick per tick.
// Publish goes to the supplied EventBus; pass nil to silence the bus and
// use Snapshot directly from tests / other reducers.
type Ticker struct {
	counters *adapters.StorageCounters
	bus      ports.EventBus
	interval time.Duration
}

// NewTicker constructs the ticker. interval must be > 0.
func NewTicker(counters *adapters.StorageCounters, bus ports.EventBus, interval time.Duration) *Ticker {
	return &Ticker{counters: counters, bus: bus, interval: interval}
}

// Run blocks until ctx is cancelled, emitting one Tick per interval. The
// caller owns the goroutine; typically: `go ticker.Run(ctx)`.
func (t *Ticker) Run(ctx context.Context) {
	start := time.Now()
	last := start
	var lastIn, lastOut int64
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			bin := t.counters.BytesIn.Load()
			bout := t.counters.BytesOut.Load()
			tick := snapshot(start, last, now, lastIn, lastOut, bin, bout,
				t.counters.OpsComplete.Load(), t.counters.OpsFailed.Load())
			if t.bus != nil {
				t.bus.Publish(tick)
			}
			last = now
			lastIn = bin
			lastOut = bout
		}
	}
}

// Snapshot produces one Tick from the current counter state without running
// the ticker loop. Useful for tests and one-shot status calls.
func (t *Ticker) Snapshot(start, last time.Time, lastIn, lastOut int64) Tick {
	return snapshot(start, last, time.Now(), lastIn, lastOut,
		t.counters.BytesIn.Load(), t.counters.BytesOut.Load(),
		t.counters.OpsComplete.Load(), t.counters.OpsFailed.Load())
}

func snapshot(start, last, now time.Time, lastIn, lastOut, bin, bout, ops, fail int64) Tick {
	dt := now.Sub(last).Seconds()
	if dt <= 0 {
		dt = 1
	}
	el := now.Sub(start).Seconds()
	if el <= 0 {
		el = dt
	}
	return Tick{
		Elapsed:     now.Sub(start),
		BytesIn:     bin,
		BytesOut:    bout,
		OpsComplete: ops,
		OpsFailed:   fail,
		NowMbpsIn:   float64(bin-lastIn) * 8 / dt / 1e6,
		NowMbpsOut:  float64(bout-lastOut) * 8 / dt / 1e6,
		AvgMbpsIn:   float64(bin) * 8 / el / 1e6,
		AvgMbpsOut:  float64(bout) * 8 / el / 1e6,
	}
}
