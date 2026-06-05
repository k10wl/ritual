# 028 — Transfer ETA stability (kill the swing)

**Date:** 2026-05-25
**Status:** Implemented
**Related:** [[001-progress-projection]] (three speed flavours: `Average`, `Smooth`, `DataAverage`), [[009-telemetry-hierarchy]] (ETA in dial sub), [[018-logical-rate-in-ui]] (`logicalMbps` = `Stream.DataAverage` for ETA + under-slot), [[027-saving-worlds-prep-eta]] (parallel ETA work for server start/stop — different data source).

## Background

Live observation, 2026-05-25:

> "speed goes from 2mbps to 16mbps and to 8 and i see writing of 4 minutes to arrive -> 30 seconds -> 2 minutes. and that's in 3 seconds. extremely volatile which ain't good."

Current chain (`projection.go:151-165`, `ritual-app.ts:249-259`, sub-line `etaSub`):

1. `progress.Ticker` emits `progress.Tick` every 1s. Fields per side:
   - `Instant` — last-second rate (one sample).
   - `Average` — ~5s rolling mean (curl convention).
   - `Smooth` — EMA, separate half-life.
   - `DataAverage` — 5s rolling mean over the **logical** stream (post-decompress).
2. Projection picks `t.Remote.{Up,Down}.DataAverage` and writes it to `vm.logicalMbps`. (Design-log/018: logical to match the bytes counter.)
3. Frontend `computeSpeedBps` returns `vm.logicalMbps * MBPS_TO_BPS` when nonzero; falls back to per-beat divided rate.
4. `etaSub` (in ritual-app.ts via PHASE_VIEW.PhaseDownloading.sub / PhaseSaving.sub) takes `(bytesTotal - bytesDone) / speedBps`, formats as `mm:ss`.

The 5s rolling window over a real R2/network link with mod-bundle bursts is fundamentally jittery. A burst of compressible JSON pushes `DataAverage` to 16 Mbps; the next second waits on a slow blob and it drops to 2 Mbps. ETA = remaining_bytes / rate, so ETA inherits the inverse swing: doubling the rate halves the ETA.

User already tried HIG-coarse buckets ("About 30 seconds") mentally and dismissed: bucket transitions would still jump ("About 30s → About 8 minutes → About 3 minutes → About 10 minutes in 5 seconds"). The bucketing doesn't fix the underlying rate volatility — it just truncates the noise to bucket boundaries.

## Problem

ETA must:

1. Move only when the *true* remaining time has materially changed (not when the *last 5 seconds* of rate flickered).
2. Decrease monotonically during normal progress — a counter that ticks up while bytes are flowing reads as broken.
3. React to genuine stalls (link drops, server backpressure) without going haywire on momentary lulls.
4. Survive the worst case in the user's repro: 2→16→8 Mbps swings inside a 3-second window.

The current pipeline fails (1), (2), (4). It handles (3) honestly but at the cost of everything else.

## Questions and Answers

**Q1.** Why not just widen the rolling window from 5s to 30s?
**A.** Better, but not enough. A 30s mean still swings significantly when the link variance is high (compressible JSON vs. binary mod blobs alternate every few seconds). And widening the window slows reaction to *real* stalls — a connection drop takes 30s to register as zero. Trades one bad behaviour for another.

**Q2.** What is the canonical "honest ETA" computation?
**A.** **Session-wide average**: `rate = bytes_done / elapsed_since_beat_start`, `ETA = (bytes_total - bytes_done) / rate`. Every second the denominator grows linearly; ETA decreases monotonically by construction (given bytes_done grows at any positive rate). The numerator `bytes_total - bytes_done` shrinks as bytes flow, so ETA decreases even faster. **Cannot swing** in the way the user described.

  Drawbacks:
  - Slow to react to mid-beat speedups: if the first half of the transfer was slow, the session-wide average stays low; even when the rate doubles, ETA shrinks less than it "should." Reads as overly pessimistic.
  - Slow to react to mid-beat slowdowns: if the link drops to 0 Bps after a fast start, ETA still reflects the cumulative average. Reads as overly optimistic.

  Both drawbacks are *worth it* relative to the current volatility. Software Update, browser downloads, every honest copy-progress indicator on Unix uses some flavour of this.

**Q3.** How do we react to a real stall, then?
**A.** Stalls have a clean signal: `bytes_done` stops growing. We don't need ETA to detect stalls — we can detect them directly via "no Tick saw bytes_done advance in N seconds" and surface a separate UI hint ("Stalled — reconnecting?") rather than letting ETA balloon. Out of scope for this log (no current stall detector in the codebase); flagged as a follow-up.

**Q4.** Should ETA be allowed to increase?
**A.** No, with one exception: when the **plan** changes (`PlanInfo` re-fires) and `bytes_total` grew. Then ETA grows because there's *literally* more work. Inside a single beat with a fixed `bytes_total`, ETA must be monotonically non-increasing.

**Q5.** What about the very first few seconds when no rate is available?
**A.** ETA shows the existing "wait for digits" decoder placeholder (`·····`, design-log/009). Don't show "Computing..." or a fake huge number. The placeholder buys us ~3-5 seconds of grace before the cumulative average is meaningful (≥5 bytes_done samples).

**Q6.** Does the under-slot **speed** display also need fixing?
**A.** Partially. The user described speed *and* ETA both jumping. Separate concerns:
  - **Speed** is meant to be live — "how fast right now?" — so a 5s rolling mean (`DataAverage`) is defensible. But the user reads it as agro. Option: keep the live rolling speed, but with a heavier smoothing (e.g. 15s EMA). Loses some responsiveness, still feels honest.
  - **ETA** should NOT come from the same number as Speed. ETA from session-wide average; speed from rolling. Display them side-by-side honestly even if they disagree (rate > session-avg means "faster than your trend so far").

**Q7.** What if the user *wants* to see optimistic ETA when the link genuinely speeds up?
**A.** Hybrid: weighted average of session-wide rate (slow-changing baseline) and recent rate (responsive), e.g. `rate = 0.7 * session_avg + 0.3 * recent_avg`. Reacts modestly to genuine speedups but never swings 8× like 2→16 Mbps. **Default starting point.** If user feedback says "still too jumpy" → raise to 0.9 / 0.1. If "too sluggish" → lower to 0.5 / 0.5.

**Q8.** Where in the codebase does the change land?
**A.**
  - **Go side (`internal/adapters/progress/`)**: add a new field to `Stream` — `SessionAverage` = total `Data` / elapsed since first Tick of this beat. This is the canonical denominator for ETA.
  - **Projection (`projection.go`)**: when populating `vm.logicalMbps` for the live speed cell, keep the existing `DataAverage` source (but consider tightening to `Smooth` for less jitter — see Q9). For the ETA-dedicated rate, write a *separate* field on the ViewModel.
  - **ViewModel**: add `EtaSeconds int64` (or `LogicalMbpsForEta float64`) so the frontend doesn't recompute ETA from the live `logicalMbps`. Single source of truth, computed Go-side from session-wide average.
  - **Frontend**: `etaSub` reads `vm.etaSeconds` directly; no division in JS.

**Q9.** Speed cell: which existing field?
**A.** Try `Smooth` (EMA) first. The user complained about the visible speed swinging 2→16→8 Mbps inside 3 seconds — that's `Average` (5s rolling) or `Instant` behaviour. `Smooth`'s EMA with a longer half-life should damp that. If `Smooth`'s half-life is too short, widen it in the ticker. Doesn't touch ETA logic.

**Q10.** Monotonic guard: enforce in Go or JS?
**A.** Go. ETA is a backend-derived value now; the projection or the ticker is the right place to clamp `etaSeconds = min(prev_etaSeconds, computed)`. Reset to the new `bytes_total/rate` baseline on `PlanInfo` (Q4). Keeping it in Go means the value emitted via ViewModel is already correct — no frontend math, no race between rapid emissions.

**Q11.** Does this apply to Pull (download) and Push (upload) symmetrically?
**A.** Yes — same logic, two computations. `progress.Side` already has `Down` and `Up`. Add `SessionAverage` to both; projection picks the active one per pipeline stage (Pulling → Down, Pushing → Up).

**Q12.** What about the `Progress=100` empty-delta anchor (design-log/019)?
**A.** Unchanged. If `bytes_total == 0`, no ETA is computed (would divide by zero), `etaSeconds = 0`, frontend shows no ETA (placeholder or empty). PhaseSaving's "Wrapping up" override (ritual-app.ts:298-303) is unaffected.

## Design

### Go side — new field on `progress.Stream`

```go
// internal/adapters/progress/ticker.go (or wherever Stream lives)
type Stream struct {
    Data         int64
    Transfer     int64
    Instant      float64 // Mbps, last second
    Average      float64 // 5s rolling mean over Transfer (existing)
    Smooth       float64 // EMA over Transfer
    DataAverage  float64 // 5s rolling mean over Data (logical)
    SessionAverage float64 // NEW: total Data / elapsed since beat start; resets on PlanInfo
}
```

Ticker tracks the beat-start `time.Time` and `Data` snapshot; on `PlanInfo` arrival, reset.

### Go side — ETA in projection

```go
// projection.go
case progress.Tick:
    return p.onTick(e)

func (p *Projection) onTick(t progress.Tick) bool {
    switch p.pipelineStage {
    case ritual.StagePulling:
        side := t.Remote.Down
        p.state.BytesDone   = side.Data
        p.state.SpeedMbps   = side.Average      // existing wire rate, used by logs/chart
        p.state.LogicalMbps = side.Smooth       // CHANGED: was DataAverage; smoother live display (Q9)
        p.state.EtaSeconds  = etaFromSessionAvg(p.state.BytesTotal, side.Data, side.SessionAverage, p.state.EtaSeconds)
    case ritual.StagePushing:
        side := t.Remote.Up
        // ... same shape
    default: return false
    }
    return true
}

func etaFromSessionAvg(total, done int64, rateMbps float64, prev int64) int64 {
    if total <= 0 || rateMbps <= 0 { return 0 }
    remainingBytes := total - done
    if remainingBytes <= 0 { return 0 }
    rateBps := rateMbps * 1_000_000 / 8
    next := int64(float64(remainingBytes) / rateBps)
    // Monotonic guard (Q10): never increase within the same plan.
    if prev > 0 && next > prev { next = prev }
    return next
}
```

On `PlanInfo`, reset `p.state.EtaSeconds = 0` so the next Tick re-baselines.

### Frontend — read the field, no math

```ts
// frontend/src/ritual-app.ts — PHASE_VIEW etaSub helper, replace existing
function etaSub(vm: ViewModel, _ctx: AppCtx): string {
    if (vm.etaSeconds <= 0) return "·····"; // decoder placeholder per 009
    return formatEta(vm.etaSeconds);
}
```

(`computeSpeedBps` stays — it still feeds the under-slot **speed** cell from `vm.logicalMbps`, now `Smooth`-sourced.)

### Hybrid weighting — deferred

Q7 proposed `0.7 * session + 0.3 * recent` hybrid; deferred to a follow-up after this first cut ships. Reason: session-wide-only is the canonical baseline; ship the boring honest version, then iterate if it reads as too sluggish.

## Implementation Plan

**Phase A — Go: add `SessionAverage` to `Stream`.**

1. Track beat-start time in the ticker; reset on `PlanInfo`.
2. Compute `SessionAverage = Data / elapsedSeconds` per Tick per side.
3. Log lines already print `data_avg=...` — add `sess_avg=...` for visibility.
4. Tests: empty stream → zero; constant-rate stream → SessionAverage converges to true rate; rate doubles mid-beat → SessionAverage moves but bounded by halving (math).

**Phase B — Projection: compute `EtaSeconds`, switch live speed to `Smooth`.**

1. Add `EtaSeconds int64` to `viewmodel.go`. JSON tag matches frontend.
2. Wire `etaFromSessionAvg` per Design.
3. Switch `p.state.LogicalMbps = side.Smooth` (was `side.DataAverage`).
4. Reset `EtaSeconds` on `PlanInfo` arrival.
5. Tests:
   - Tick mid-Pulling → EtaSeconds reflects (bytesTotal-bytesDone)/SessionAverage.
   - Tick after PlanInfo re-fires with larger bytesTotal → EtaSeconds re-baselines, not capped by old prev.
   - Tick with SessionAverage briefly spiking → EtaSeconds clamped to previous value (monotonic).

**Phase C — Frontend.**

1. `etaSub` reads `vm.etaSeconds` directly; no division.
2. Speed cell already reads `vm.logicalMbps` — inherits the `Smooth` change automatically.
3. Story: progress-bar with synthetic Tick stream that swings 2→16→8 Mbps → EtaSeconds visibly stable.

**Phase D — smoke.**

1. Re-run the user's exact repro shape (full session, ~880 MB push). Observe the under-slot ETA — should move monotonically downward, no swings.
2. Compare wall-clock final ETA prediction to actual finish time. Acceptable error: ±15% (session-wide average is conservative).

## Verification

- ETA digit reads strictly non-increasing during a single beat (PlanInfo unchanged).
- Speed cell still shows live rate but its perceived volatility is materially lower than today's `DataAverage`-sourced display.
- No "4 min → 30 sec → 2 min in 3 seconds" pattern — worst-case visible swing inside any 5-second window stays within a single bucket boundary (or, if monotonic, no upward motion at all).
- First-3-seconds-of-beat shows the decoder placeholder per design-log/009, not "0:00" or a giant number.

## Trade-offs

- **Slower mid-beat reactivity.** Session-wide average means a genuine speedup is reflected gradually rather than instantly. Counter: the alternative is the current volatility; users overwhelmingly prefer stable-but-conservative over wild-but-precise.
- **Optimistic during late-beat collapses.** If the link dies at 95% complete, ETA still reads the session average, lying low. Counter: handled by future stall-detector (Q3); ETA's job is not stall detection.
- **Two sources of truth for "rate."** Live speed (Smooth, jittery) and ETA-baseline rate (SessionAverage, stable). Risk: user compares them and notices disagreement. Counter: that disagreement is *information* — "right now I'm faster than my session trend." We can add a tiny tooltip in a follow-up; not required for v1.
- **Stale state across resumes.** A crash mid-pull resumes from `bytes_done > 0` but `SessionAverage` would start fresh from elapsed=0 on the new beat. ETA briefly inaccurate until enough samples accumulate. Acceptable; matches first-N-seconds placeholder handling.

## Out of scope (follow-ups)

- **Stall detection.** "No bytes_done growth in N seconds" UI hint; would belong in a separate small log.
- **Hybrid weighting.** Session + recent blend if session-wide alone reads as too sluggish post-ship.
- **Per-beat history.** Predicting "this push usually takes ~3 minutes" from previous sessions, parallel to [[027-saving-worlds-prep-eta]]'s prep/wrap history. Reasonable extension once #027's history substrate exists; would let ETA show a prediction even before the first byte flows.

## Implementation Results — 2026-05-29

Shipped Phases A–C. Phase D (live smoke against the user's repro) deferred — pending a real session.

**Landed entirely in the `projection` module + a one-line frontend swap. The `progress` adapter and ticker were not touched.**

### Deviation from §Q8 — no `SessionAverage` on `Stream`

§Q8 put `SessionAverage` on `progress.Stream`, reasoning the ticker owns the beat-start clock. Avoided: `progress.Tick` already carries cumulative `Elapsed`, and the projection already resets per beat and knows `BytesTotal`/`BytesDone`. So the projection computes the beat-wide average itself. Net effect: zero changes to the progress adapter/ticker, no new `Stream` field, no log-line churn.

- `viewmodel.go` — added `EtaSeconds int64` (`json:"etaSeconds"`).
- `projection.go` — three beat anchors on the struct (`etaBeatStarted`, `etaBeatElapsed`, `etaBeatBytes`); `resetEtaBeat()` called from `onStateChanged` (every stage change) and the `PlanInfo` fold; `etaFromSessionAvg(elapsed, done)` called in `onTick`.
- `frontend/src/ritual-app.ts` — `etaSub` now `vm.etaSeconds <= 0 ? placeholder : formatEta(vm.etaSeconds)`. Deleted the local `snapEta` (no longer used; the Go value is already integer seconds and monotone). Bindings regenerated via `task gui:bindings`.

### Deviation from §Q2 — delta form, not absolute

§Q2 wrote `rate = bytes_done / elapsed_since_beat_start`. Implemented as **delta-from-anchor**: `(done - etaBeatBytes) / (elapsed - etaBeatElapsed)`. Reason: on resume a beat can start with `done > 0` at `elapsed ≈ 0`, and the absolute form divides by ~zero → infinite rate. The delta form matches the frontend's existing resume handling (`transferStartBytes`) and is correct for the fresh-beat case too (anchor bytes = 0).

### Deviation from §Q9 — speed cell left on `DataAverage`

§Q9/§Design proposed switching the live speed cell `LogicalMbps` from `DataAverage` → `Smoothed`. **Not done.** `Smoothed` is the *wire* EWMA; switching would regress [[018-logical-rate-in-ui]], which deliberately makes that cell show the *logical* rate to match the bytes counter beside it. The user's complaint was the ETA swing — fixed here; the speed cell stays honest-and-logical. If the speed cell still reads as agro post-smoke, revisit as a separate logical-EWMA change (would need a new ticker flavour, not a source swap).

### Tests

`internal/gui/projection/projection_test.go` — 5 new tests, all green (`go test ./internal/gui/projection/` ok):
- first tick of a beat anchors → `EtaSeconds == 0` (placeholder, §Q5);
- second tick derives from beat-wide average (800 B / 200 Bps → 4s, §Q2);
- mid-beat slowdown does **not** raise the estimate (monotonic guard, §Q10);
- `PlanInfo` re-baselines and the guard releases for a larger plan (§Q4);
- empty plan keeps `EtaSeconds == 0` (no divide-by-zero, [[019-plan-info-delta]]).

Frontend `tsc --noEmit` clean; full `go build ./...` ok.

### Not done

- **Phase C story** — the existing `dial-composition.stories.ts` still computes its own ETA from `speedBps` via a local `snapEta` mock (story playground, not wired to `vm.etaSeconds`). Left as-is; a synthetic-swing story demonstrating stability is still a worthwhile follow-up.
- **Phase D live smoke** — needs a real ~880 MB session to confirm the monotone-downward read and the ±15% wall-clock target.
