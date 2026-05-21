# 013 — Cutover: dialed GUI replaces stage layouts

- **Status:** Draft
- **Date:** 2026-05-21
- **Area:** GUI / Frontend
- **Related:** [007 One dial](007-hig-ux-coherence.md), [009 Telemetry](009-telemetry-hierarchy.md), [010 RUN addresses](010-run-addresses.md), [011 Dial frame](011-dial-frame-flip.md)

## Background

#007 specified replacing five per-stage components with one morphing `<ritual-dial>` inside a minimal `<ritual-shell>`. Phases A–B (substrate slice + dial primitive) shipped; #009/#010 built `<dial-telemetry>` and `<run-addresses>` as the dial's under-block companions; #011 froze the frame geometry. The composition is rehearsed live in Storybook (`Dial / Composition / Cycle`).

Phase C of #007 — "rewrite `ritual-app.ts`, delete `frontend/src/stages/`, delete `error-banner.ts`" — never landed. `ritual-app.ts:17-22, 48-63, 73-76` still routes `Stage` → `<stage-idle|downloading|running|uploading|locked>` inside the shell slot, and slots `<error-banner>`.

## Problem

Production renders the old five-screen UI. The dial only exists in Storybook. Every shipped behaviour from #007/#009/#010 — single morphing surface, telemetry hierarchy, copy-target address rows, frame stability — is dead code in the user-facing app.

## Design

`ritual-app.ts` becomes the composition root for the dial, mirroring `dial-composition-cycle` in `dial-composition.stories.ts`. Three children inside `<ritual-shell>`:

1. `<ritual-dial>` (always)
2. Under-slot: `<dial-telemetry>` during PREP/FINAL, `<run-addresses>` during RUN, nothing otherwise. Same fade wrapper as the story (`opacity` + `translateY`, `--motion-base`).
3. `<ambient-footer>` (already inside shell).

No banner slot. No stage children. No `error-banner`. Failure copy lives in `dial.label`/`dial.sub` per #007 §State table.

### VM → DialProps (pure mapper, in `ritual-app.ts`)

ViewModel fields available (verified from `frontend/bindings/ritual/internal/gui/projection/models.ts`): `stage, progress, bytesDone, bytesTotal, filesDone, filesTotal, speedMbps, logicalMbps, label, errorText, lockHolder, readyLight, addresses`.

```ts
type Derived = {
  dial:        { state: DialState; arc: number; glyph: DialGlyph; label: string; sub: string };
  underSlot:   "telemetry" | "addresses" | null;
  telemetry?:  { speedBps: number; bytesDone: number; bytesTotal: number };
  addresses?:  JoinAddress[];
};

function deriveView(vm: ViewModel): Derived {
  if (vm.errorText) return failView(vm);
  switch (vm.stage) {
    case Stage.StageIdle:
      return vm.lockHolder
        ? idleLockedView(vm.lockHolder)
        : idleView();
    case Stage.StageDownloading: return prepView(vm);
    case Stage.StageRunning:     return runView(vm);
    case Stage.StageUploading:   return finalView(vm);
    case Stage.StageLocked:      return idleLockedView(vm.lockHolder);
    case Stage.StageFailed:      return failView(vm);
  }
}
```

Per-state shape:

| Stage         | dial.state | dial.arc                          | dial.glyph | dial.label              | dial.sub          | underSlot   |
|---------------|------------|-----------------------------------|------------|-------------------------|-------------------|-------------|
| Idle          | `idle`     | 0                                 | `play`     | `Start`                 | `""`              | `null`      |
| Idle+Locked   | `idle`     | 0                                 | `x`        | `${lockHolder} is playing` | `Tap to check again` | `null` |
| Downloading   | `prep`     | `progress`                        | `download` | `Getting ready`         | `formatEta(eta)`  | `telemetry` |
| Running       | `run`      | 1 (hold drains)                   | `stop`     | `Ready to play`         | `formatEta(uptime)` | `addresses` |
| Uploading     | `final`    | `progress`                        | `upload`   | `Saving`                | `formatEta(eta)`  | `telemetry` |
| Failed        | `fail`     | last seen `progress` (carry-over) | `x`        | `Couldn't finish ${noun}` | `Tap to try again` | `null`      |

`noun` per #007: PREP→"getting ready", RUN→"running the server", FINAL→"saving".

### ETA / uptime derivation

`<dial-telemetry>` consumes `speedBps`, but VM ships `speedMbps`. Convert at the boundary: `speedBps = speedMbps * 1_000_000 / 8`. ETA from `(bytesTotal − bytesDone) / speedBps`, snapped via the same `snapEta` logic the story uses (rounds to 1s / 10s / 1min). Uptime in RUN: accumulate from a `runStartedAt` captured on first `stage === Running` view.

Carry-over `arc` for FAIL: track `lastProgress` in `RitualApp` state; when `errorText` arrives, render the dial with that frozen value. Reset on next non-fail VM.

### Frame-anchor preservation (#011)

`<ritual-shell>` reserves `min-height: 480px` for the dial column. Telemetry / addresses occupy the under-slot below, with the same opacity+translateY transition the Storybook composition uses — so dial position is constant across all states, no FLIP.

### Settings / inputs

#007 specified `<settings-gear>` + `<settings-sheet>` (Advanced: port + memory; Folder; Logs). Neither was built. `<ambient-footer>` ships today with one `log` button — covers `Show logs`. `Open folder` and Advanced inputs have **no surface**.

Resolved: hardcode `start(25565, 4096)` defaults at the dial's `tap` handler. Settings sheet is a separate later log; cutover does not block on it.

## Implementation Plan

### Phase A — wire dial into `ritual-app.ts`

1. Replace `ritual-app.ts` body: drop stage imports + `stageBody()` + banner slot; render `<ritual-dial>` + under-slot wrapper + `<ambient-footer>` already in shell.
2. Add `deriveView(vm)` pure function in same file.
3. Track `lastProgress` and `runStartedAt` as `@state` for FAIL carry-over + RUN uptime.
4. `tap` handler: dispatch `start(25565, 4096)` from IDLE, `recheck()`/no-op from LOCKED+IDLE (`recheck` doesn't exist yet — call `start` with same defaults; lock will re-resolve or re-fail), `retry()` from FAIL (call `start`).
5. `hold-commit` handler: `stop()`.
6. Under-slot fade wrapper: same CSS as `dial-composition.stories.ts:211-224`.

### Phase B — delete legacy

7. `git rm -r frontend/src/stages/` (5 components + 5 stories + `_anim.ts` + `format.ts` + `error-banner.*`).
8. Verify no other importer (`grep -rn 'from "./stages' frontend/src`).
9. Remove `<slot name="banner">` from `ritual-shell.ts` plus its `::slotted([slot="banner"])` CSS. No more banners (resolved OQ3).

### Phase C — verify

10. `pnpm build` clean.
11. Storybook: existing `Dial / Composition / Cycle` story still loads (reference composition; cutover does not change it).
12. **New `App / Live` story** driving `<ritual-app>` directly via mocked `wails-api` transport (#005 `setTransport` harness). Story walks IDLE → PREP → RUN → FINAL → IDLE plus fail-at-each-stage paths plus IDLE+lockHolder. This is the primary verification surface for the cutover — the existing composition story exercises the same shape but not `ritual-app.ts`'s VM-mapper.

## Verification Criteria

1. `ls frontend/src/stages/` returns "no such file or directory".
2. `grep -rn 'error-banner\|stage-idle\|stage-downloading\|stage-running\|stage-uploading\|stage-locked' frontend/src` returns nothing.
3. `ritual-app.ts` imports only: `lit`, `lit/decorators`, `wails-api`, `./ui/ritual-shell`, `./ui/ritual-dial`, `./ui/dial-telemetry`, `./ui/run-addresses`, `./ui/telemetry-format`.
4. Cold-loaded app renders dial centered, no banner, no card chrome.
5. Failure path: dial stays at last `progress`, state shifts to `fail`, `label` reads end-user noun ("getting ready" / "running the server" / "saving"). No `R2`, no `connection reset`, no path strings.
6. RUN under-slot lists addresses with protocol labels preserved (#007 Q2 — `LAN` / `Tailscale` kept).
7. PREP/FINAL under-slot shows speed + size row driven by `speedMbps→speedBps` conversion.
8. Dial bounding-box top-edge stable (≤2px drift) across IDLE → PREP → RUN → FINAL → IDLE walk.

## Trade-offs

| Choice                                              | Gain                                                 | Cost                                                  |
|-----------------------------------------------------|------------------------------------------------------|-------------------------------------------------------|
| Delete `stages/` outright, no migration phase       | One PR, no dual-render branch                        | Any UI behaviour only present in old stages is lost — confirm none survives by inspection before merge |
| Hardcode `start(25565, 4096)` defaults              | Unblocks cutover; settings sheet stays a follow-up   | First-run users can't change port/memory until OQ1 resolves |
| `<error-banner>` removed wholesale                  | #007 verification #8 (no banners) holds              | Lose the explicit infra-leak surface — must verify fail copy stays user-facing |
| RUN `arc = 1` constant (per dial impl)              | Matches dial-composition story; no extra wiring      | RUN ignores `progress`; not a regression — RUN has no quantitative progress |
| FAIL `arc` carry-over via local `@state`            | Continuity per #007 Q11                              | Carry-over lives in `RitualApp`, not VM — VM-side `lastProgress` could be cleaner long-term |

## Open Questions

> OQ1. ~~Settings reachability.~~ **Resolved.** Hardcode `start(25565, 4096)`; settings sheet is later work.

> OQ2. LOCKED tap behaviour. #007 §State-table calls for `recheck()`, but no `Recheck` method exists in `wails-api`. Reuse `start(25565, 4096)` (server will re-resolve the lock and re-fail or unlock), or add a `Control.Recheck` binding? **Status:** undecided — proposed default is reuse `start`, revisit if behaviour feels wrong in real build.

> OQ3. ~~`<slot name="banner">` in `<ritual-shell>`.~~ **Resolved.** Drop slot + CSS.

> OQ4. ~~Storybook coverage for the cutover.~~ **Resolved.** New `App / Live` mocked-transport story drives `<ritual-app>` end-to-end through every state.

## See also

- 007 §Q8 "fold" — this log executes that fold.
- 011 — frame geometry stays as-is; cutover does not reopen positioning.
- `dial-composition.stories.ts` — reference composition. The cutover lifts that shape verbatim into `ritual-app.ts`.
