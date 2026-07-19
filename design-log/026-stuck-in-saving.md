# 026 — Stuck in saving phase after backend reaches Done

**Date:** 2026-05-25
**Status:** Draft — hypothesis-driven; root cause not yet confirmed by frontend instrumentation
**Related:** [[017-stage-bucket-honesty]] (Phase taxonomy, `saving` bucket), [[020-lit-render-purity]] (Lit reactivity contract — earlier work in this area), [[018-logical-rate-in-ui]] (under-slot speed/ETA wiring), [[025-drop-redundant-exists-gate]] (same log, unrelated cause).

## Background

User repro 2026-05-25, session `20260525211546.log`:

- Session reached `Unlocking → Done` and `lifecycle.StatusChanged{Status: Done}` at 21:22:41. Logged cleanly:
  ```
  [21:22:41] Unlocking → Done
  [21:22:41] status: done
  ```
- Lock released both sides. Retain GC ran. No errors anywhere in the chain.
- Two `progress.Tick` events fired *after* Done (t=416s, t=417s) — but the projection's `onTick` is gated on `pipelineStage` (`projection.go:151-165`), which `onStatusChanged{Done}` clears (line 207). So those Ticks should be no-ops.
- Eleven minutes later (`[21:33:06] stop requested`) the user hit Stop because the dial was still showing the saving-phase visual at 100% arc fill.

Unit test `TestProjection_StatusDone_ResetsToIdle` (projection_test.go:220-230) covers the exact transition and passes — Phase resets to `PhaseIdle`, BytesTotal/Done/ErrorText all cleared.

## Problem

Backend produced the correct terminal event; frontend rendered a stuck saving-phase visual at 100%. Either:

- **(A)** `lifecycle.StatusChanged{Done}` never reached `projection.fold`. Possible cause: non-blocking event bus (`adapters/eventbus.go:46-49`) dropped it under subscriber back-pressure.
- **(B)** Projection folded `StatusChanged{Done}` and emitted an Idle ViewModel, but `wailsViewEmitter` failed to deliver it. Possible cause: latest-wins coalescing race in `cmd/gui/main.go:438-463`.
- **(C)** Wails IPC delivered the Idle ViewModel but the frontend's render didn't reflect it. Possible causes: Lit reactive-state stickiness in `ritual-dial.ts` GSAP-driven animations (`updated()` at line 256-272), or the `<rune-decoder>` label element caching the previous decode result.

## Questions and Answers

**Q1.** Backend logs confirm `status: done` was published. Did the projection consume it?
**A.** Unconfirmed without instrumentation. The non-blocking bus (`eventBus.Publish` line 38-51) drops on `select { case ch <- evt: default: }`. Projection's subscriber buffer is `NewEventBus(4096)` from `cmd/gui/main.go:189` — much larger than the ~1700 events this session generated, so drop on backpressure is **unlikely** but not proven.

**Q2.** Did the projection emit an Idle ViewModel?
**A.** Unit test proves it would *for the same event sequence under blocking-bus semantics*. But the production bus is non-blocking, and `projection.Run` → `emitter.Emit` is a tight serial loop — both calls happen, in order, on the same goroutine. Hard to lose unless the bus dropped (Q1).

**Q3.** Did `wailsViewEmitter` deliver the Idle snapshot?
**A.** The emitter is latest-wins (main.go:446-463): `pending.Store(&vm)` + non-blocking `wake <- struct{}{}` signal. The `loop()` consumes the wake and calls `pending.Swap(nil)`. Trace:
  - Emit#1 (PhaseSaving from `StateChangedInfo{Done}` fold, no state mutation but `fold` returns true at line 137 → emits unchanged state): pending=saving, wake queued.
  - Emit#2 (PhaseIdle from `StatusChanged{Done}` fold): pending=idle (overwrites saving), wake queued or dropped (if already queued).
  - loop: Swap → idle (latest wins). `a.Event.Emit("ritual:view", idle)`.

  Coalescing-as-designed; idle should be the *last* delivered VM. Unless the loop was blocked inside a prior `a.Event.Emit` and the Wails IPC was slow enough that... still ends with idle delivered. Architectural review does **not** find a way to lose the final Idle here.

**Q4.** Did the frontend's `onView` handler fire with the Idle ViewModel?
**A.** Unconfirmed. `frontend/src/wails-api.ts:21-23` subscribes via `Events.On("ritual:view", ...)`. Wails v3 alpha on Windows has had spurious-event-delivery issues in the past; unverified in this codebase. **Best path to confirm: one-line `console.log` in `applyVm` (ritual-app.ts:211) capturing every received VM.**

**Q5.** If `applyVm` fired with PhaseIdle, why didn't the dial show "Start" + play glyph?
**A.** This is the strongest **(C)**-class suspect:

  - `derive()` (ritual-app.ts:285-325) for PhaseIdle returns `state="idle", glyph="play", label="Start", arc=0, sub=""`.
  - `<ritual-dial>` receives these via property binding (line 363-369).
  - `ritual-dial.updated()` (line 256-272) runs `applyZoom(true)` when `state` changes and `morphTo("play")` when `glyph` changes — both GSAP tweens with `overwrite: "auto"` / `overwrite: true`.
  - The label is rendered via `<rune-decoder .text=${this.label}>` (line 374-376). If `rune-decoder` does not properly react to a `.text` property change (e.g. it caches the prior decode and only updates on `connectedCallback` or `firstUpdated`), the visible label stays "Wrapping up" even when the underlying property is "Start".

**Q6.** Has `rune-decoder` been audited for `.text` reactivity?
**A.** [[020-lit-render-purity]] swept eight Lit render purity issues but did not specifically validate `rune-decoder` re-decodes on `.text` change. Audit gap.

**Q7.** Why does PhaseSaving's "Wrapping up" override land while PhaseIdle's "Start" doesn't?
**A.** Both flow through the same `derive()` → `<ritual-dial label=...>` → `<rune-decoder .text=...>` chain. If reactivity to `.text` is broken, the most-recent successfully-decoded label sticks. The "Wrapping up" decode would have completed at the moment bytes hit 100%, before the Idle emission. After that, further `.text` updates do nothing → "Wrapping up" stays visible.

**Q8.** Why is the arc stuck at 100% if Phase reset to Idle (where arc=0)?
**A.** Same reactivity concern in `<ritual-dial>` itself if `.arc` updates don't trigger `dashOffset` recomputation. Less likely than the rune-decoder case because arc is a plain `@property({ type: Number })` (line 68), which Lit reacts to natively without bespoke decode logic.

**Q9.** Could the bug be (A) — bus drop?
**A.** Possible but unlikely. The 1628 ops + ~70 transition events in this session are well under the 4096 buffer. A path to confirm: log subscriber-buffer high-watermark inside `eventBus` for one session. Phase B verification, not Phase A.

**Q10.** Could the bug be (B) — `wailsViewEmitter` race?
**A.** Code review of the latest-wins emitter does not find a way to lose the final Idle VM. The race patterns I traced all converge on "the most recent `pending.Store` wins, signalled by *at least one* wake." If neither side fires `wake`, the loop sleeps, but the next Emit always (re-)signals.

**Q11.** Commit to a primary suspect.
**A.** **(C) — `<rune-decoder>` not reacting to `.text` change in the saving → idle transition.** Justification:
  - Unit-tested backend path is the most verified.
  - `wailsViewEmitter` review finds no delivery hole.
  - 020 explicitly cited render-purity holes in custom elements; rune-decoder was on the perimeter of that sweep and is the only element on the label dispatch path with bespoke text-decode logic.
  - The user-visible stickiness ("got up to 100 and just sitting there") is exactly the shape of "decoder finished a decode and never restarted."

**Q12.** *Post-draft observation, 2026-05-25.* User reports the stuck-saving state **eventually self-resolved** ("the save finished now, idk what happened there"). This invalidates the pure-(C) hypothesis: a `rune-decoder` reactivity bug would be permanent, not eventually-consistent.
**A.** Revised primary suspect: **delayed delivery, not lost delivery.** The Idle ViewModel reached the frontend *eventually* — minutes late. Two new candidate causes:
  - **(D) Wails IPC backpressure.** `a.Event.Emit("ritual:view", *vm)` on Windows + WebView2 may queue under sustained log-event throughput; the log emitter shares the same Wails event pipeline (`logsWindow.EmitEvent("log:line", line)`, main.go:474-479) and emits one event per log line. Session generated ~4500 log lines; the log:line emissions could be saturating the IPC channel, delaying ritual:view delivery by minutes.
  - **(E) Browser tab inactive / requestAnimationFrame suspended.** If WebView2 throttles when the window is unfocused, Lit's scheduled updates pause; coming back into focus drains them. Worth testing: does the un-stick correlate with the user re-focusing the window?
  Updated investigation priority:
  1. Phase B layer-3 instrumentation (frontend `applyVm` console.log with timestamps) becomes the *highest-value* diagnostic — confirms whether Idle arrives late vs not at all, and timestamps the "eventual" delivery.
  2. Phase A (`rune-decoder` audit) becomes lower-priority, since the symptom is not "stuck forever" but "stuck for minutes."
  3. New: instrument `wailsViewEmitter.loop()` to log queue-depth and IPC-emit duration; if loop iterations themselves are slow (≥seconds), (D) is confirmed.
  4. Test (E) by reproducing with the window deliberately kept focused vs. background.

## Design

Phase A is **instrumentation-light, fix-first under hypothesis (C)**: audit and fix `rune-decoder` `.text` reactivity. If repro persists after the fix, fall back to instrumented investigation per Phase B.

### Phase A — Fix the suspect

1. **Audit `rune-decoder.ts`** for `.text` reactivity:
   - Confirm `@property() text` is declared (not `@state`).
   - Confirm `willUpdate(changed)` or `updated(changed)` checks `changed.has("text")` and re-kicks the decode tween.
   - If the decode is keyed on `connectedCallback` / `firstUpdated`, that's the bug — move to a property-change hook.
2. **Test it**:
   - Add `rune-decoder.test.ts` case: render with `text="A"`, await one frame, mutate `.text = "B"`, await decode-duration, assert rendered shadow-DOM text contains "B".
   - Add Storybook story: button that toggles `.text` between two values; visually confirm the decode re-runs.
3. **Verify the user repro**: re-run a full session and confirm the dial returns to "Start" + play glyph after a successful Done.

### Phase B — fallback: instrument and confirm

If Phase A doesn't resolve the repro, add **temporary** logging on three layers (all behind a `// REMOVE AFTER 026` comment so we strip them in a follow-up):

1. **Projection** (`internal/gui/projection/projection.go:204`): `log.Printf("projection: status=%s reset to Idle", e.Status)` inside the `Idle, Done, Dismissed` case. Confirms (A).
2. **wailsViewEmitter** (`cmd/gui/main.go:461`): `log.Printf("emit ritual:view phase=%s stage=%s", vm.Phase, vm.Stage)` before `a.Event.Emit`. Confirms (B).
3. **Frontend `applyVm`** (`frontend/src/ritual-app.ts:211`): `console.log("applyVm", vm.phase, vm.stage, vm.bytesDone, vm.bytesTotal)`. Confirms (C).

Reproduce. Read logs. Pinpoint the boundary at which the Idle state stops propagating. Then write the Phase C fix.

### Phase C — fix the confirmed cause (placeholder)

To be filled in based on Phase B findings. Most likely: corrective Lit reactivity wiring in the affected element.

## Implementation Plan

**Phase A (fix on hypothesis):**

1. Read `frontend/src/ui/rune-decoder.ts` (or wherever `<rune-decoder>` lives).
2. Diagnose `.text` reactivity per Q11.
3. Add the test + story.
4. Reproduce.

**Phase B (instrumentation, only if A fails):**

1. Add the three diagnostic logs.
2. Reproduce, capture session log + DevTools console.
3. Triangulate.

**Phase C (final fix):**

1. Targeted change based on Phase B evidence.
2. Strip Phase B instrumentation.
3. Regression test.

## Verification

- After a full successful session ending in `status: done`, the dial visibly returns to:
  - state: idle (CSS attr `state="idle"`)
  - label: "Start"
  - glyph: play (▶)
  - arc: 0 (full circle empty)
  - sub: empty
- No "Wrapping up" or "Almost done" text remains visible.
- Repro a second session immediately after — Start affordance fires cleanly.

## Trade-offs

- **Risk of wrong hypothesis.** If (C) is wrong and the bug is actually (A) or (B), Phase A burns audit-cycle time. Mitigation: Phase A audit on `rune-decoder` is independently valuable — if it does have a reactivity bug, fixing it is a net win even if it doesn't explain this specific stuck state.
- **Skipping instrumentation up-front.** User declined initial instrumentation. Phase B remains available as a fallback; the cost is one repro cycle.
- **Cost of being wrong is bounded** — the dial is stuck on a terminal screen, no data loss, recoverable by app restart.

## Open follow-up — relationship to #025

#025 (drop redundant `Exists` gate) shares the same log file but is causally independent — Push completed cleanly and `status: done` was published well before the user noticed the stuck state. #025 shortens the Pushing wait; it does not fix the terminal-state visual bug captured here.
