package progress

import (
	"fmt"
	"time"
)

// Tick is one snapshot of transfer metrics, emitted once per ticker
// interval when any counter advanced. Side-first: Remote carries the
// remote-storage activity (Pull/Push), Local carries the local-storage
// activity (Apply/Commit). Each Side has Down (BytesIn at its counter)
// and Up (BytesOut at its counter) Streams.
//
// All wire-rate flavours derive from Transfer (wire-layer bytes), all
// logical-rate flavours from Data (caller-layer bytes). One Tick per
// second captures everything the GUI or logs need from both sides at
// once. See design-log/001-progress-projection.md §Refinement 2.
type Tick struct {
	Elapsed time.Duration
	Remote  Side
	Local   Side
	Ops     OpsTally
}

// Side is one storage side's activity. Down counts bytes flowing INTO the
// caller from this side (GetStream / read); Up counts bytes flowing OUT
// of the caller into this side (PutStream / write).
//
//   - Remote.Down: Pull reads (remote → local).
//   - Remote.Up:   Push writes (local → remote).
//   - Local.Down:  Apply reads (local objects/ → workdir).
//   - Local.Up:    Commit writes (workdir → local objects/).
//
// The same Stream shape on both sides means the projection picks by side
// the same way it picks by direction: indexed access, no special cases.
type Side struct {
	Down Stream
	Up   Stream
}

// Stream is one direction's view of transfer activity.
//
//   - Data: uncompressed bytes seen by the caller. Matches PlanInfo.BytesTotal
//     and drives the progress bar's numerator.
//   - Transfer: compressed bytes that crossed the backend boundary. Source for
//     the three wire-rate flavours below and for "actual uplink/downlink used"
//     diagnostics.
//   - Instant: wire Mbps over the last tick — raw single-interval derivative.
//     Jumpy by construction (completion-driven counter bumps); kept on the
//     log line for diagnostics, not for the UI label.
//   - Average: wire Mbps over the last N ticks — rolling window mean. What
//     the UI label reads (matches curl's --progress-bar convention,
//     design-log/001 §Refinement).
//   - Smoothed: wire Mbps as EWMA with factor α — running average. Kept on
//     the log line as a diagnostic against which Average is cross-checked.
//   - DataAverage: LOGICAL Mbps over the last N ticks — rolling window mean
//     on the Data counter. Drives the chart's second series (Steam's "green
//     bar" — install/decompress rate, distinct from network throughput).
type Stream struct {
	Data        int64
	Transfer    int64
	Instant     float64
	Average     float64
	Smoothed    float64
	DataAverage float64
}

// OpsTally counts storage operations seen by the logical counter — one
// op per caller invocation regardless of how many wire-level reads or
// retries it triggered.
type OpsTally struct {
	Done   int64
	Failed int64
}

// String renders Tick as a six-line diagnostic block. One block per
// emitted tick; the on-disk log is grep-friendly via side+direction
// keywords (`remote down`, `local up`, etc.) and per-flavour suffixes
// (`inst=`, `avg=`, `smooth=`, `data_avg=`). Reconstructs what the UI
// rendered at any past second AND what the local-storage subsystems
// (Apply, Commit) were doing in parallel — no screen recording, no
// instrumentation.
func (t Tick) String() string {
	return fmt.Sprintf(
		"progress t=%.1fs\n"+
			"  remote down  data=%dB transfer=%dB inst=%.2fMbps avg=%.2fMbps smooth=%.2fMbps data_avg=%.2fMbps\n"+
			"  remote up    data=%dB transfer=%dB inst=%.2fMbps avg=%.2fMbps smooth=%.2fMbps data_avg=%.2fMbps\n"+
			"  local  down  data=%dB transfer=%dB inst=%.2fMbps avg=%.2fMbps smooth=%.2fMbps data_avg=%.2fMbps\n"+
			"  local  up    data=%dB transfer=%dB inst=%.2fMbps avg=%.2fMbps smooth=%.2fMbps data_avg=%.2fMbps\n"+
			"  ops    done=%d failed=%d",
		t.Elapsed.Seconds(),
		t.Remote.Down.Data, t.Remote.Down.Transfer, t.Remote.Down.Instant, t.Remote.Down.Average, t.Remote.Down.Smoothed, t.Remote.Down.DataAverage,
		t.Remote.Up.Data, t.Remote.Up.Transfer, t.Remote.Up.Instant, t.Remote.Up.Average, t.Remote.Up.Smoothed, t.Remote.Up.DataAverage,
		t.Local.Down.Data, t.Local.Down.Transfer, t.Local.Down.Instant, t.Local.Down.Average, t.Local.Down.Smoothed, t.Local.Down.DataAverage,
		t.Local.Up.Data, t.Local.Up.Transfer, t.Local.Up.Instant, t.Local.Up.Average, t.Local.Up.Smoothed, t.Local.Up.DataAverage,
		t.Ops.Done, t.Ops.Failed,
	)
}
