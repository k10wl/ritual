# 050 — Chained-launch download-progress overflow + friend's Windows 11 "no icons" report

**Date:** 2026-07-19
**Status:** §A **implemented** — per-flow baseline shipped in `internal/gui/projection/projection.go`, TDD tests green. §B narrowed by requestor to the dial's radial-button glyphs (play/stop/brain-cog/upload); hardened with a regression-test safeguard + a structural CI-gating fix. §C (new, same session) **implemented** — size/speed telemetry formatting and the "Live" uptime clock moved from the frontend to the backend ("client is only a projection"); mandatory version bump to 2.0.2. Neither the exe icon nor the friend's exact icon failure was directly confirmed (§B); this remains defense-in-depth there, not a confirmed root-cause fix.
**Related:** [[001-progress-projection]] (Data vs Transfer counter split), [[019-plan-info-delta]] (per-flow `PlanInfo` delta), [[028-transfer-eta-stability]] (beat-wide ETA baseline — same "re-anchor per flow" shape as the §A fix and the model for §C's `EtaSeconds`/`UptimeSeconds` split), [[037-autoupdate]] (exe icon embedding via `generate:syso`; version bump feeds the mandatory-update flow), [[047-os-notifications]] (AppUserModelID self-registration), [[010-run-addresses]] (sibling lucide→SVG icon usage, not yet covered by the same guard), [[018-logical-rate-in-ui]] (established `LogicalMbps` as the displayed rate — §C's `SpeedText` continues reading from it, not `SpeedMbps`)

## Background

User-reported from `20260718202843.log` (one continuous app session, 20:28:43–21:04:09; the dial was started/stopped five times in a row — "chained launches" — to test sync behaviour, all inside the same running process):

1. `plan pull files=814 bytes=1280889536` at 20:29:51 — the initial full download (~1.19 GiB). It runs to near-completion (`done=1277639100/1280889536` at 20:38:31) and finishes cleanly.
2. `plan push files=27 bytes=62066935` at 20:45:49 — session ends, uploads world deltas.
3. A second chained launch at 20:47:50 correctly computes `plan pull files=0 bytes=0` — nothing changed, no bug.
4. A third chained launch at 20:51:52 computes `plan pull files=25 bytes=61655570` (~58.8 MB — this is the "50mb update" the user refers to). But the `[snap]` telemetry line for it reads:
   ```
   [20:51:52] [snap] stage=downloading phase=downloading eta=0s done=1280889536/0 speed=0.00Mbps stalled=false
   [20:51:53] plan pull files=25 bytes=61655570
   [20:51:53] [snap] stage=downloading phase=downloading eta=0s done=1280889536/61655570 speed=0.00Mbps stalled=false
   ```
   `done` (1,280,889,536 — the *entire first pull's total*) is already **20.8×** the new plan's total (61,655,570) before a single byte of the third pull has moved. This is the "misscalculated total" — the progress readout is impossible (>2000%) and never reads as a clean 0→100% climb for the small delta.
5. `plan push files=109 bytes=167052748` at 21:03:23 closes the session.

Second, independent report: a friend running the app on Windows 11 "had no icons" — no repro log, no screenshot, no detail on which icon(s). Not diagnosable from `20260718202843.log` (that machine never generated it).

## Problem

### A. `BytesDone` numerator is a process-lifetime cumulative counter; `BytesTotal` denominator is a per-flow delta

Confirmed by code trace, not just log inference:

- `internal/adapters/counter.go:32-36` — `StorageCounters{BytesIn, BytesOut, OpsComplete, OpsFailed}` are bare `atomic.Int64`s. `GetStream`/`PutStream` (`counter.go:55-103`) only ever `Add()` to them — nothing ever calls `Store(0)` or otherwise resets a counter.
- `cmd/gui/main.go:435-450` constructs exactly one `remoteLogical`/`remoteWire`/`localLogical`/`localWire` `*StorageCounters` set **once, at process startup**, and `NewTicker` (`main.go:652-654`) is wired to that same set for the app's entire lifetime — not reconstructed per flow.
- `internal/adapters/progress/ticker.go:174-186` (`readCounters`) does a raw `.Load()` on those counters every tick — always the full cumulative total since process start, never a since-this-flow delta.
- `internal/gui/projection/projection.go:289-313` (`onTick`), pull branch: `done = t.Remote.Down.Data` — assigned straight to `ViewModel.BytesDone` with no baselining against where the counter stood when this pull's `PlanInfo` arrived.
- `internal/gui/projection/projection.go:151-153` (`case ritual.PlanInfo`) sets `BytesTotal`/`FilesTotal` fresh from the event, which **is** correctly scoped to just this flow's delta (design-log/019 §pre-flight `List` diff). So numerator and denominator have different scopes: numerator = "since the app opened," denominator = "since this plan was computed."

For a process's *first* pull this is invisible — the cumulative counter starts at 0, so numerator and denominator happen to agree. It only breaks on the second-or-later pull inside the same running process, and it breaks worse the smaller the new plan is relative to everything already downloaded this session — exactly "chained launches ... 50mb update."

**Push has the same defect, quieter.** `onTick`'s push branch (`projection.go:296-309`) uses `t.Ops.Done * avg-blob-size` instead of raw `s.Data` (chosen for an unrelated reason — logical-vs-confirmed timing, see the doc comment at `projection.go:277-283`) — but `t.Ops.Done` (`ticker.go:184`, `t.remote.Logical.OpsComplete.Load()`) is **also** one shared cumulative counter, incremented by both pull's `GetStream` calls and push's `PutStream` calls, never reset. The only reason push doesn't show a >100% readout is the explicit clamp two lines down (`if done > p.state.BytesTotal { done = p.state.BytesTotal }`, `projection.go:304-305`) — so a chained push silently jumps straight to "100% done" instead of climbing, which is just as wrong but far less visually alarming than the pull case, and easy to miss.

### B. Friend's "no icons" on Windows 11 — narrowed to the dial's radial-button glyphs

Requestor clarified mid-investigation: **not** the exe/taskbar icon — the play/stop/brain-cog/upload glyphs on the dial itself (`frontend/src/ui/ritual-dial.ts`).

These are `lucide` icon shapes (framework-agnostic `[tag, attrs]` node arrays) converted to SVG path `d` strings at runtime-once (module-load time), via `shapeToD`/`compoundD` in what's now `frontend/src/ui/dial-glyphs.ts` (see §Implementation Results — originally inline in `ritual-dial.ts`). `shapeToD` only handles `path`/`line`/`circle`/`rect` tags (§Design B). If a shape's tag isn't one of those four, it silently returns `""` — no error, no console warning, just an empty path segment that draws nothing. `compoundD` then runs the result through `svgpath(...).abs().toString()` (relative→absolute command normalization, needed because GSAP's `MorphSVGPlugin` morphs between glyphs and needs consistent absolute coordinates) and caches it once into a module-level `GLYPHS` dict — computed a single time at import, not per-render, so a bad conversion is invisible for the entire session, not just one frame.

**This exact class of regression had zero test coverage before this session.** Confirmed by direct check: no `.test.ts` file in the repo imports `lucide`, `svgpath`, or `gsap` — `ritual-dial.ts`, `run-addresses.ts` (icon-copy affordance, [[010-run-addresses]]), and `run-console-link.ts` all render icons through this same lucide→path pipeline, and none had ever been exercised by a test. Worse: **no frontend test of any kind was ever wired into `task test`** (confirmed by reading `Taskfile.yml`'s `test:` task, `_publish`'s gate, and both `.github/workflows/{ci,publish}.yml` — every one of them runs `go test ./...` only). `frontend/CLAUDE.md`'s own "Testing Posture" section mandates `@web/test-runner` behavior tests per component, and 171 of them exist and pass — they just never gated a build or a publish. A silent empty-path regression in `shapeToD` (e.g. from a future `lucide` upgrade that redraws one of these icons using a `polygon`/`polyline`/`ellipse` tag — none are handled) could ship to every user, dev and CI alike, and nothing would catch it before a bug report like this one.

The exe/taskbar icon (candidate 1 from the original draft) was checked anyway, since it was already in flight: **confirmed fine.** `ExtractIconEx` (the actual resource-table count, not `ExtractAssociatedIcon`'s shell-synthesized fallback which returns a generic icon for *any* exe and can't distinguish "real branded icon" from "no icon at all") reports exactly 1 embedded icon resource on all three locally-built exes (`bin/ritual.exe`, `bin/ritualdev.exe`, and an older `bin/ritualdev-2.0.0 - Copy.exe`). Not proof the friend's specific binary was fine, but rules out "the local build pipeline is structurally broken" — and a build-time check now guards it going forward regardless (§Implementation Results).

## Reproduction Plan

### A. Deterministic — reproduce at the unit level first, before touching production code (TDD red)

Per CLAUDE.md methodology ("write tests first"), the repro **is** the red test — written to assert the *correct* behavior, which fails against today's code. Not a characterization test of the bug; a specification of the fix, proven to fail first. Full red→green cycle in §Testing Plan; the repro itself is step 1 there:

1. In `internal/gui/projection/projection_test.go`, drive one `Projection` instance through two full flows without resetting it between them (mirrors a real process's long-lived `Ticker`/`StorageCounters`):
   - Flow 1: `Checking→Pulling`, `PlanInfo{BytesTotal: 1_280_889_536, FilesTotal: 814}`, a `Tick` with `Remote.Down.Data: 1_280_889_536` (mirrors the log's near-total first pull), `Done`.
   - Flow 2 (no `Projection` reset — same instance): `Checking→Pulling` again, `PlanInfo{BytesTotal: 61_655_570, FilesTotal: 25}` (the "50mb update" delta), then a `Tick` with `Remote.Down.Data: 1_280_889_536` **unchanged** (nothing new has streamed yet — matches the log's stray pre-`PlanInfo` tick at line 13509).
   - Assert the **desired** behavior: `final.BytesDone == 0` (nothing new has moved yet in flow 2) and `final.BytesDone <= final.BytesTotal` always. Against today's unfixed `onTick`, this fails with `final.BytesDone == 1_280_889_536` — the exact overflow from the log. **Red.**
2. Run `go test ./internal/gui/projection/... -run TestProjection_ChainedPull -v` and confirm it fails with that exact value — the sign-off that the repro is real and isolated to `onTick`/`onStateChanged`, not a log-reading artifact.

### A. Manual GUI confirmation (secondary, after the unit repro)

1. `task gui:run:dev:local` (048's mock backend — no live R2 needed, same `projection`/`progress` code path).
2. Seed the mock remote root with a nontrivial initial world (tens of MB), press Start, let the pull complete, Stop.
3. Press Start again with **no changes** — confirm the zero-delta no-op path (`plan pull files=0 bytes=0`, log line 11038's pattern) shows a clean/instant complete, not a red flag by itself.
4. Modify a handful of MB under the mock remote's world data, press Start a third time. Watch the dial the instant it enters the downloading state: bug reproduces if the ring/percentage reads complete-or-overfull *before* any new bytes stream, instead of climbing 0%→100% for the small delta.
5. Cross-check `<root>/logs/<ts>.log` for the same `done=X/Y` overflow pattern as the original report.

### B. No independent repro — verified current correctness instead; a real repro would still need the friend

Superseded mid-investigation: requestor confirmed the report is about the dial's radial-button glyphs, not the exe icon (§Problem B). No log/screenshot/repro steps exist for the actual reported symptom, so nothing here proves what was wrong on the friend's machine specifically. What was actually done:

1. **Exe icon (deprioritized but already in flight — steps 1-2 below, kept for the record):** verified locally via `ExtractIconEx` against all three local `bin/*.exe` builds (§Testing Plan step 7) — all carry exactly 1 icon resource. Not a live-CI or friend's-machine check, but rules out "the build pipeline as configured cannot embed an icon."
2. **Dial glyphs:** ran the real installed `lucide` package's icon-node data for all seven dial glyphs through `shapeToD` (§Testing Plan step 6) — all produce non-empty paths today. Confirms current correctness; does not prove what the friend saw, since their build/lucide-version/environment is unknown.
3. **If the friend can still reproduce it:** a screenshot of the dial (does it show a blank circle where the glyph should be, or a visibly wrong/garbled shape?) plus their `<root>/logs/<ts>.log` would be the only way to move this from "hardened against a plausible cause" to "confirmed and fixed." Worth asking for even now — the guard added this session catches *a* failure mode, not necessarily *the* one they hit.

## Questions and Answers

**Q1 (§A).** Is the counter-vs-plan scope mismatch also visible in the very first pull of a session?
**A.** No — a fresh process's `StorageCounters` all start at 0, so cumulative-since-start and delta-for-this-plan coincide for the first flow. The bug is latent until a second flow runs in the same process.

**Q2 (§A).** Does `resetEtaBeat()` (already called on every `PlanInfo` and stage change, `projection.go:157` / `339`) already solve this?
**A.** No — it only clears the ETA-beat anchor (`etaBeatStarted`/`etaBeatElapsed`/`etaBeatBytes`), not `BytesDone` itself. `BytesDone` is recomputed fresh on every `Tick` from the raw cumulative counter (§A code trace above), so no amount of resetting *other* state fixes it — the fix has to change what `onTick` reads.

**Q3 (§A).** Why does `TestProjection_TickInPullingStage_UpdatesBytesDone` (`projection_test.go:266-272`) pass if this is broken?
**A.** That test (and the whole projection test suite) constructs a fresh `progress.Tick` with `Remote.Down.Data` set directly to the value under test — it never simulates *two* pulls sharing one underlying counter across a `PlanInfo` reset. The unit tests exercise `onTick`'s per-call correctness, not the cross-flow counter-lifetime invariant that's actually broken. This is a coverage gap, not a contradiction.

**Q4 (§A).** Where should the fix live — reset the counters, or baseline the projection?
**A.** Proposed: **baseline in the projection**, not reset the counters. Resetting `StorageCounters` between flows would require plumbing a reset call through `cmd/gui/main.go` at every flow-start site and would also zero the *speed* window state the same `Ticker` derives (`Instant`/`Average`/`Smoothed` are already delta-based between consecutive ticks, so they're unaffected by a reset either way — but coupling a UI-only bug fix to touching the long-lived, already-tested `Ticker`/`StorageCounters` singletons is a bigger blast radius for no extra benefit). `Projection` already privately tracks flow-scoped bookkeeping (`pipelineStage`, `etaBeatStarted`, etc.) — this is one more field of the same kind: `pullBaseline int64` (raw `Remote.Down.Data` observed at the moment the flow enters `StagePulling`) and `pushOpsBaseline int64` (raw `Ops.Done` observed at the moment it enters `StagePushing`). `onTick` then computes `done = t.Remote.Down.Data - p.pullBaseline` (pull) / `done = (t.Ops.Done - p.pushOpsBaseline) * avg` (push), each clamped to `[0, BytesTotal]`.

**Q5 (§A).** The log shows a stray `[snap] ... done=1280889536/0` **before** the `plan pull` line that sets the new total (13509 vs 13513). Doesn't that mean a `Tick` can arrive before `PlanInfo`, defeating a baseline captured *on `PlanInfo`*?
**A.** Yes — this is why Q4's baseline must be captured on the **`Checking → Pulling` / `Checking → Pushing` stage transition** (`onStateChanged`, already the hook for `resetEtaBeat()`), not on `PlanInfo` arrival. The projection already receives every `Tick` regardless of stage (it just currently only *acts* on them during `StagePulling`/`StagePushing`, `onTick` line 292); it needs to keep tracking the latest raw counter values unconditionally so a baseline is available the instant the stage flips, even before that flow's own `PlanInfo` has arrived.

**Q6 (§B).** Which "icons" — can we narrow it from the three candidates before writing any code?
**A.** Resolved by requestor mid-session: the dial's radial-button glyphs (play/stop/brain-cog/upload), not the exe/taskbar icon. See revised §Problem B.

**Q7 (§B).** Given no repro log/screenshot from the friend exists, how do we test a fix for a bug we can't reproduce?
**A.** We can't test *the specific reported instance* — but the underlying mechanism (`shapeToD`'s tag coverage) is a real, narrow, fully-specified piece of logic that's either correct or not for the *current* `lucide` version, independent of what happened on the friend's machine. So the plan shifts from "reproduce and fix a specific defect" to "verify current correctness and add a regression guard against the most plausible failure mode (a future/different `lucide` version redrawing an icon with an unhandled shape tag)." This is a hardening response, not a confirmed diagnosis — stated plainly in Status and Verification below.

**Q8 (§B).** Why did testing `ritual-dial.ts` directly hit a wall?
**A.** `svgpath` (used for `.abs()` path normalization) is bare CommonJS — `module.exports = SvgPath`, no `export` keyword anywhere. `frontend/web-test-runner.config.mjs`'s `nodeResolve: true` only resolves bare specifiers to file paths; it does **not** convert CommonJS to ESM (confirmed by direct experiment — see §Implementation Results), so the browser's native ESM loader receives the raw CJS file and rejects `import svgpath from "svgpath"` ("does not provide an export named 'default'"). This is exactly why zero tests existed for the three lucide-consuming components before this session — anyone who tried immediately hit this wall. Vite's real dev/build pipeline handles the same import fine (confirmed: `npm run build` succeeds, single bundled chunk, §Implementation Results), so this is a test-tooling gap, not a production bug.

## Design

### A. Per-flow baseline for `BytesDone`

```go
// internal/gui/projection/projection.go — new unexported fields on Projection
type Projection struct {
    // ...existing fields...
    pullBaseline    int64 // Remote.Down.Data observed at Pulling stage-entry
    pushOpsBaseline int64 // Ops.Done observed at Pushing stage-entry
    lastRemoteDown  int64 // most recent raw Tick.Remote.Down.Data, tracked unconditionally
    lastRemoteOps   int64 // most recent raw Tick.Ops.Done, tracked unconditionally
}
```

- `onTick` starts unconditionally recording `p.lastRemoteDown = t.Remote.Down.Data` and `p.lastRemoteOps = t.Ops.Done` before the `switch p.pipelineStage` gate (currently the function returns `false` immediately for any stage other than Pulling/Pushing — line 310-311 — so today it silently drops ticks outside those two stages; the raw-tracking write needs to happen before that early return).
- `onStateChanged` (`projection.go:331+`), on transition **into** `ritual.StagePulling`, sets `p.pullBaseline = p.lastRemoteDown`; on transition into `ritual.StagePushing`, sets `p.pushOpsBaseline = p.lastRemoteOps`.
- `onTick`'s pull branch: `done = t.Remote.Down.Data - p.pullBaseline`.
- `onTick`'s push branch: `done = (t.Ops.Done - p.pushOpsBaseline) * avg`, then existing `if done > BytesTotal { done = BytesTotal }` clamp stays (also guards a negative baseline edge case with a `max(0, done)`).

This preserves every existing invariant the docstrings already promise (`BytesDone` matches `PlanInfo.BytesTotal`'s scope) — it was always meant to be flow-scoped, the code just never subtracted the flow-start offset.

### B. Split the pure conversion out so it's testable; gate frontend tests into the build

Two independent changes, both structural rather than a targeted bugfix (no confirmed specific defect to target — Q7):

1. **Extract the svgpath-free half.** `shapeToD` (tag → path string, the part with actual per-icon regression risk) moves to a new dependency-free module `frontend/src/ui/lucide-shape.ts` (no `lucide`, no `svgpath` import). `compoundD`/`GLYPHS`/`dFor` (which do need `svgpath` for `.abs()` normalization) move to a new `frontend/src/ui/dial-glyphs.ts`, importing `shapeToD` from the pure module. `ritual-dial.ts` shrinks to just `import { dFor, type DialGlyph } from "./dial-glyphs"`. This isn't a workaround for the test-runner gap alone — it's a legitimate separation of pure data transform from the stateful Lit component, and it's what makes `shapeToD` testable in isolation: a test importing only `lucide-shape.ts` never touches `svgpath`, so it isn't blocked by Q8's CJS wall.
2. **Test `shapeToD` against the real installed `lucide` data**, not synthetic fixtures — `frontend/src/ui/lucide-shape.test.ts` imports the actual `Play`/`Square`/`XIcon`/`Download`/`Upload`/`BrainCog`/`Unplug` icon-node arrays from `lucide` (a real ESM package, no CJS issue) and asserts every shape in every one of the seven dial glyphs produces a non-`""` path through `shapeToD`. This is the closest available proxy for "the dial's icons are not empty" without a DOM/browser fixture (which would still hit Q8's wall via `dial-glyphs.ts`'s `svgpath` import).
3. **Wire frontend tests into the actual build gate.** New `common:test:frontend` task (`build/Taskfile.yml`) runs `npm run test` in `frontend/`; root `Taskfile.yml`'s `test:` task (already the single gate `_publish` and both CI workflows depend on) now runs it alongside the Go suites. This is the durable fix for the structural gap in §Problem B — not just today's icon guard, but every future frontend regression now blocks `task publish:*` the same way a failing Go test already does.

An exe-icon build-time check (`build/windows/check-icon.ps1`, wired into `build:native` after `go build`) was also added, since it was mid-flight when the requestor redirected to the dial glyphs — see §Implementation Results.

## Testing Plan

Strict red→green for §A; §B has no code to test yet (its "testing" is the investigation checklist in §Reproduction Plan B, not a test suite).

**1. Red — pull-side chained-flow test (write first, against unfixed code).**
`TestProjection_ChainedPull_BytesDoneResetsPerFlow` in `internal/gui/projection/projection_test.go`, shape given in §Reproduction Plan A:
- Two `Checking→Pulling` cycles on one `Projection`, second `PlanInfo.BytesTotal` smaller than the first flow's transferred bytes, second flow's first `Tick` carrying the **unchanged** cumulative `Remote.Down.Data` from flow 1 (no new bytes yet).
- Assert `final.BytesDone == 0` and, as the general invariant, `final.BytesDone <= final.BytesTotal`.
- Run it. **Expect failure**: `final.BytesDone == 1_280_889_536` (flow 1's stale total), violating both assertions. This is the checked-in proof the bug exists, using the exact byte counts from the source log — anyone re-running this test on a fresh checkout reproduces the reported bug with zero manual steps.

**2. Green — implement the Design §A fix (Implementation Plan Phase A steps 2-5), re-run step 1's test.**
- Passes once `onTick` computes `done = t.Remote.Down.Data - p.pullBaseline` and `onStateChanged` baselines on `StagePulling` entry.
- No other assertion in the test changes — only the production code does.

**3. Green (written post-fix, protects the fix from regressing) — push-side sibling.**
`TestProjection_ChainedPush_BytesDoneResetsPerFlow`: same two-flow shape, `StagePushing` instead, second flow's `Ops.Done` baseline carried over from flow 1. Assert the second flow's `BytesDone` climbs from 0, not clamped-at-100%-instantly. This one can be written directly against the fixed code (no separate red run required — Phase A step 6 already covers push in the same change), but it must still fail if the push baseline capture is later reverted, so assert the pre-fix-shaped failure mode explicitly (`BytesDone` at first tick of flow 2 must be `0`, not `BytesTotal`).

**4. Full-suite non-regression.**
`go test ./internal/gui/projection/...` — every existing `TestProjection_*` (Q3's suite, single-flow scenarios) must stay green **unmodified**. If any of them needed a code change to pass, the fix touched single-flow behavior and the design is wrong (baseline should be 0 for a process's first flow, matching Q1 — existing tests already implicitly assert this by never setting a non-zero starting counter).

**5. Manual GUI sign-off.**
§Reproduction Plan A's manual steps, run once after step 4 is green, as the human-observable confirmation that the unit-level fix closes the originally-reported symptom end-to-end (Wails IPC → frontend `formatSize` included, not just the Go-side `ViewModel`).

**6. §B — regression guard, not red→green (no confirmed defect to redden against — Q7).**
`frontend/src/ui/lucide-shape.test.ts`: 7 tests (one per dial glyph — play/stop/x/download/upload/brain-cog/unplug), each running every shape of the *real installed* `lucide` icon-node array through `shapeToD` and asserting a non-`""` result. All 7 pass today (§Implementation Results) — confirming current correctness, not fixing a found defect. The guard's value is forward-looking: a future `lucide` bump that redraws one of these icons with an unhandled tag (`polygon`/`polyline`/`ellipse`) fails this test instead of shipping silently. Wired into `task test` (§Design B item 3) so it's `_publish`-gated like every Go test.

**7. §B — exe-icon build-time check (opportunistic, not the confirmed bug).**
`build/windows/check-icon.ps1`, run after `go build` in `build:native`. Uses `ExtractIconEx` (real resource-table count) rather than `ExtractAssociatedIcon` (always returns *something* — Explorer's generic fallback for a zero-resource exe reads identically to a real icon, so it can't detect this failure mode at all; confirmed by direct comparison against `go.exe`, which has no custom icon: `ExtractAssociatedIcon` still returns a 32x32 icon, `ExtractIconEx` correctly reports 0). Verified both branches: `ExtractIconEx` reports 1 on all three local `bin/*.exe` (pass), and reports 0 + the script exits 1 against `go.exe` (fail, as intended).

## Implementation Plan

**Phase A — fix the counter/plan scope mismatch (§A, ready to implement, TDD):**
1. Write the red test from §Reproduction Plan / §Testing Plan step 1 (`TestProjection_ChainedPull_BytesDoneResetsPerFlow` or similar) in `projection_test.go`. Confirm it fails (red) against unfixed code — this **is** the reproduction, checked in before the fix.
2. Add `pullBaseline`, `pushOpsBaseline`, `lastRemoteDown`, `lastRemoteOps` fields to `Projection` (`internal/gui/projection/projection.go`).
3. Move the raw-counter tracking to the top of `onTick`, before the `pipelineStage` switch's early return.
4. Baseline capture in `onStateChanged` on entry to `StagePulling`/`StagePushing`.
5. Update `onTick`'s pull/push `done` computation to subtract the baseline; clamp to `[0, BytesTotal]`.
6. Re-run the step-1 test — confirm green. Add the push-side sibling test (§Testing Plan step 3) and confirm it's green too (no separate red phase needed if written after the fix, but write it to fail-if-reverted by asserting the same invariant).
7. Full suite: `go test ./internal/gui/projection/...` — the existing `TestProjection_*` cases (Q3) must stay green unmodified, proving the fix is additive, not a behavior change to single-flow sessions.

**Phase B — §B hardening (done this session, see Implementation Results):**
1. Extract `shapeToD` into svgpath-free `frontend/src/ui/lucide-shape.ts`; `compoundD`/`GLYPHS`/`dFor` into `frontend/src/ui/dial-glyphs.ts`; trim `ritual-dial.ts` to import from the latter.
2. `frontend/src/ui/lucide-shape.test.ts` — 7 tests over real `lucide` data.
3. `common:test:frontend` task (`build/Taskfile.yml`) + wire into root `test:` (`Taskfile.yml`).
4. `build/windows/check-icon.ps1` + wire into `build:native` (`build/windows/Taskfile.yml`), opportunistic exe-icon guard.
5. Verify: `npx tsc --noEmit` clean, `npm run test` 178/178 green, `npm run build` (real Vite production build) succeeds, `task test` end-to-end green (Go + frontend), icon-check script verified on both a passing and failing exe.

## Examples

✅ After the Phase A fix, the third chained pull from the log would read:
```
[20:51:52] [snap] stage=downloading phase=downloading eta=0s done=0/0            ← baseline captured on stage-entry, not stale
[20:51:53] plan pull files=25 bytes=61655570
[20:51:53] [snap] stage=downloading phase=downloading eta=0s done=0/61655570     ← 0%, correct
...
[20:51:5x] [snap] stage=downloading phase=downloading eta=0s done=61655570/61655570  ← 100% on completion, never >100%
```

❌ Current (from the actual log, line 13509/13514):
```
done=1280889536/0
done=1280889536/61655570   ← 2078% of the new plan's total, before any new bytes moved
```

## Trade-offs

- **Projection-side baseline vs counter reset:** chosen to keep `StorageCounters`/`Ticker` (already covered by their own test suites, `counter_test.go`/`ticker_test.go`, and shared by the speed/EWMA machinery) untouched. Cost: `Projection` grows two more bookkeeping fields, same category as the ETA-beat fields it already carries.
- **Push fix (§A) rides along with pull:** same root cause, same file, marginal extra cost to fix both now rather than leaving push's silent instant-100% bug for a future report.
- **§B ships hardening, not a confirmed fix:** with no repro, "fix the icons" isn't answerable precisely — the honest move was to verify current correctness, close the coverage gap that let this class of bug go unnoticed, and say plainly that the friend's specific instance is still unconfirmed. Cost: engineering effort spent on a guard that may not have caught the actual reported defect.
- **`shapeToD` extraction (§B) over inline test workarounds:** splitting pure logic into `lucide-shape.ts` is a real architectural improvement (single-responsibility, matches "Design System First" — pure data transform vs. stateful component), not just a test-tooling dodge. Cost: one more file, one more import hop in `ritual-dial.ts`.
- **Frontend tests gated into `task test` (§B) rather than a separate CI job:** reuses the exact mechanism that already gates Go tests (`_publish` → `task: test`), so no new CI wiring, no second "did we remember to run this" surface. Cost: `task test` (and thus every `task publish:*`) is now slower by the frontend suite's ~10-20s — accepted, matches the existing "RED TESTS ABORT THE DEPLOY" principle already stated for Go.
- **`@rollup/plugin-commonjs` + `@web/dev-server-rollup` attempted and abandoned:** the "proper" generic fix for `@web/test-runner`'s CJS gap (Q8) didn't converge within reasonable effort — installed, wired per documented usage, confirmed the plugin's hooks existed, but `svgpath` still failed to resolve under both an indirect (`svgpath/index.js`) and a direct (`svgpath/lib/svgpath.js`, plain `module.exports = SvgPath`) import shape. Reverted cleanly (uninstalled, config restored) rather than leave a half-working dependency in `package.json`. The `lucide-shape.ts` extraction sidesteps the problem instead of solving it generically — future CJS-import test needs will hit the same wall and should either repeat the extraction pattern or invest in properly debugging the rollup-adapter wiring.

## Verification criteria

**§A:**
1. New regression test (Phase A step 5-6) passes, asserting `BytesDone <= BytesTotal` at every tick of a second chained pull/push whose plan is smaller than the first's cumulative transfer.
2. Manual repro: run two chained sessions locally (`task gui:run:dev:remote`, stop, start again with a small delta pending) and confirm the dial climbs 0→100% on the second run instead of opening already past 100% or (for push) snapping instantly to full.
3. `go test ./internal/gui/projection/...` green; no regression in the existing `TestProjection_*` suite (Q3's tests keep passing — they test single-flow correctness, which is unchanged).

**§B:**
1. `frontend/src/ui/lucide-shape.test.ts` — 7/7 pass, exercising the real installed `lucide` data for every dial glyph.
2. `task test` (Go + frontend, the actual `_publish` gate) green end-to-end — confirmed by running it directly.
3. `npm run build` (real Vite production build, not the test runner) succeeds — confirms the extraction didn't change what ships.
4. `build/windows/check-icon.ps1` correctly passes on a real branded exe and fails (exit 1) on one with no icon resource — confirmed both branches directly.
5. **Not verified:** whether either guard would have actually caught what the friend saw — no repro exists to check against (Q7). This is hardening against a plausible failure mode, not a confirmed fix.

## Open questions for requestor

- **Can the friend still reproduce it?** A screenshot of the dial (blank circle vs. garbled shape vs. something else) and their `<root>/logs/<ts>.log` are the only path from "hardened" to "confirmed and fixed" for the actual reported instance.
- **Was the friend's binary a fresh install/download, or did it arrive via autoupdate** ([[037-autoupdate]] — the `minio/selfupdate` in-place swap)? If autoupdate, was it a CI-published build ([[049-ci-publish-workflow]], currently Draft) or a locally-`task publish`'d one?
- Should the same `shapeToD` regression guard be extended to `run-addresses.ts`'s copy/check icons and `run-console-link.ts` (both use the same lucide→SVG pipeline, neither covered by this session's test)? Deferred — not reported as broken, and §Design B item 1's extraction pattern would need to be repeated for each.
- Is `frontend/build:frontend`'s `npm install` (not `npm ci`, `build/Taskfile.yml:22`) worth tightening to `npm ci` for stricter reproducibility across dev/CI? Not implicated here (lockfile and installed `lucide`/`gsap`/`svgpath` versions matched exactly), but it's the one remaining "resolved fresh, not truly pinned" gap in the dependency chain this investigation touched.

## Implementation Results — §A (2026-07-19)

**Confirmed already-coded (from an earlier pass this session, before §B's redirect):** `internal/gui/projection/projection.go` — `Projection` struct fields `lastRemoteDown`/`lastRemoteOps`/`pullBaseline`/`pushOpsBaseline`; `onTick` unconditionally tracks the raw counters before its pipeline-stage gate and baseline-subtracts them for both the pull (`s.Data - p.pullBaseline`) and push (`Ops.Done - p.pushOpsBaseline`) branches; `onStateChanged` captures each baseline from the tracked raw value on entry to `StagePulling`/`StagePushing` (factored into a small `baselineOnStageEntry` helper during lint cleanup — see §C `golangci-lint` note).

**New this pass:** the TDD tests from §Testing Plan, added to `internal/gui/projection/projection_test.go`:
- `TestProjection_ChainedPull_BytesDoneResetsPerFlow` — reproduces the exact log scenario (flow 1: 1.19 GiB / 814 files; flow 2: 61,655,570 B / 25 files, same cumulative counter carried over). Asserts `final.BytesDone == 0` and `final.BytesDone <= final.BytesTotal`.
- `TestProjection_ChainedPush_BytesDoneResetsPerFlow` — same shape for `StagePushing`/`Ops.Done` (flow 1: 62,066,935 B / 27 files; flow 2: 167,052,748 B / 109 files).

**Test results:** both new tests pass; full `internal/gui/projection` suite (46 tests) green; full repo `go test ./...` green; `golangci-lint run ./...` clean (0 issues, after two fixes — see §C).

**Deviation:** none from the design — the fix landed exactly as specified in §Design/§Implementation Plan.

## Implementation Results — §C: telemetry formatting moved to the backend (2026-07-19)

Mid-session, walking through *where the displayed progress number actually comes from* surfaced a broader issue: the frontend was independently computing several derived values from raw `ViewModel` fields — a KB/MB/GB and Mbps unit-conversion + formatting pass (`telemetry-format.ts`), and, worse, a **client-side fallback rate estimate** (`computeSpeedBps` in `ritual-app.ts`) that invented a speed number from a locally-remembered wall-clock/byte anchor whenever the backend's own rolling rate read 0, plus a **client-side ticking clock** (`setInterval`) for the "Live" uptime caption. User directive, refined through discussion: **the frontend is a projection** — it paints exactly what the backend says, holds no independently-evolving state, and never re-derives a number the backend already owns.

**Scope, as negotiated (see plan file / conversation for the full back-and-forth):**
- Size/speed **unit formatting** moves to Go — real data-shaping, one source of truth. Speed renders in **bytes/s** (not bits), matching what was already on screen (`"12.3 MB/s"`), so there's zero visible change.
- **Percentage/ratio math** (`bytesDone / bytesTotal` for the dial ring) stays frontend — explicitly allowed ("ok to have"). No `Arc` field was added.
- **ETA copy** (seconds → "mm:ss", plus the "" / "Almost done" / placeholder branching) stays frontend — "stage texting is on frontend... copy does not belong to the backend." `EtaSeconds` was already backend-computed (design-log/028); unchanged.
- **The client-side speed-fallback estimate is deleted outright, not fixed in Go.** Rationale in the user's words: recomputing on the frontend "creates client thoughts on what can be wrong which are derived from duplicated hallucinated calculations" — if the backend hasn't sent fresh data, the honest thing is to show that (0 / placeholder), not paper over it with a guess.
- **The uptime clock also moves to the backend** — this reversed an initial (wrong) call to leave it as-is. User: "the backend also should drive the time, no intervals needed on the frontend." Resolved by having the backend drive **when** the number changes (a new 1Hz ticker) while the frontend still decides **how** to render it (reusing the existing `formatEta()` — no new Go duration-formatting code, no new violation of the ETA-copy rule).
- **Mandatory version bump** (`config.AppVersion` 2.0.1 → 2.0.2) — this is a real release: a `ViewModel`-contract change, riding the mandatory-update rail ([[037-autoupdate]]) so installed/friend clients pick it up automatically.

**Files changed (backend):**
- `internal/gui/projection/format.go` (new) — `formatSize`/`formatSpeed`/`mbpsToBps`/`pickUnit`/`fmtNum`, ported 1:1 from the former `telemetry-format.ts` `formatSize`/`formatSpeed`. No duration formatting here (stays frontend per the scope decision).
- `internal/gui/projection/viewmodel.go` — new `ViewModel` fields: `SizeDoneText`/`SizeTotalText`/`SizeUnit`, `SpeedText`/`SpeedUnit`, `UptimeSeconds int64` (same "0 means no value yet" convention as `EtaSeconds`).
- `internal/gui/projection/projection.go` — new `refreshFormattedFields()` helper (computes the five text fields from `BytesDone`/`BytesTotal`/`LogicalMbps`), called at the end of `onTick`, the end of the `PlanInfo` fold case, and `onStateChanged`'s `StageRunning` reset branch. New `playingStartedAt time.Time` field, set in the `running.ServerReadyInfo` fold case. New 1Hz `uptimeTicker` in `Run`'s `select` loop — while `Phase == PhasePlaying`, sets `UptimeSeconds = time.Since(playingStartedAt)` and emits; a no-op tick otherwise. `UptimeSeconds` reset to 0 in the `running.ServerStoppingInfo` fold case (covered elsewhere by the existing full-`ViewModel{}` resets).
- `internal/config/config.go` — `VersionPatch: 1 → 2`.
- Wails bindings regenerated (`task gui:bindings`) — `frontend/bindings/ritual/internal/gui/projection/models.ts` picked up all six new fields.

**Files changed (frontend):**
- `frontend/src/ui/telemetry-format.ts` — `formatSize`/`formatSpeed`/`pickUnit`/`fmt`/the KB/MB/GB consts/`SpeedParts`/`SizeParts` types all removed (confirmed zero remaining references before deleting). `formatEta`/`ETA_PLACEHOLDER` kept.
- `frontend/src/ui/dial-telemetry.ts` — properties changed from `speedBps`/`bytesDone`/`bytesTotal` to `sizeDoneText`/`sizeTotalText`/`sizeUnit`/`speedText`/`speedUnit` (ready text, bound directly) plus `bytesTotal`/`logicalMbps` kept as raw numbers purely for the existing `planned`/`rushing` placeholder gates. No more `formatSize`/`formatSpeed` calls.
- `frontend/src/ritual-app.ts` — deleted `computeSpeedBps`, `trackTransferBeat`, `isTransferPhase` (its only caller), `transferStartedAt`/`transferStartBytes`, `MBPS_TO_BPS`, the `speedBps` `@state` field, and the entire uptime-clock apparatus (`startUptime`/`stopUptime`/`uptimeTimer`/`runStartedAt`/`uptimeSub` `@state`, the `AppCtx.uptimeSub`/`effectiveSpeedBps` fields, and the `startUptime()`/`stopUptime()` calls in `applyVm`). `PHASE_VIEW[PhasePlaying].sub` now reads `(vm) => formatEta(vm.uptimeSeconds)` directly. `Derived.telemetry` and the `<dial-telemetry>` template binding updated to the new field set. `arcFromBytes`/`etaSub` unchanged.
- `frontend/src/ui/dial-telemetry.stories.ts` + `frontend/src/ui/dial-composition.stories.ts` — both had live-simulation demos passing raw `bytesDone`/`speedBps` straight to `<dial-telemetry>`; each gained a small local `demoFormatSize`/`demoFormatSpeed` helper (legitimate fixture code — simulating what Go would have sent, not production display logic) so the existing range-slider controls still work while the component itself only ever receives ready text.

**Test/verification results:**
- `go build ./...`, `go vet ./...` clean; full `go test ./...` green (every package, not just projection).
- `golangci-lint run ./...`: **0 issues**, after two fixes surfaced mid-pass — `gofumpt` import-grouping (reverted `gofumpt -w`'s stdlib/local split back to this file's established single-group style) and `gocyclo` (`onStateChanged` hit 17 against a 15 budget once the earlier §A baseline-capture switch was in place; extracted it into `baselineOnStageEntry`, a pure complexity-budget split with no behavior change).
- `npx tsc --noEmit` clean.
- `npm run test`: **178/178** passing (unchanged from §B — no frontend tests target `ritual-app.ts`/`dial-telemetry.ts` directly, a pre-existing gap, not one this pass introduced).
- `npm run build` (real Vite production build): succeeds, single `main-*.js` chunk.
- **Live smoke test:** built `bin/ritualdev-local.exe` (`task gui:build:dev:local`, which also re-ran the §B `check-icon.ps1` gate — passed), launched it, confirmed a clean boot with no crash/panic and a well-formed `[snap]` log line sequence, and — notably — the autoupdate check line reads `update check: 2.0.2 — no remote build (prefix empty)`, live confirmation the version bump is wired end-to-end into the running binary. Did not click through a full pull/push/Playing cycle interactively (no UI-automation capability available this session) — the chained-flow behavior and the uptime ticker are covered by the `internal/gui/projection` unit tests instead; an interactive pass is recommended before shipping.

**Deviations from the plan:**
1. `AppCtx.effectiveSpeedBps` and `AppCtx.uptimeSub` were removed entirely rather than left unused — the plan only explicitly called out deleting the *producers* of those values; removing the now-dead `AppCtx` fields and the `isTransferPhase` helper (whose only caller, `trackTransferBeat`, was deleted) followed naturally and keeps the diff honest about what's actually still live.
2. Two `golangci-lint` fixups (`gofumpt` grouping, `gocyclo` extraction) weren't anticipated in the plan — normal lint-gate friction from the combined size of the §A+§C changes to `projection.go`, not a design change.

**Files changed:**
- `frontend/src/ui/lucide-shape.ts` (new) — `shapeToD` + `LucideChild`/`LucideIcon` types, zero dependencies beyond types.
- `frontend/src/ui/lucide-shape.test.ts` (new) — 7 tests, one per dial glyph, run `shapeToD` over the real installed `lucide` package's icon-node arrays.
- `frontend/src/ui/dial-glyphs.ts` (new) — `compoundD`/`GLYPHS`/`dFor`/`DialGlyph`, imports `shapeToD` from `lucide-shape.ts` plus `lucide` + `svgpath` directly.
- `frontend/src/ui/ritual-dial.ts` (edited) — dropped its inline `shapeToD`/`compoundD`/`GLYPHS`/`dFor`/lucide-icon imports; now `import { dFor, type DialGlyph } from "./dial-glyphs"`.
- `build/Taskfile.yml` (edited) — new `test:frontend` task (`npm run test` in `frontend/`, deps on `install:frontend:deps`).
- `Taskfile.yml` (edited) — root `test:` task now runs `common:test:frontend` after the two Go test tasks.
- `build/windows/check-icon.ps1` (new) — `ExtractIconEx`-based icon-resource-count check; plain ASCII only (see deviation below).
- `build/windows/Taskfile.yml` (edited) — `build:native` runs the icon check (Windows-only) immediately after `go build`, before the `.syso` cleanup step.

**Test results:**
- `npm run test` (frontend, `@web/test-runner`): **178/178 passing** (171 pre-existing + 7 new), 0 failed, ~9-18s.
- `npx tsc --noEmit` (frontend): clean, no errors.
- `npm run build` (real Vite production build): succeeds, single `main-*.js` chunk (~239 kB) — confirms `gsap`/`lucide`/`svgpath` all bundle correctly post-extraction, no dynamic-import chunk-splitting risk introduced.
- `task test` (repo root — the actual `_publish`/CI gate): **green end-to-end**, Go suites + the new frontend step, run directly to confirm the wiring is real and not just theoretical.
- `build/windows/check-icon.ps1`: verified both branches directly — passes (`icon check OK`, exit 0) against all three local `bin/*.exe`; fails correctly (exit 1) against `go.exe` (no custom icon resource).

**Deviations from the design above:**
1. **`@rollup/plugin-commonjs` + `@web/dev-server-rollup` generic CJS-interop fix was attempted first and abandoned** (see Trade-offs) in favor of the `lucide-shape.ts` extraction. Not in the original plan; discovered necessary once the first draft of a `ritual-dial.test.ts` DOM-fixture test failed to import at all.
2. **Original plan (mid-session) was a full `<ritual-dial>` DOM-fixture test** (`fixture(html\`<ritual-dial glyph="play">\`)`, assert `.glyph-path`'s rendered `d`/`getTotalLength()`). Abandoned — `ritual-dial.ts` transitively needs `svgpath` via `dial-glyphs.ts` regardless of the extraction, so a full-component fixture test is **structurally impossible** under this test runner's current config, extraction or not. `lucide-shape.test.ts` tests the highest-risk sub-part (tag→path conversion) instead, using real `lucide` data — a smaller but still meaningful guard.
3. **`build/windows/check-icon.ps1` had to be rewritten ASCII-only.** The first version (em-dashes, section-mark `§`) failed with bogus "missing string terminator"/"missing closing brace" errors on unrelated lines when run under Windows PowerShell 5.1 — a known class of gotcha: a BOM-less script containing multi-byte UTF-8 characters gets misdecoded under the system ANSI codepage, corrupting parsing far from the actual character. Fixed by removing all non-ASCII characters; verified both pass/fail branches afterward.
4. **§A (progress overflow) has no code changes this session** — design + Reproduction/Testing Plan only, per the original scope; this session's implementation effort went entirely to §B once the requestor redirected attention there.
