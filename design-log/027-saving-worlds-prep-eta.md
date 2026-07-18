# 027 — "Saving worlds" copy + Prep/Wrap ETA from session history

**Date:** 2026-05-25
**Status:** Draft
**Related:** [[007-hig-ux-coherence]] (single-dial copy table), [[009-telemetry-hierarchy]] (sub-line digit/placeholder rules), [[017-stage-bucket-honesty]] (Phase taxonomy; PhaseWrapping = `unplug + "Wrapping up…"`), [[018-logical-rate-in-ui]] (logical-rate wiring — transfer ETA stays separate), [[028-transfer-eta-stability]] (companion log for download/upload ETA, different data source).

## Background

The dial today shows opaque copy for two beats where nothing visibly moves:

| Phase           | Stage signal               | Glyph     | Label          | Sub          |
|-----------------|----------------------------|-----------|----------------|--------------|
| `PhasePreparing`| Acquiring → Running pre-Ready | brain-cog | "Spinning up"  | "Almost live"|
| `PhaseWrapping` | ServerStopping → Committing  | unplug    | "Spinning down"| "Going offline" |

Two real problems:

1. **Copy taste.** User dislikes "Spinning down" — metaphor without information. "Saving worlds" reads as what's actually happening (Minecraft world-save before commit).
2. **No time signal during prep/wrap.** Both phases can run ≥30 seconds on a real NeoForge boot/shutdown, with zero progress feedback. Users don't know whether to wait or panic.

The transfer phases (`PhaseDownloading`, `PhaseSaving`) already get an ETA from the live byte counter (see [[028-transfer-eta-stability]] for the stability work there). Prep/Wrap don't have a byte counter — they're wall-clock operations whose duration depends on host hardware + mod count + world size. The only honest predictor is **history**: this user's previous boots/shutdowns on this machine.

## Problem

We need:

1. A new copy pair for `PhaseWrapping`: **"Saving worlds"** label + an ETA-like sub.
2. A history substrate: per-session record of how long the **prep beat** (Acquiring → ServerReady) and the **wrap beat** (ServerStopping → Done) actually took.
3. A derived "~Ns" string in the sub line of both `PhasePreparing` and `PhaseWrapping`, sourced from a trimmed mean of the last N sessions.
4. A first-run fallback (no history) so the dial doesn't show "~?s" or a placeholder.

Out of scope (handled elsewhere):

- Transfer-phase ETA volatility — see [[028-transfer-eta-stability]].
- `PhasePreparing` label rename (likely stays "Spinning up" — user only flagged "Spinning down"; confirm in Q1).
- Persisted progress across crashes within a single beat — ETA derives from completed history, not live extrapolation.

## Questions and Answers

**Q1.** Rename PhasePreparing too, or only PhaseWrapping?
**A.** User answer (this run): "saving worlds + eta based on prev." Only PhaseWrapping label changes explicitly. PhasePreparing ("Spinning up") stays; it's already verb-shaped and inoffensive. Open: revisit if "Spinning up" feels equally hollow once an ETA is sitting underneath it.

**Q2.** Sub-line format — "~12s" vs "About 12 seconds" vs "12s avg"?
**A.** "~12s" — user picked the rolling-average shape. Compact, fits the existing sub-line width (telemetry uses single-digit-cluster format), and the tilde itself signals "approximate." Format function: `\`~\${ceil(ms/1000)}s\`` for ms < 60_000; `\`~\${round(ms/1000/60)}m\`` for ≥ 60s. No "and Xs" composite.

**Q3.** History window — last N sessions, or last N days?
**A.** Last N sessions where N=10. Time-window cuts off rarely-used machines (vacation laptop). Trimmed mean: drop top + bottom 1 sample if N ≥ 5, so a single rogue cold-boot doesn't bias future ETAs.

**Q4.** First-run fallback (no history yet)?
**A.** Sub stays as today's literal copy: "Almost live" for PhasePreparing, **"Almost done"** for PhaseWrapping (new copy, paired with the new "Saving worlds" label). No "~?s" placeholder — honest absence of data beats a fake number.

**Q5.** Where does history persist?
**A.** New file `<root>/prep-history.json`. **Not** merged into `settings.json` — settings is user-edited config; prep-history is runtime-derived telemetry. Keeping them separate means a user can `rm prep-history.json` to "forget my machine" without losing port/memory config.

**Q6.** Schema?
**A.**
```json
{
  "version": 1,
  "samples": [
    {"runID": "host::ts-nanos", "startedAt": "2026-05-25T18:20:11Z",
     "prepMs": 14200, "wrapMs": 28800}
  ]
}
```
  - `prepMs` = wall ms from Acquiring entry to ServerReadyInfo publish.
  - `wrapMs` = wall ms from ServerStoppingInfo to lifecycle.StatusChanged{Done} publish.
  - Bounded to last 50 entries on write — ring-buffer FIFO. Even if N=10 for the average, 50-deep history lets us widen later without losing data.

**Q7.** What if a session fails mid-prep / mid-wrap?
**A.** No sample recorded. Only successful Done sessions feed the history. Failed sessions distort the timing (operator may have hung, killed the process, etc.) — garbage in, garbage out. If 5 out of 10 recent sessions failed, the average is just less stable; that's correct behaviour.

**Q8.** Live vs. derived — do we count up while the beat is running?
**A.** No. ETA derived from history is a *predicted* duration, not an elapsed counter. Showing it count down would imply we know better than we do (the real beat may finish in 8s when history said 12s). Static "~12s" is honest. The user reads it once at beat start and knows roughly what they're waiting for.

**Q9.** Where in the architecture?
**A.** New subsystem `internal/subsystems/preprundup/` (working name) with:
  - `History` type wrapping the JSON file (load/append/trim).
  - `Recorder` — subscribes to bus, watches Acquiring/ServerReady/ServerStopping/StatusChanged{Done} transitions, accumulates prepMs/wrapMs into a fresh sample, writes on Done.
  - `Estimator` — reads History, returns trimmed-mean prep/wrap ETAs as `(prepMs, wrapMs int64)`. Called by Projection at PhasePreparing / PhaseWrapping entry to populate two new ViewModel fields.

**Q10.** ViewModel surface?
**A.** Two new fields:
  - `PrepEtaMs int64` — populated when entering PhasePreparing; nonzero only if history has ≥2 samples.
  - `WrapEtaMs int64` — same, for PhaseWrapping.
  Frontend reads these in the `sub` slot of PhasePreparing / PhaseWrapping. Zero → fall back to "Almost live" / "Almost done."

**Q11.** Anchor moments for the timing measurement — what counts as "the prep beat"?
**A.**
  - Prep beat **start**: `StateChangedInfo{To: Acquiring}` arrives. (Acquiring is the first beat the user perceives as "wait" after pressing Start — Checking/Pulling are owned by the bytes-flowing PhaseDownloading.)
  - Prep beat **end**: `running.ServerReadyInfo` arrives.
  - Wrap beat **start**: `running.ServerStoppingInfo` arrives.
  - Wrap beat **end**: `lifecycle.StatusChanged{Done}` arrives.

**Q12.** What about a session where the user clicks Stop *during* PhasePreparing (server crashed during boot)?
**A.** No sample. wrapMs requires ServerStoppingInfo as its anchor; if the server never reached Ready, ServerStoppingInfo is never published. Out-of-scope failure path; history stays clean.

**Q13.** Decoder behaviour for the ETA sub-line?
**A.** Per [[009-telemetry-hierarchy]]: digit-presence rule means "~12s" stays plain (digits = stable), "Almost live" decodes (no digits = decoder-jitter). Existing dispatch in the sub renderer handles this for free — no decoder code touched.

## Design

### Frontend copy

```ts
// frontend/src/ritual-app.ts — PHASE_VIEW table
[Phase.PhaseWrapping]: {
    state: "final", glyph: "unplug",
    label: "Saving worlds",  // was: "Spinning down"
    underSlot: null,
    arc: () => 0,
    sub: (vm, _ctx) => vm.wrapEtaMs > 0 ? formatEtaApprox(vm.wrapEtaMs) : "Almost done",  // was: () => "Going offline"
},

// new helper next to formatEta() in the existing time-format module:
function formatEtaApprox(ms: number): string {
    if (ms < 60_000) return \`~\${Math.ceil(ms / 1000)}s\`;
    return \`~\${Math.round(ms / 60_000)}m\`;
}
```

PhasePreparing entry gets the same treatment for symmetry once PrepEtaMs is wired:

```ts
[Phase.PhasePreparing]: {
    state: "prep", glyph: "brain-cog",
    label: "Spinning up",
    underSlot: null,
    arc: () => 1,
    sub: (vm, _ctx) => vm.prepEtaMs > 0 ? formatEtaApprox(vm.prepEtaMs) : "Almost live",
},
```

### Backend — history file

`<root>/prep-history.json`:

```go
// internal/subsystems/preprundup/history.go
type Sample struct {
    RunID     string    \`json:"runID"\`
    StartedAt time.Time \`json:"startedAt"\`
    PrepMs    int64     \`json:"prepMs"\`
    WrapMs    int64     \`json:"wrapMs"\`
}
type File struct {
    Version int      \`json:"version"\`
    Samples []Sample \`json:"samples"\`
}

const (
    historyVersion = 1
    historyCap     = 50
    historyWindow  = 10  // samples used for the trimmed mean
)
```

### Backend — recorder

```go
type Recorder struct {
    bus   ports.EventBus
    store HistoryStore  // load + atomic write
    cur   *Sample       // in-flight session, nil between runs
    prepStart, wrapStart time.Time
}

// Subscribe and dispatch:
//   StateChangedInfo{To: Acquiring}   -> r.cur = &Sample{RunID, StartedAt: now}; r.prepStart = now
//   ServerReadyInfo                   -> r.cur.PrepMs = since(r.prepStart)
//   ServerStoppingInfo                -> r.wrapStart = now
//   lifecycle.StatusChanged{Done}     -> r.cur.WrapMs = since(r.wrapStart); store.Append(r.cur); r.cur = nil
//   lifecycle.StatusChanged{Failed}   -> r.cur = nil  (discard)
```

### Backend — estimator

```go
func (e *Estimator) PrepEta() time.Duration { /* trimmed mean of last N PrepMs */ }
func (e *Estimator) WrapEta() time.Duration { /* trimmed mean of last N WrapMs */ }
```

### Projection wiring

Projection takes a new optional `Estimator` dependency. On PhasePreparing/PhaseWrapping entry:

```go
case ritual.StageAcquiring:
    p.state.Stage = StageDownloading
    p.state.Phase = PhasePreparing
    if p.estimator != nil {
        p.state.PrepEtaMs = p.estimator.PrepEta().Milliseconds()
    }
// similarly for ServerStoppingInfo -> PhaseWrapping
```

Both fields zero by default; first-run fallback handled in the frontend.

## Implementation Plan

**Phase A — frontend copy-only change.** Land the rename + first-run fallback strings so the user-visible label updates immediately, even before the history substrate exists. ViewModel fields `PrepEtaMs`/`WrapEtaMs` default to 0, fallback string shows.

1. Edit `PHASE_VIEW[PhaseWrapping].label` → `"Saving worlds"`, `.sub` → fallback `"Almost done"`.
2. Add `formatEtaApprox` helper alongside `formatEta`.
3. Sanity: storybook snapshot of the wrap beat shows the new copy.

**Phase B — history substrate.**

1. New package `internal/subsystems/preprundup/` with `History`, `Recorder`, `Estimator`.
2. JSON file under `<root>/prep-history.json`, atomic write (temp + rename).
3. Wire in `cmd/gui/main.go`: load history at startup, build Recorder, subscribe to bus, build Estimator, pass to projection.
4. Tests: append + trim, trimmed mean correctness, empty-history returns zero duration.

**Phase C — ViewModel + projection wiring.**

1. Add `PrepEtaMs int64`, `WrapEtaMs int64` to `internal/gui/projection/viewmodel.go`.
2. Populate in `onStateChanged{Acquiring}` and on `ServerStoppingInfo` per Design.
3. Frontend uses the fields in PhasePreparing / PhaseWrapping sub functions.
4. Test: PhasePreparing entry with mocked Estimator returning 12s → sub reads "~12s"; with empty history → sub reads "Almost live".

**Phase D — smoke.**

1. Fresh `<root>/prep-history.json` absent: run two full sessions. First shows fallback, second shows "~Ns" derived from the first.
2. Manually edit history to inject a 60s sample: sub displays "~1m".

## Verification

- PhaseWrapping label reads "Saving worlds" everywhere (live app + storybook).
- After ≥2 successful sessions on a fresh machine, PhasePreparing sub reads "~Ns" and PhaseWrapping sub reads "~Ns" where N is the trimmed-mean of recorded prepMs/wrapMs.
- First-run-ever shows fallback strings ("Almost live" / "Almost done"), no broken "~0s" or "~NaNs".
- Failed sessions don't pollute the history file (verify via file inspection after `kill -9` mid-prep).

## Trade-offs

- **History pollution risk.** If a user has wildly inconsistent hardware moments (e.g. laptop on battery vs. plugged in), the trimmed mean is meh. Mitigation: not solved here; could later condition on hostname or wall-power state. Out of scope.
- **Static ETA "lies."** ETA stays "~12s" for the whole beat even if the actual beat is taking 30s. Counter: counting down would imply we know better; sitting on a static prediction matches Software Update behaviour and is honest about its uncertainty (the tilde).
- **New file under `<root>/`.** One more thing to back up / delete. Reasonable cost.
- **Coupling to specific bus events.** Recorder hard-codes `Acquiring/ServerReady/ServerStopping/StatusChanged{Done}` anchors; if the stage taxonomy changes (e.g. [[017-stage-bucket-honesty]] follow-up), the recorder needs updating. Acceptable — same lock-in the projection has.
