// Package progress snapshots StorageCounters at a UI cadence and publishes
// one Tick per direction-active interval. See design-log/001-progress-
// projection.md.
package progress

import (
	"context"
	"sync/atomic"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"
)

// Default smoothing constants. Both are public Ticker fields so callers can
// override before Run; tests rely on this to drive the EWMA series with
// deterministic inputs.
const (
	DefaultAlpha   = 0.2 // EWMA factor — ~5-sample effective memory at 1s tick
	DefaultWindowN = 5   // rolling window length, in samples
)

// CounterSide pairs the two counter layers installed at one storage side
// (local or remote). Both pointers must be non-nil; pass the same pointer
// for both in tests that don't need the wire/logical split.
type CounterSide struct {
	Logical *adapters.StorageCounters
	Wire    *adapters.StorageCounters
}

// Ticker derives Tick events from two storage sides (remote + local), each
// with two counter layers (logical above CompressingStorage, wire below).
// Per side per direction it tracks two parallel window rings:
//
//   - wire ring → Instant (raw 1-tick), Average (5-tick rolling), Smoothed
//     (EWMA α). Average drives the UI speed label; Smoothed stays as a log
//     diagnostic.
//   - logical ring → DataAverage (5-tick rolling on Data). Drives the chart's
//     second series (decompress / install rate, distinct from wire rate).
//
// Op counts come from the remote-logical counter — one logical-op per caller
// invocation, no duplication across layers or sides.
//
// After activity ceases on BOTH sides, exactly one trailing zero-delta tick
// fires as the "we're done" marker, then the ticker stays silent until
// counters move again (audit fix #9).
type Ticker struct {
	remote, local CounterSide
	bus           ports.EventBus
	interval      time.Duration

	// Tunables — defaulted on first Snapshot / Run call.
	Alpha   float64
	WindowN int

	start, last time.Time

	// transferActive marks an in-flight network transfer (Push/Pull body).
	// While set, Run emits heartbeat ticks even when the byte counters are
	// frozen, so a stalled R2 PutStream still pulses liveness. Set by the
	// transfer goroutine, read by Run — atomic for that cross-goroutine race.
	transferActive atomic.Bool

	// Eight windowStates: 2 sides × 2 directions × 2 layers (wire + logical).
	// Layer split is what drives Stream.Average vs Stream.DataAverage.
	remoteDownWire, remoteUpWire windowState
	remoteDownData, remoteUpData windowState
	localDownWire, localUpWire   windowState
	localDownData, localUpData   windowState
}

// windowState carries per-(side, direction, layer) time-domain state.
// Fixed-size array + head index (see memory: feedback_shape_first_then_stdlib)
// — the shape is a ring of N numeric samples, not a linked list of arbitrary
// nodes.
type windowState struct {
	buf  []sample // len == WindowN; nil until init
	head int      // next slot to write; once full, also points at the oldest
	full bool
	ewma float64
	prev int64 // previous byte count for this layer, used for Instant
}

type sample struct {
	t  time.Time
	by int64
}

// NewTicker constructs a Ticker watching both storage sides. Each CounterSide
// carries the Logical (above compression) and Wire (below) counters for that
// side.
func NewTicker(remote, local CounterSide, bus ports.EventBus, interval time.Duration) *Ticker {
	return &Ticker{
		remote:   remote,
		local:    local,
		bus:      bus,
		interval: interval,
		Alpha:    DefaultAlpha,
		WindowN:  DefaultWindowN,
	}
}

// SetTransferActive marks whether a network transfer (Push/Pull body) is
// currently in-flight. While true, Run emits heartbeat ticks even when the
// byte counters are frozen — an R2 PutStream blocked on a TCP retransmit
// (31s silences observed) otherwise leaves the silence gate suppressing every
// tick, freezing the dial below 100% with no liveness signal. Callers set it
// true at the start of the transfer phase and false once bytes stop flowing.
// Safe for concurrent use: the transfer goroutine sets it while Run reads it.
func (t *Ticker) SetTransferActive(active bool) {
	t.transferActive.Store(active)
}

// Run blocks until ctx is cancelled, emitting one Tick per interval when any
// counter advanced. One trailing zero-delta tick fires after activity stops.
func (t *Ticker) Run(ctx context.Context) {
	t.init(time.Now())

	var prev counterSnapshot
	wasActive := false

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			cur := t.readCounters()
			active := cur != prev
			// Heartbeat: an explicitly-marked transfer that has stalled
			// (counters frozen, active==false) must still pulse so the
			// projection can render "Stalled — waiting on R2…" instead of a
			// dead-frozen dial. Idle stages leave transferActive false and
			// stay silent — see TestTicker_StableCounters_NoTicks.
			if !active && !wasActive && !t.transferActive.Load() {
				continue
			}

			tick := t.snapshotAt(now, cur)
			if t.bus != nil {
				t.bus.Publish(tick)
			}

			prev = cur
			wasActive = active
		}
	}
}

// Snapshot reads the current counter state and returns one Tick, advancing
// internal smoothing state. Successive Snapshot calls form a series — tests
// drive the EWMA / window by writing counters between calls.
//
// On the very first call, `now` seeds start + last so Elapsed begins at zero;
// callers wanting a non-zero Elapsed should call Snapshot once at the
// intended start time, then write counters and call again at later times.
func (t *Ticker) Snapshot(now time.Time) Tick {
	t.init(now)
	return t.snapshotAt(now, t.readCounters())
}

// counterSnapshot is the full set of atomic.Int64 values read from both
// sides in one tick. Lifted into its own type so equality comparison is
// trivial and Run's prev/cur diff is one expression.
type counterSnapshot struct {
	remoteLogIn, remoteLogOut   int64
	remoteWireIn, remoteWireOut int64
	localLogIn, localLogOut     int64
	localWireIn, localWireOut   int64
	ops, fail                   int64
}

func (t *Ticker) readCounters() counterSnapshot {
	return counterSnapshot{
		remoteLogIn:   t.remote.Logical.BytesIn.Load(),
		remoteLogOut:  t.remote.Logical.BytesOut.Load(),
		remoteWireIn:  t.remote.Wire.BytesIn.Load(),
		remoteWireOut: t.remote.Wire.BytesOut.Load(),
		localLogIn:    t.local.Logical.BytesIn.Load(),
		localLogOut:   t.local.Logical.BytesOut.Load(),
		localWireIn:   t.local.Wire.BytesIn.Load(),
		localWireOut:  t.local.Wire.BytesOut.Load(),
		ops:           t.remote.Logical.OpsComplete.Load(),
		fail:          t.remote.Logical.OpsFailed.Load(),
	}
}

func (t *Ticker) init(now time.Time) {
	if t.Alpha <= 0 {
		t.Alpha = DefaultAlpha
	}
	if t.WindowN <= 0 {
		t.WindowN = DefaultWindowN
	}
	for _, ws := range []*windowState{
		&t.remoteDownWire, &t.remoteUpWire, &t.remoteDownData, &t.remoteUpData,
		&t.localDownWire, &t.localUpWire, &t.localDownData, &t.localUpData,
	} {
		if ws.buf == nil {
			ws.buf = make([]sample, t.WindowN)
		}
	}
	if t.start.IsZero() {
		t.start = now
		t.last = now
	}
}

func (t *Ticker) snapshotAt(now time.Time, c counterSnapshot) Tick {
	dt := now.Sub(t.last).Seconds()
	if dt <= 0 {
		dt = t.interval.Seconds()
		if dt <= 0 {
			dt = 1
		}
	}

	rdw := t.advance(&t.remoteDownWire, now, c.remoteWireIn, dt)
	ruw := t.advance(&t.remoteUpWire, now, c.remoteWireOut, dt)
	rdd := t.advance(&t.remoteDownData, now, c.remoteLogIn, dt)
	rud := t.advance(&t.remoteUpData, now, c.remoteLogOut, dt)

	ldw := t.advance(&t.localDownWire, now, c.localWireIn, dt)
	luw := t.advance(&t.localUpWire, now, c.localWireOut, dt)
	ldd := t.advance(&t.localDownData, now, c.localLogIn, dt)
	lud := t.advance(&t.localUpData, now, c.localLogOut, dt)

	t.last = now

	return Tick{
		Elapsed: now.Sub(t.start),
		Remote: Side{
			Down: Stream{
				Data: c.remoteLogIn, Transfer: c.remoteWireIn,
				Instant: rdw.instant, Average: rdw.avg, Smoothed: rdw.ewma,
				DataAverage: rdd.avg,
			},
			Up: Stream{
				Data: c.remoteLogOut, Transfer: c.remoteWireOut,
				Instant: ruw.instant, Average: ruw.avg, Smoothed: ruw.ewma,
				DataAverage: rud.avg,
			},
		},
		Local: Side{
			Down: Stream{
				Data: c.localLogIn, Transfer: c.localWireIn,
				Instant: ldw.instant, Average: ldw.avg, Smoothed: ldw.ewma,
				DataAverage: ldd.avg,
			},
			Up: Stream{
				Data: c.localLogOut, Transfer: c.localWireOut,
				Instant: luw.instant, Average: luw.avg, Smoothed: luw.ewma,
				DataAverage: lud.avg,
			},
		},
		Ops: OpsTally{Done: c.ops, Failed: c.fail},
	}
}

type derived struct {
	instant float64
	avg     float64
	ewma    float64
}

// advance updates ws with the new byte total at time now and returns the
// three speed flavours for this tick. EWMA is seeded by the first Instant
// value; subsequent values blend with Alpha. Window average uses bytes-
// since-oldest over wall-time-since-oldest; before the ring fills, it
// falls back to instant so the first ticks don't print misleading zeros.
func (t *Ticker) advance(ws *windowState, now time.Time, cur int64, dt float64) derived {
	instant := mbps(cur-ws.prev, dt)

	if !ws.full && ws.head == 0 {
		ws.ewma = instant
	} else {
		ws.ewma = t.Alpha*instant + (1-t.Alpha)*ws.ewma
	}

	var oldest sample
	hasOldest := false
	switch {
	case ws.full:
		oldest = ws.buf[ws.head]
		hasOldest = true
	case ws.head > 0:
		oldest = ws.buf[0]
		hasOldest = true
	}

	ws.buf[ws.head] = sample{t: now, by: cur}
	ws.head = (ws.head + 1) % len(ws.buf)
	if !ws.full && ws.head == 0 {
		ws.full = true
	}

	avg := instant
	if hasOldest {
		elapsed := now.Sub(oldest.t).Seconds()
		if elapsed > 0 {
			avg = mbps(cur-oldest.by, elapsed)
		}
	}

	ws.prev = cur
	return derived{instant: instant, avg: avg, ewma: ws.ewma}
}

func mbps(bytes int64, seconds float64) float64 {
	if seconds <= 0 {
		return 0
	}
	return float64(bytes) * 8 / seconds / 1e6
}
