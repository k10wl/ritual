# 058 — Prep/Wrap ETA: backend-driven countdown + download-stage total

**Date:** 2026-08-28
**Status:** Draft
**Related:** [[027-saving-worlds-prep-eta]] (supersedes its Q8 static-ETA
decision and Q10 ViewModel surface; Q1–Q7, Q9, Q11–Q13 unchanged and still
govern the history substrate), [[028-transfer-eta-stability]] (the
`EtaSeconds` field this log extends), [[050-chained-launch-progress-overflow-and-missing-icons]]
(§C "client is only a projection" — `UptimeSeconds` backend-ticker pattern
this log reuses), [[055-operational-functional-root-split]] (CONTROL root
= `config.RootPath`, where `prep-history.json` lives).

## Background

[[027-saving-worlds-prep-eta]] designed a history-backed ETA for the two
invisible session beats — prep (`Acquiring → ServerReady`) and wrap
(`ServerStopping → Done`) — but was never implemented (confirmed: no
`internal/subsystems/preprundup/`, no `prep-history.json`, no
`PrepEtaMs`/`WrapEtaMs` anywhere in the tree). Circling back on it now, with
three changes to what 027 decided. Per this project's design-log
methodology, **027 stays unedited** (design logs are immutable once
written) — this log records the revised decisions and supersedes the
pieces it touches.

## Problem

027's plan is still mostly right (history file, trimmed mean, recorder/
estimator split), but three things need to change before implementation:

1. 027 §Q8 decided the ETA sub-line is **static** ("~12s", never counts
   down) because "showing it count down would imply we know better than we
   do." New direction: show it as an **actual ticking countdown**, and it
   must be **backend-driven** — the frontend must not run its own
   `setInterval`/clock, matching the existing `UptimeSeconds` pattern
   (design-log/050 §C) where the backend pushes a fresh value once a
   second and the frontend only ever formats whatever it was last given.
2. The download stage's own ETA should fold in the upcoming prep estimate,
   so what's shown while bytes are still flowing reads as "time until
   playable," not just "time until bytes land."
3. Confirm where the history file lives, using the project's own
   terminology rather than 027's design-log/055 citation: "functional
   folder is where logs and settings and lock are stored" — i.e.
   `config.RootPath`, a plain pretty-printed JSON file beside
   `settings.json`.

## Questions and Answers

**Q1 (revises 027 §Q8).** Static or ticking?
**A.** Ticking countdown, for both `PhasePreparing` and `PhaseWrapping`.
Backend-driven: `Projection.Run`'s existing 1Hz `uptimeTicker` (design-log/050)
is extended to also decrement whichever of `PrepEtaSeconds`/`WrapEtaSeconds`
is active and emit it — no second ticker, and no frontend clock of any
kind. 027's original objection to counting down is accepted as a known,
deliberate trade-off now, not a blocker — matches Software Update /
installer countdown conventions.

**Q2 (new).** Does the download stage's own ETA fold in the prep
estimate?
**A.** Yes. `PhaseDownloading`'s existing `EtaSeconds` (design-log/028,
beat-wide average of the byte counter) gets the current prep estimate
added on top for the duration of the download beat. Only for
`FlowSession` (the flow that actually boots a server afterward) —
`FlowDownload`'s standalone download never proceeds to a prep beat, so it
must not carry the addend.

**Q3 (reconfirms 027 §Q5, corrects its root citation).** History file
location?
**A.** `config.RootPath` — the fixed root holding `settings.json` + `lock`
+ root `logs/` (`cmd/gui/main.go`'s `opRoot`), not the movable `WorkRoot`
content root. Same physical location 027 §Q5 already picked (027 cited it
as "`<root>/`"); this just confirms the term against current code. Plain
pretty-printed JSON (`json.MarshalIndent`), atomic write mirroring
`domain.Settings.Save`'s temp+fsync+rename+chmod. Not merged into
settings.json — a user can delete `prep-history.json` to "forget my
machine" without losing config.

**Q4 (new).** Flow scope for the recorder?
**A.** `FlowSession` only, gated explicitly via `FlowStartedInfo{Flow:
FlowSession}` — not inferred from which stages happen to fire.
`StateChangedInfo{To: Acquiring}` also fires during `FlowUpload`
(design-log/031's Probing→Acquiring→Committing chain), so an implicit gate
would misrecord upload timing as prep timing. A sample is appended only
when both `PrepMs` and `WrapMs` came out non-zero in the same run — a
partial beat (e.g. `FlowLocalSession`, which has no Acquiring anchor at
all) records nothing rather than a half sample, consistent with 027 §Q7's
"no sample on failure" stance.

## Design

### ViewModel surface (supersedes 027 §Q10)

```go
// internal/gui/projection/viewmodel.go
PrepEtaSeconds int64 `json:"prepEtaSeconds"` // ticks down while PhasePreparing; 0 = no estimate/not active
WrapEtaSeconds int64 `json:"wrapEtaSeconds"` // ticks down while PhaseWrapping; 0 = no estimate/not active
```

`EtaSeconds` (existing, design-log/028) gains the prep addend during
`PhaseDownloading` under the Q2 gate — no new field for the combined
number; the transfer-ETA field just carries more information for that one
beat, formatted by the same frontend `formatEta` call sites unchanged.

### Backend — history substrate (as 027 §Q6/§Q9, unchanged)

`internal/subsystems/preprundup/`:

- `history.go` — `Sample{RunID, StartedAt, PrepMs, WrapMs}`, `File{Version,
  Samples}`, `Store` (`Load`/`Append`, atomic write, `historyCap = 50`).
- `estimator.go` — `Estimator{PrepEta, WrapEta}`, trimmed mean
  (`historyWindow = 10`, drop top/bottom 1 sample when N ≥ 5).
- `recorder.go` — `Attach(ctx, bus, store) func()`, same shape as
  `internal/subsystems/notify.Attach`: tracks `activeFlow` via
  `FlowStartedInfo`, anchors `prepStart`/`wrapStart` off
  `StateChangedInfo{To: Acquiring}` / `ServerStoppingInfo`, records
  `PrepMs`/`WrapMs` off `ServerReadyInfo` / `lifecycle.StatusChanged{Done}`,
  discards on `StatusChanged{Failed}`.

### Backend — projection wiring

```go
// internal/gui/projection/projection.go
type Estimator interface {
    PrepEta() time.Duration
    WrapEta() time.Duration
}
```

`Projection` takes this as a new optional constructor param (nil-safe,
same shape as the existing `AddressProvider`). New fields:
`estimator Estimator`, `prepEtaEstimate time.Duration`,
`prepEtaStartedAt time.Time`, `wrapEtaEstimate time.Duration`,
`wrapEtaStartedAt time.Time`.

- `FlowStartedInfo{Flow: FlowSession}` → snapshot
  `p.prepEtaEstimate = p.estimator.PrepEta()` (0 if `estimator == nil` or
  no history).
- `onTick`'s `StagePulling` branch, only when `p.state.Phase ==
  PhaseDownloading && p.activeFlow == ritual.FlowSession`: after computing
  the normal beat-wide `EtaSeconds`, add
  `int64(p.prepEtaEstimate.Seconds())` on top.
- `StateChangedInfo{To: Acquiring}` (entering `PhasePreparing`):
  `p.prepEtaStartedAt = time.Now()`; seed
  `p.state.PrepEtaSeconds = int64(p.prepEtaEstimate.Seconds())`.
- `ServerReadyInfo`: `p.state.PrepEtaSeconds = 0`; snapshot
  `p.wrapEtaEstimate = p.estimator.WrapEta()` for the wrap beat about to
  follow (fetched here rather than at `ServerStoppingInfo` so a
  first-history-write race can't leave it stale — matches how `prepEtaEstimate`
  is fetched a full beat ahead in Q2).
- `ServerStoppingInfo` (entering `PhaseWrapping`): `p.wrapEtaStartedAt =
  time.Now()`; seed `p.state.WrapEtaSeconds =
  int64(p.wrapEtaEstimate.Seconds())`.
- `uptimeTicker`'s existing 1Hz case in `Run` (design-log/050) gains two
  more branches alongside the uptime one:
  ```go
  case p.state.Phase == PhasePreparing && p.prepEtaStartedAt is set:
      remaining := p.prepEtaEstimate - time.Since(p.prepEtaStartedAt)
      p.state.PrepEtaSeconds = max(0, int64(remaining.Seconds()))
  case p.state.Phase == PhaseWrapping && p.wrapEtaStartedAt is set:
      remaining := p.wrapEtaEstimate - time.Since(p.wrapEtaStartedAt)
      p.state.WrapEtaSeconds = max(0, int64(remaining.Seconds()))
  ```
  Floors at 0 rather than going negative; once the real event
  (`ServerReadyInfo`/`StatusChanged{Done}`) fires the beat ends anyway.
- `onStatusChanged`'s existing full-`ViewModel` resets on
  Idle/Done/Dismissed/Failed already zero every field, including the two
  new ones — no extra cleanup needed there.

### Frontend (supersedes 027's Phase-A copy-only plan)

```ts
// frontend/src/ritual-app.ts PHASE_VIEW table
[Phase.PhaseWrapping]: {
    state: "final", glyph: "unplug",
    label: "Saving worlds",           // was: "Spinning down" (027's copy change, still pending)
    underSlot: null,
    arc: () => 0,
    sub: (vm) => vm.wrapEtaSeconds > 0 ? formatEta(vm.wrapEtaSeconds) : "Almost done",
},
[Phase.PhasePreparing]: {
    state: "prep", glyph: "brain-cog",
    label: "Spinning up",
    underSlot: null,
    arc: () => 1,
    sub: (vm) => vm.prepEtaSeconds > 0 ? formatEta(vm.prepEtaSeconds) : "Almost live",
},
```

Reuses the existing `formatEta` helper (already used for `uptimeSeconds`/
`etaSeconds`) rather than a new `formatEtaApprox` — 027's tilde-prefixed
"~12s" made sense for a static estimate; a live countdown reads better in
the same `MM:SS`/`Ns` shape the rest of the dial already uses. No new
frontend helper, no client-side interval — the sub function is a pure
render of whatever `vm.prepEtaSeconds`/`vm.wrapEtaSeconds` the backend
last pushed, same shape as `PhasePlaying`'s `uptimeSeconds` sub already
uses today.

`PhaseDownloading`'s `sub` (`etaSub`) is untouched — it already renders
`vm.etaSeconds` directly, and Q2's addend is computed server-side into
that same field, so no frontend change is needed there at all.

### cmd/gui wiring

```go
// cmd/gui/main.go
historyStore := preprundup.NewStore()
estimator := preprundup.NewEstimator(historyStore)
stopPrepRecorder := preprundup.Attach(ctx, bus, historyStore)
// ...
proj := projection.New(bus, viewEmitter, addresses, estimator)
```

`stopPrepRecorder` added to the same shutdown sequence as `stopNotify`.

## Implementation Plan

**Phase A — history substrate.**
1. `internal/subsystems/preprundup/history.go`: `Sample`, `File`, `Store`
   (`Load`/`Append`, atomic write). Tests: append + trim to 50, load of a
   missing file returns empty, atomic-write survives a simulated failure
   (mirror `settings_test.go`'s `TestSettingsSave_*` cases).
2. `estimator.go`: trimmed-mean `PrepEta`/`WrapEta`. Tests: empty history →
   zero; <5 samples → plain mean; ≥5 samples → drops one high + one low.
3. `recorder.go`: `Attach`. Tests (mirror `notify_test.go`'s fake-bus
   shape): full session records a sample; `StatusChanged{Failed}` mid-prep
   records nothing; `FlowUpload`'s `Acquiring` does not start a prep timer;
   `FlowLocalSession` (no Acquiring) records nothing.

**Phase B — projection wiring.**
1. `Estimator` interface + constructor param + new state fields.
2. Fold-site changes per Design above (`FlowStartedInfo`, `onTick`,
   `StateChangedInfo{To: Acquiring}`, `ServerReadyInfo`,
   `ServerStoppingInfo`).
3. Extend `uptimeTicker`'s case in `Run`.
4. Tests: mocked `Estimator` returning fixed durations — `PrepEtaSeconds`
   seeded correctly at `Acquiring`, decrements across simulated ticks,
   floors at 0, clears at `ServerReadyInfo`; `WrapEtaSeconds` mirrors it;
   `EtaSeconds` during a `FlowSession` download beat includes the addend;
   a `FlowDownload` beat does not.

**Phase C — wiring + frontend.**
1. `cmd/gui/main.go`: build `Store`/`Estimator`, `Attach` the recorder,
   pass `Estimator` into `projection.New`.
2. `task gui:bindings` to regenerate `ViewModel` TS bindings with the two
   new fields.
3. `ritual-app.ts` `PHASE_VIEW` sub functions per Design (and land 027's
   still-pending "Saving worlds" label swap here, since it's the same
   `PhaseWrapping` entry).
4. Storybook: update/add stories exercising the ticking sub text for both
   phases.

**Phase D — smoke.**
1. Fresh machine (no `prep-history.json`): first full session shows
   "Almost live"/"Almost done"; second session shows a real countdown
   seeded from the first.
2. Manually seed history with a 30s prep sample; confirm the download
   stage's `EtaSeconds` is visibly ~30s higher than a plain transfer ETA
   for the same plan size, and that `PrepEtaSeconds` counts down from ~30
   once `PhasePreparing` is entered.

## Verification

- `PrepEtaSeconds`/`WrapEtaSeconds` visibly count down once per second in
  the live app, driven entirely by backend emits (confirm via DevTools:
  no `setInterval` in the frontend bundle touches these fields).
- After ≥2 successful `FlowSession` runs, the download beat's displayed
  ETA is higher than the raw transfer-only estimate by roughly the prep
  history average.
- `FlowDownload`/`FlowUpload`/`FlowLocalSession` runs never populate
  `PrepEtaSeconds`/`WrapEtaSeconds` and never add the addend to
  `EtaSeconds`.
- `prep-history.json` appears at `config.RootPath` beside `settings.json`,
  pretty-printed, capped at 50 samples, and is untouched by a failed run.

## Trade-offs

- Same history-pollution and "static-becomes-visible-lie" risk 027 §Trade-offs
  already named — a ticking countdown makes the lie more visible than a
  static "~12s" would (a real beat finishing early means the countdown
  visibly snaps to 0 early, or a slow beat means it sits at 0 for a while
  before the real event fires). Accepted per the countdown directive; the
  floor-at-0 behavior keeps it from ever going negative or looking broken.
- One more backend ticker branch inside an already-shared 1Hz loop —
  cheap, same "no-op when not applicable" cost `uptimeTicker` already
  pays.

## Implementation Results

Implemented (2026-08-28): `internal/subsystems/preprundup/{history,estimator,recorder}.go`
+ tests, `ViewModel.PrepEtaSeconds`/`WrapEtaSeconds`, projection wiring
(`Estimator` interface, `onFlowStarted`/`onPrepBeatOver`/
`seedPrepCountdownIfAcquiring`/`tickEtaCountdowns`), `cmd/gui/main.go`
wiring, and the `ritual-app.ts` sub-line + Storybook updates, all per the
Design above. `go build`/`go test`/`golangci-lint run` clean repo-wide,
`tsc --noEmit` clean, 183/183 frontend tests green.

**Deviation from Design — "just store last one" (user directive,
2026-08-28, live-testing session):** the Design section's history file
(`File{Version, Samples []Sample}`, 50-entry ring buffer) and Estimator
(trimmed mean over the last 10, dropping high+low when ≥5) are **not**
what shipped. On seeing the very first live session sit on the "Almost
live" fallback (correct — `prep-history.json` didn't exist yet — but a
natural moment to reconsider the mechanism), the user asked to simplify to
storing only the single most recently completed run:

- `File` is now `{Version int, Last *Sample}` — `Store.Append` overwrites
  `Last` instead of appending to a slice. No cap, no trim, no ring buffer.
- `Estimator.PrepEta()`/`WrapEta()` return `Last`'s field directly
  (0 if `Last == nil`) — no sorting, no window, no averaging.
- **Side effect (not just a simplification):** the original trimmed-mean
  estimator required ≥2 samples before returning anything non-zero, so a
  fresh machine needed *two* completed sessions before ever seeing a
  countdown. Last-one-only needs just **one**.
- Rationale (as given): simpler code, and a rolling average that blends
  several past runs is arguably less honest about *this* machine's
  *current* state than just using the immediately preceding run — a
  machine's actual boot time doesn't drift slowly across 10 samples, it
  jumps around per-hardware-state (plugged in vs. battery, background
  load), so an average isn't obviously better than the latest data point.
- Everything else in the Design — the two anchor events, the FlowSession
  gate, the backend-driven countdown ticker, the download-stage addend —
  is unchanged; only the history *storage/estimation* shape differs.
- Tests updated to match: `TestStoreAppend_OverwritesPreviousSample`
  replaces the trim-to-50 test; `TestEstimator_ReturnsLastSampleDirectly_NoAveraging`
  replaces the trimmed-mean tests. `recorder.go` and its tests were
  unaffected (recorder only ever calls `Append` once per completed run;
  it never depended on how many samples the store retains).

**Live-testing round 2 — fallback copy correction (2026-08-28):** a live
first-ever run correctly sat on "Almost live" (no `prep-history.json` yet
— verified via the actual `~/k10wl/ritualdev/` tree). An intermediate
attempt swapped the static "Almost live"/"Almost done" fallback for the
`·····` decoder-glitch placeholder (`formatEta(null)`, the same treatment
`etaSub` already uses for the download beat's first-tick grace window) on
the theory that literal readable text reads as "settled," not "no data."
**Rejected by the user** — "first run should say 'almost done'" — so the
static copy from 027 §Q4 stands as originally designed; `ritual-app.ts`
and the `ritual-dial.stories.ts` stories were reverted to it. Separately,
the user reported a live flicker (ticking countdown → a one-frame fallback
→ ticking resumed) that this static-copy revert does not explain by
itself; investigated against the actual session log
(`~/k10wl/ritualdev/logs/20260828155051.log`) — confirmed no duplicate
`ServerStopping`/`Acquiring` events, but the `Snap.String()` log formatter
(`internal/gui/projection/viewmodel.go`) never printed `PrepEtaSeconds`/
`WrapEtaSeconds`, so the exact tick-by-tick values from that run aren't
recoverable. Fixed the formatter to include `prepEta=`/`wrapEta=` (now also
`progress=`) so a future recurrence is directly diagnosable; root cause
unconfirmed as of this entry.

**Live-testing round 3 — combined download+prep ring fill (2026-08-28,
new scope beyond the original design):** user directive: the download
beat's ring should climb 0→80% (not 0→100%), handing off to the prep beat
climbing 80%→100%, driven by the real prep-history estimate — "instead of
initial almost done, send with stage transfer data for the initial state
of stage stored from json so we won't have jitter." Explicit constraints
given alongside: **never invent data** — no fabricated/default duration
when no real history exists, and the recorder must keep storing only
genuinely-measured samples (already true, unchanged) — and **the frontend
must stay a "dumb projection"**: all ratio/elapsed-fraction math for this
feature belongs on the backend, not the frontend (supersedes 050 §C's
general allowance for frontend ratio math — for this specific feature
only; `arcFromBytes`'s pre-existing bytesDone/bytesTotal math for
PhaseSaving is untouched, out of scope, still frontend-side as 050 §C
originally decided).

Implementation (`internal/gui/projection/`):
- `prepSplitPercent = 80` (const) + two pure helpers: `downloadProgress(fraction, hasPrepEstimate)` and `prepProgress(totalSeconds, remainingSeconds)`.
- `onTick`'s `StagePulling` branch now writes `ViewModel.Progress` directly (previously never touched during a real, non-empty transfer — see the existing `TestProjection_PlanInfoDuringPulling_...` test, still valid since it asserts the state *before* any Tick fires) — scaled to 80 when `p.prepEtaEstimate > 0` for a `FlowSession` run, else the legacy unscaled 0→100.
- `onPlanInfo`'s empty-delta branch anchors to `prepSplitPercent` instead of 100 under the same gate — no Tick will ever fire to correct an empty-delta beat, so this is the only anchor point.
- `seedPrepCountdownIfAcquiring` seeds `Progress` to exactly `prepSplitPercent` (elapsed fraction 0) in the same emit that starts the countdown — atomic with the phase flip, no gap.
- `StateChangedInfo{To: Running}` (fires immediately after Acquiring in every real run) no longer zeroes `Progress` — it used to, unconditionally, as part of clearing the stale download-beat `BytesDone`/`BytesTotal`/`Progress` trio. Left as-is it would have flashed the ring back to 0% for one frame before the next tick recomputed it — exactly the "jitter" the user asked to avoid. Now recomputes via `prepProgress` in place instead of zeroing.
- `tickEtaCountdowns`'s `PhasePreparing` branch recomputes `Progress` alongside `PrepEtaSeconds` every second.
- No new `PrepEtaTotalSeconds` ViewModel field (an earlier attempt added one so the frontend could compute the elapsed-fraction ratio itself, mirroring `arcFromBytes`'s existing bytesDone/bytesTotal pattern — **rejected**: "this calculation must be done purely on the backend side, frontend in just a dumb projection." Reverted; the field never shipped in a build.).
- Frontend: `ritual-app.ts` gained one `progressArc(vm) = clamp(vm.progress / 100)` — a unit conversion, not a ratio decision — used for both `PhaseDownloading` and `PhasePreparing`. `PhaseSaving` keeps `arcFromBytes` unchanged.
- Tests: `internal/gui/projection/projection_test.go` gained a `fakeEstimator` + `runProjectionWithEstimator` helper and seven new tests covering the split-scaled tick, the unscaled no-estimator case, the unscaled `FlowDownload` case (never capped at 80, since it never proceeds to a prep beat), the empty-delta anchor, the Acquiring seed (both with and without a real estimate), and the Running-doesn't-reset regression. All pass; full existing suite (`go test ./...`, `golangci-lint run ./...`, `tsc --noEmit`, 183/183 frontend tests) stays green.
