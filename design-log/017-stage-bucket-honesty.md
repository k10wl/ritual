# 017 — Stage bucket honesty: phase beats, ETA discipline, dismiss-to-idle

- **Status:** Draft
- **Date:** 2026-05-25
- **Area:** GUI / Projection / Lifecycle
- **Related:** [007 HIG one dial](007-hig-ux-coherence.md), [009 Telemetry hierarchy](009-telemetry-hierarchy.md), [013 Dialed GUI cutover](013-dialed-gui-cutover.md), [010 Run addresses](010-run-addresses.md)
- **Supersedes (in part):** 007 §State table (FAIL row, PREP/FINAL glyph rows)

## Background

GUI ships four visual dial states (`idle / prep / run / final` plus `fail` overlay) per 007. The orchestrator runs **eight** stages (`Checking → Pulling → Acquiring → Running → Committing → Pushing → Unlocking → Retaining`) plus server-lifecycle events inside `Running` (`ServerStartingInfo → ServerReadyInfo → ServerStoppingInfo → ServerStoppedInfo`).

`internal/gui/projection/projection.go:155-174` collapses the eight ritual stages into the four buckets. The frontend `ritual-app.ts:126-209` then renders one of five dial states from `vm.stage`.

The collapse is currently dishonest in three places.

## Problem

Three concrete leaks between runtime and UI:

### 1. Prep → Run boundary fires too early

`projection.go:163` flips `vm.stage = StageRunning` on `ritual.StageRunning` *entry* — before `ServerReadyInfo` fires. `ritual-app.ts:174-186` then hard-codes label `"Ready to play"` and the stop glyph, while the server is still booting (port not bound, address unreachable). User sees "Ready to play" + stop button while the server is in fact not yet ready.

### 2. Wrap-up boundary fires too late

When the user hold-stops, `running.ServerStoppingInfo` fires inside `StageRunning` (`ReadyLight=false`), but `vm.stage` stays `StageRunning` until the orchestrator advances to `Committing`. During this window the dial still shows "Ready to play" + stop glyph. The visible "stopping" beat is invisible.

### 3. ETA / phase copy lie outside bytes-flowing windows

`projection.go:204-216` carefully sets `vm.Label` to `"Snapshotting…"`, `"Releasing lock…"`, `"Pruning old refs…"`, `"Acquiring lock…"`, `"Checking…"` per ritual stage. `ritual-app.ts:167,193` ignores it and hard-codes `"Getting ready"` / `"Saving"`. Meanwhile the sub-line shows ETA even during phases where no bytes flow (apply, acquire, commit, unlock, retain), inventing time from stale rates.

Also: 007 §Q11 + §State table promised retry-from-failed (arc-preserving resume). `ritual-app.ts:213-216` actually calls `start()` (fresh run). UI says "try again", code starts fresh. Two-faced.

## Core principles

Steam and macOS Software Update both solve the heterogeneous-phase problem the same way:

1. **Each phase has a name**, and the name is the dominant signal.
2. **Determinate progress only while bytes flow.** Outside that, no number, no ETA.
3. **Indeterminate or plateau visuals** carry liveness during invisible work — never extrapolated numbers.
4. **No fake unified curve** spanning download → install → cleanup.

Apply that, plus 007's "one dial, four states" rule, and the eight stages collapse honestly.

## Design

### Phase taxonomy

New `Phase` field on `ViewModel`. Distinct from `Stage` (the dial-state bucket): `Phase` is the finer-grained sub-state the frontend uses to pick glyph + sub copy. The dial color/bucket still maps from `Stage`.

```go
type Phase string

const (
    PhaseIdle        Phase = "idle"
    PhaseDownloading Phase = "downloading"  // bytes flowing in (Pulling[download])
    PhasePreparing   Phase = "preparing"    // apply + acquire + server boot
    PhasePlaying     Phase = "playing"      // server ready, addr reachable
    PhaseWrapping    Phase = "wrapping"     // server stopping + commit
    PhaseSaving      Phase = "saving"       // bytes flowing out (Pushing) + tail
    PhaseFailed      Phase = "failed"       // lock conflicts surface here with LockHolder set
)
```

Lock conflicts fold into PhaseFailed with `vm.LockHolder` populated — no separate PhaseLocked / StageLocked. Frontend picks friendly "{holder} is playing" copy when LockHolder is non-empty, generic "Couldn't finish {noun}" otherwise.

### Bucket × Phase mapping

| Runtime stage | + Server lifecycle | `vm.Stage` | `vm.Phase` |
|---|---|---|---|
| Checking | — | downloading | downloading |
| Pulling [byte-download] | — | downloading | downloading |
| Pulling [apply] | — | downloading | **preparing** |
| Acquiring | — | downloading | preparing |
| Running | `!everReady` | downloading | preparing |
| Running | `ServerReady` fired | **running** | playing |
| Running | `ServerStopping` fired | **uploading** | **wrapping** |
| Committing | — | uploading | wrapping |
| Pushing | — | uploading | **saving** |
| Unlocking | — | uploading | saving |
| Retaining | — | uploading | saving |
| (Acquiring → LockHeld) | — | locked | locked |
| Failed | — | failed | failed |
| Done → Idle | — | idle | idle |

Boundaries that shift relative to today:
- `StageRunning + !ServerReady` no longer flips bucket — stays in `downloading` bucket with `Phase=preparing`.
- `ServerReady` flips bucket to `running` and `Phase=playing`.
- `ServerStopping` flips bucket to `uploading` and `Phase=wrapping`.
- Pulling-apply tail flips `Phase` from `downloading` to `preparing` (see §Open questions for the detection signal).

### Visual dispatch

Frontend maps `Phase` → (glyph, title, sub, ETA visibility). Copy locked
in iteration after Apple/server idiom review — gerund titles, short
descriptive subs, no ellipses, lock-conflict folded into Failed:

| Phase | Glyph | Title | Sub | ETA shown? | Arc |
|---|---|---|---|---|---|
| idle | play | "Start" | — | — | 0 |
| downloading | download | "Downloading" | bytes line | **yes** | 0→1 (bytes) |
| preparing | brain-cog | "Spinning up" | "Almost live" | **no** | plateau @ 1 |
| playing | stop | "Live" | uptime | — | 1 + glow |
| wrap-head | unplug | "Spinning down" | "Going offline" | **no** | plateau @ hold-drained value |
| saving (bytes) | upload | "Saving" | bytes line | **yes** | 0→1 (Pushing bytes) |
| save-tail | upload | "Wrapping up" | "Almost done" | no | plateau @ 1 |
| failed (generic) | x | `Couldn't finish {phase-noun}` | "Tap to dismiss" | — | frozen at last value |
| failed (lock) | x | `{holder} is playing` | "Tap to dismiss" | — | 0 |

**Phase-noun map** for generic failure title: `downloading | preparing → "starting"`; `playing → "running"`; `wrapping | saving → "saving"`. Three nouns cover the six active phases.

**Save-tail detection** (PhaseSaving with "Wrapping up" / "Almost done"): `Phase === saving && bytesDone >= bytesTotal && bytesTotal > 0`. Frontend reads existing fields — no new Go signal.

**Lock-conflict path:** `acquiring.LockHeldInfo` records `vm.LockHolder` on the projection without changing Phase; the subsequent `lifecycle.StatusChanged{Failed}` flips Phase=failed. Frontend reads `vm.LockHolder` during PhaseFailed to pick friendly copy. No `PhaseLocked` / `StageLocked` constants — they were folded out.

### Arc continuity (one honest narrative)

```
arc │
 1.0│           ━━━━━━━━━━━━━━━━━━━━━━─┐
    │          ╱│preparing │ playing │ │╲                    ━━━━→ idle
 0.5│        ╱  │          │         │ │ ╲              ╱
    │      ╱    │          │         │ │  ╲    plateau╱
 0.0│ ────╱     │          │         │ │   ────────╱
    │  idle  download  preparing  playing wrapping saving idle
    │  ▼        ▼          ▼         ▼      ▼        ▼      ▼
    │  0      0→1 bytes   1 plateau  1+glow  0 plateau 0→1 bytes
```

Two plateaus (preparing, wrapping) flank the playing peak. Each tells one honest beat without inventing progress. Arc never resets between adjacent phases — continuous story across the whole lifecycle.

### Address list timing

Mount on `Phase === playing`, unmount on any other phase. Today `run-addresses` mounts on `StageRunning` entry and lingers; with the new mapping it appears later (only when reachable) and disappears earlier (the moment shutdown starts). Tighter alignment with "you can copy this address and connect right now".

### Failure: dismiss to idle

007 §Q11 promised retry-from-failed (arc-preserving resume). Implementation never landed — `ritual-app.ts:213-216` calls `start()` fresh. The promise is dropped here.

New rule:
- Sub copy `"Tap to try again"` → `"Tap to dismiss"`.
- Fail tap calls a new `ControlService.Dismiss()` method which fires a `lifecycle.StatusChanged{Status: Idle}` event.
- Projection treats `Failed → Idle` as a state reset (clears `ErrorText`, resets `everReady`, leaves the rest at zero).
- After dismissal, user re-engages by tapping the (now-idle) `▶ play` glyph deliberately. Two taps to recover; each tap unambiguous.

HIG basis: Apple alerts always separate dismiss from action ("OK" vs "Try Again"); the dial is one unlabeled tap zone, so dismiss is the only safe gesture.

### Frontend simplification

`ritual-app.ts derive()` collapses from ~80 LOC of stage-switching to a thin Phase-dispatch table. Hard-coded labels removed. `vm.Label` no longer consumed by the dial (kept on Go side for logs only, or deleted — see Q3).

```ts
// New shape — illustrative
const PHASE_VIEW: Record<Phase, DialView> = {
    idle:        { state: "idle",  glyph: "play",      label: "Start",          sub: "" },
    locked:      { state: "idle",  glyph: "x",         label: lockLabel,        sub: "Tap to check again" },
    downloading: { state: "prep",  glyph: "download",  label: "Getting ready",  sub: etaSub },
    preparing:   { state: "prep",  glyph: "brain-cog", label: "Getting ready",  sub: "Preparing…" },
    playing:     { state: "run",   glyph: "stop",      label: "Ready to play",  sub: uptimeSub },
    wrapping:    { state: "final", glyph: "unplug",    label: "Saving",         sub: "Wrapping up…" },
    saving:      { state: "final", glyph: "upload",    label: "Saving",         sub: savingSub /* bytes or empty */ },
    failed:      { state: "fail",  glyph: "x",         label: failLabel,        sub: "Tap to dismiss" },
};
```

## Examples

### ❌ Before — three lies in five seconds

```
t+0s:  StageRunning fires       → dial: green, "Ready to play", stop glyph
                                  (server still booting, port not bound)
t+3s:  ServerReady fires        → dial: green, "Ready to play"
                                  (vm.Label became "Ready" — frontend ignored it)
t+9m:  User hold-stops          → dial: still green, "Ready to play"
                                  (server already stopping, vm.Label = "Stopping…" — ignored)
t+9m1s: Committing fires        → dial: teal, "Saving", upload glyph, ETA shown
                                  (no bytes flowing yet; ETA invented from stale rate)
```

### ✅ After — six honest beats

```
t+0s:  StageRunning + !everReady → dial: yellow, brain-cog, "Getting ready" / "Preparing…"
t+3s:  ServerReady fires         → dial: green, stop, "Ready to play" / uptime
                                   addresses appear underneath
t+9m:  User hold-completes       → dial: teal, unplug, "Saving" / "Wrapping up…"
                                   addresses disappear; arc starts at hold-drained value
t+9m1s: Committing fires         → dial: teal, unplug, "Saving" / "Wrapping up…" (continuous)
t+9m4s: Pushing starts ticking   → dial: teal, upload, "Saving" / bytes + ETA, arc fills 0→1
t+9m12s: Pushing done, Unlocking → dial: teal, upload, "Saving" / empty sub, arc plateau @ 1
t+9m13s: Retaining → Done → Idle → dial: blue, play, "Start"
```

## Implementation Plan

### Phase A — Go projection

1. Add `Phase` type + constants to `internal/gui/projection/viewmodel.go`. Add `Phase Phase \`json:"phase"\`` to `ViewModel`.
2. Add `everReady bool` field to `Projection` struct in `projection.go`.
3. Rewire `onStateChanged`:
   - `Checking | Pulling`: `Stage=downloading`, `Phase=downloading`.
   - `Acquiring`: `Stage=downloading`, `Phase=preparing`.
   - `Running`: `Stage=downloading`, `Phase=preparing`. *Do not flip bucket.*
   - `Committing`: `Stage=uploading`, `Phase=wrapping`.
   - `Pushing`: `Stage=uploading`, `Phase=saving`.
   - `Unlocking | Retaining`: `Stage=uploading`, `Phase=saving`.
4. Add handlers:
   - `ServerReadyInfo`: set `everReady=true`, `Stage=running`, `Phase=playing`, attach addresses.
   - `ServerStoppingInfo`: `Stage=uploading`, `Phase=wrapping`, drop addresses.
5. Subscribe to new `pulling.ApplyStartedInfo` event (Q1 (c)); on receipt set `Phase=preparing`.
6. `onStatusChanged(Idle | Done)`: reset `everReady=false`, blank `Phase=idle`.
7. `onStatusChanged(Failed)`: `Phase=failed`, keep `everReady` for failure attribution.
8. `onStatusChanged(Dismissed)`: clear `ErrorText`, reset `everReady`, set `Phase=idle`, blank `Stage=idle` (Q5).
9. Remove `vm.Label` field + `downloadLabel()` / `uploadLabel()` helpers (Q3 (a)).

### Phase B — Lifecycle dismiss path

10. Add `lifecycle.Dismissed` status constant + permitted `Failed → Dismissed` edge (Q5).
11. Add `Dismiss()` method to control service. Fires `lifecycle.StatusChanged{Status: Dismissed}`.
12. Audit `internal/core/stages/failed/strategy.go` for retry back-edge — cut whatever exists (Q6).
13. Audit other `lifecycle.StatusChanged` consumers; extend their switch with `Dismissed` case (or default-passthrough).

### Phase B' — Pulling event

14. Publish `pulling.ApplyStartedInfo` from `internal/core/stages/pulling/strategy.go` between download loop and workdir-apply call (Q1).
15. Test in `pulling/strategy_test.go`: event fires exactly once per Pulling run, before apply, even when `BytesTotal==0`.

### Phase C — Wails binding + frontend

16. Regenerate Wails bindings (`Phase` type, `Dismiss()` method, removed `Label` field).
17. Rewrite `ritual-app.ts derive()` as Phase-dispatch table (per §Design).
18. Drop hard-coded labels `"Getting ready"` / `"Saving"` indirection — they're now in the table.
19. Address list visibility: `Phase === playing` predicate replaces `Stage === running`.
20. Under-slot telemetry: `Phase === downloading || (Phase === saving && bytesDone < bytesTotal)`.
21. Fail-tap calls `Dismiss()` instead of `start()`.
22. `lastNonFailStage` field becomes `lastNonFailPhase` for noun mapping.

### Phase D — Dial glyph additions

23. Add `brain-cog` to `DialGlyph` enum + lucide import in `ritual-dial.ts`.
24. Add `unplug` to `DialGlyph` enum + lucide import.
25. Re-run the compound-path normalization (`svgpath(d).abs()`) for both new glyphs — verify they morph cleanly from/to existing glyphs. `brain-cog` renders static (no rotation, per Q4).

### Phase E — Storybook

26. Add `PrepBooting` story: `state=prep, glyph=brain-cog, label="Getting ready", sub="Preparing…", arc=1`.
27. Add `FinalWrapping` story: `state=final, glyph=unplug, label="Saving", sub="Wrapping up…", arc=0`.
28. Add `FinalSavingTail` story: `state=final, glyph=upload, label="Saving", sub="", arc=1`.
29. Update `Cycle` story to walk all 8 phases.
30. Update fail stories: sub `"Tap to dismiss"`; clicking the dial in `Fail` story calls `Dismiss()`.

### Phase F — Tests

31. `projection_test.go`: new cases — ServerReady gate, ServerStopping gate, ApplyStartedInfo transition, fail-mid-boot phase attribution, dismiss reset path.
32. Modify existing cases that asserted "StageRunning entry → bucket=running" → now "→ Phase=preparing".
33. `pulling/strategy_test.go`: ApplyStartedInfo fires exactly once per run, before apply, even with `BytesTotal==0`.
34. `lifecycle/subsystem_test.go`: `Failed → Dismissed` edge permitted; `Dismissed → Idle` follow-on if applicable.

### Phase G — Verify

35. Live Wails build: walk full lifecycle, confirm all eight phase beats render with the right glyph + copy + arc behavior.
36. Walk failure paths: fail-mid-download, fail-mid-prepare, fail-mid-play, fail-mid-wrap, fail-mid-save. Verify dismiss returns to idle cleanly.
37. Confirm address list appears only during `playing`.
38. Confirm ETA visible only during `downloading` and `saving`-with-bytes.

## Trade-offs

| Choice | Gain | Cost |
|---|---|---|
| New `Phase` field on ViewModel | Single source of truth for visual dispatch; Go owns the logic | One more field on the wire; Wails binding regen |
| Drop `vm.Label` from dial consumption | Frontend stops fighting the projection; dead-code reduction | Carefully-tuned phase copy in Go becomes dev-only / candidate for removal |
| Two prep beats (download + prepare) | Honest about apply + acquire + boot windows; ETA stops lying | Glyph swap mid-prep — must morph cleanly; one extra Storybook story |
| Three back-side beats (wrap + save + tail) | Honest about server-stop and post-bytes cleanup | Asymmetric with prep; tail-detection needs a heuristic |
| Cut retry-from-failed | Orchestrator simplification; honest UI ("dismiss" means dismiss) | Two taps to recover instead of one; 007 §Q11 contract retired |
| Bytes-done heuristic for save-tail | Zero new Go events | Fragile when `bytesTotal==0` (no upload work) — accepted per Q2 |
| Explicit `pulling.ApplyStartedInfo` event | Single source of truth for the apply-tail; robust at `BytesTotal==0` | Small Go change in `pulling/strategy.go` + test |
| Distinct `lifecycle.Dismissed` status | Observers can tell user-dismiss from Done→Idle | One more status constant; consumers extend their switch |

## Verification Criteria

1. **Phase enumeration.** `grep -E 'PhaseDownloading|PhasePreparing|PhasePlaying|PhaseWrapping|PhaseSaving|PhaseFailed|PhaseIdle' internal/gui/projection/viewmodel.go` returns all 7 constants. `PhaseLocked` / `StageLocked` do not exist.
2. **No bucket flip on StageRunning entry.** `projection_test.go` case: emit `ritual.StateChangedInfo{To: StageRunning}` → expect `vm.Stage = downloading, vm.Phase = preparing`.
3. **ServerReady gates run bucket.** `projection_test.go` case: after StageRunning, emit `running.ServerReadyInfo` → expect `vm.Stage = running, vm.Phase = playing, vm.Addresses != nil`.
4. **ServerStopping gates final bucket.** `projection_test.go` case: from playing, emit `running.ServerStoppingInfo` → expect `vm.Stage = uploading, vm.Phase = wrapping, vm.Addresses == nil`.
5. **Apply-tail detection.** When Pulling completes its download phase and applies, `Phase` shifts to `preparing` (specific mechanism per Q1).
6. **Save-tail detection (frontend).** When `Phase === saving && bytesDone >= bytesTotal`, dial sub renders empty (not bytes/ETA).
7. **ETA discipline.** Grep `ritual-app.ts` for `etaSub` calls — only invoked when `Phase === downloading` or (`Phase === saving && bytesDone < bytesTotal`).
8. **Address visibility.** `<run-addresses>` only rendered when `Phase === playing`.
9. **Fail dismisses.** Storybook story `Fail` — clicking dial calls `Dismiss()` (not `start()`); after dismissal `vm.Phase === idle`.
10. **No "Tap to try again" copy.** `grep -r "try again" frontend/src` returns nothing.
11. **Address visibility.** Cold-loaded `Playing` story shows `<run-addresses>`; cold `Wrapping` story does not.
12. **Lifecycle edge.** `internal/subsystems/lifecycle` test confirms `Failed → Idle` transition fires `StatusChanged` without panicking.
13. **Pixel parity.** Re-screenshot the six honest beats at 1280×800; bounding boxes stable across phase transitions (Phase swap is data, not layout).
14. **Cycle story.** `dial-cycle-demo` walks all 8 phases; arc shows continuous narrative (no resets between adjacent phases except at idle re-entry).

## Open Questions

> Q1. **Pulling-apply transition signal.** Pulling internally does download → apply, both inside one `ritual.StageChangedInfo{To: Pulling}` event. How does projection detect "downloads done, apply running" to flip `Phase` from `downloading` to `preparing`?
>
> **Options:**
> - (a) **Bytes-done heuristic**: when `pipelineStage == Pulling && BytesDone >= BytesTotal && BytesTotal > 0`, set `Phase=preparing`. Cheap, no Go-side changes. Fragile when `BytesTotal==0` (already-cached case — but then Pulling exits immediately, so window is invisible anyway).
> - (b) **Tick-staleness heuristic**: if last `progress.Tick` arrived > 1s ago while still in Pulling, switch to preparing. More precise but introduces a timer in projection.
> - (c) **Explicit Go event**: `pulling/strategy.go` emits a new `pulling.ApplyStartedInfo` event when its download phase finishes. Cleanest, smallest source of truth, but requires Go work.
>
> **A:** (c) explicit Go event. `pulling.ApplyStartedInfo` published from `pulling/strategy.go` between the download loop and the workdir-apply call. Projection subscribes and flips `Phase=preparing` on receipt. Single source of truth, no heuristic, robust to `BytesTotal==0` (the event still fires even when there's nothing to download — projection sees the event, transitions cleanly).

> Q2. **Save-tail when `bytesTotal==0`.** If a Pushing run has nothing to upload, the bytes-done heuristic fires immediately. Phase=saving with empty sub for the entire window. Acceptable (window is brief) or special-case?
>
> **A:** Acceptable. Brief window, honest about nothing-to-show. No special-case branch.

> Q3. **`vm.Label` fate.** The projection sets `vm.Label` to per-substage strings (`"Snapshotting…"`, etc). Frontend will stop reading it. Two options:
> - (a) **Delete.** `vm.Label` field removed; `projection.go` stops populating it. Cleanest.
> - (b) **Keep, freeze to bucket-level.** Demote to `"Downloading…"` / `"Saving…"` for log/debug surfaces. Field stays, populated less aggressively.
>
> **A:** (a) delete. `Label` field removed from `ViewModel`; `projection.go` strips `downloadLabel()` / `uploadLabel()` helpers; Wails binding regenerates without the field. Dead code rots faster than dev convenience.

> Q4. **Glyph rotation during preparing beat.** `brain-cog` is gear-bearing — should the gear visibly rotate during the preparing beat for liveness? Or is the static glyph + plateau-arc enough? `prefers-reduced-motion: reduce` would suppress.
>
> **A:** Static. Plateau arc + gear silhouette + breath glow from neighboring beats reads as live enough. No additional motion. Keeps the motion budget tight and avoids the `prefers-reduced-motion` override surface.

> Q5. **Failed→Idle edge in lifecycle.** Verify `internal/subsystems/lifecycle.StatusChanged` already permits this transition. If not, what's the minimum addition? Affects API surface of `lifecycle` subsystem and any consumers that switch on its status.
>
> **A:** Add a new `lifecycle.Dismissed` status value. `ControlService.Dismiss()` fires `StatusChanged{Status: Dismissed}`. Projection switches on `Dismissed` to clear `ErrorText`, reset `everReady`, then transitions internally to `Phase=idle`. Distinct from `Idle` (clean Done→Idle) so observers can tell "user dismissed a failure" apart from "run completed cleanly". Affects: `lifecycle/subsystem.go` (new constant + edge), `projection.go onStatusChanged` (new case), any other consumers (audit + extend default branch).

> Q6. **`internal/core/stages/failed/strategy.go` audit.** Does it currently have a back-edge to retry-from-failed? If yes, this design cuts it. If no, the cut is no-op in core (just docs).
>
> **A:** Cut entirely. Whatever the audit finds, the back-edge is removed. Failed becomes terminal-pending-dismiss in the orchestrator: the stage's only outgoing edge is the next-strategy chain ending at `nil` (Done). Smaller orchestrator surface aligns with 017's dismiss-to-idle contract. Concrete impact deferred to the Phase B audit step.

## See also

- **007** — One dial, four states. This log supersedes the FAIL row (retry → dismiss) and the PREP/FINAL glyph rows (now phase-driven).
- **009** — Telemetry hierarchy. ETA-as-hero is preserved during bytes-flowing beats; dropped outside per the Apple-aligned rule.
- **010** — Run addresses. Visibility predicate tightens to `Phase === playing`.
- **013** — Dialed GUI cutover. The `ritual-app` shell shape established there gains a `Phase`-dispatch table; structure otherwise unchanged.
