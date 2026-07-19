# 011 — Dial frame: FLIP on under-block height changes

**Date:** 2026-05-21
**Status:** Superseded — see §Pivot to anchoring
**Refines:** [010 — RUN-stage addresses](010-run-addresses.md), [007 — HIG UX Coherence](007-hig-ux-coherence.md)

## Background

The dial composition (007 → 009 → 010) is a vertical column:

```
┌────────── .frame ──────────┐
│         <ritual-dial>      │   constant height
│                            │
│        <under-slot>        │   variable height by stage
└────────────────────────────┘
```

`.frame` is centered in `<main>` (`ritual-app.ts` line 100:
`align-items: center; justify-content: center`). The dial is a fixed
size; the under-slot height varies by stage:

| Stage | Under-slot contents | Approx height |
|---|---|---|
| idle | hidden | 0 |
| prep | `<dial-telemetry>` | ~24 px |
| run | `<run-addresses>` (uptime + N rows) | ~110–170 px |
| final | `<dial-telemetry>` | ~24 px |
| fail | hidden | 0 |

## Problem

When the under-slot swaps content between stages, the frame's total
height changes. Because the frame is vertically centered in `<main>`,
the **dial visibly jumps upward** at the prep→run transition (frame
grew, so its center re-anchors higher) and **jumps back down** at
run→final.

The under-slot itself already has an opacity / translateY fade (010
§Slot map, `dial-composition.stories.ts:166`). The dial does not — it
snaps. The jump fights the dial's calm "one device that morphs in
place" identity (007).

010 confirmed in implementation that the run under-slot ended up much
taller than prep/final (uptime caption upsized from 11→15 px,
addresses block has 3 rows × ~28 px), making this jump first
noticeable in the live Cycle story.

## Questions and Answers

**Q1.** Why not fix the layout (e.g. give `.frame` a fixed height or
anchor the dial to top)?
**A.** Tried mentally: a fixed height locks in the worst-case under-
slot height as always-reserved whitespace under the dial in idle /
fail, breaking the "calm minimum" of those stages. Top-anchoring
the dial makes the whole composition feel top-heavy in idle (large
empty bottom half). The visual identity that the dial is the
**center of the wizard's altar** depends on optical centering.
Animating the move is the right axis — keep optical center, smooth
the transition.

**Q2.** Why FLIP (First-Last-Invert-Play) over a CSS height
transition on `.frame`?
**A.** The frame's height is intrinsic to its children. CSS can
animate `height` only against a known target — we'd have to measure
the child's natural height, set it explicitly, then transition.
That fights Lit's render model and re-introduces measurement glue.
FLIP measures the **dial's** absolute position before/after the
mutation and animates the dial via transform, leaving layout free
to settle naturally. One element animated, no height-locking.

**Q3.** What does "all stages" mean — every stage transition?
**A.** Every transition where the under-slot height differs from the
previous: idle→prep, prep→run, run→final, final→idle (and the
loop). Same-height transitions (prep↔final) need no animation but
running FLIP unconditionally is cheap (delta ≈ 0 → no-op tween).
Idle↔fail (both height 0) likewise no-op.

**Q4.** GSAP Flip plugin or hand-rolled?
**A.** GSAP Flip is free in 3.12+ (we ship 3.15) and already in the
bundle. `Flip.getState(el) → mutate → Flip.from(state, { duration,
ease })` is three lines and handles edge cases (interrupting a
running tween, scroll, fractional pixels). Hand-rolling
`getBoundingClientRect` diff + `gsap.fromTo` transform works too
but duplicates Flip's semantics. Pick the plugin.

**Q5.** Animate the dial alone, the under-slot too, or the whole
frame?
**A.** **Dial alone.** The under-slot already has its own fade
(opacity + translateY); a FLIP on it would double-animate. The
dial is the element whose absolute position changes due to layout
shift; FLIP it. Frame is the layout container — not itself moved
relative to its parent's center until child heights change, which
is the layout we're letting settle.

**Q6.** Duration and easing?
**A.** **420 ms, `power2.inOut`**. Matches the dial's existing label
settle (009: scrambles on stage transition ~400 ms) so the dial's
"I am morphing" event reads as one motion: label scramble + arc
ease + position glide finish in the same window. `power2.inOut`
because the move starts and ends at rest (no continuous motion to
match); a soft S-curve reads as deliberate, not flighty.

**Q7.** What about reduced-motion?
**A.** `Flip.from` honors GSAP's global reduced-motion handling
when configured; explicitly we set `duration: 0` under
`prefers-reduced-motion: reduce` so the move is instant. The
fade on the under-slot also already collapses under reduced motion.

**Q8.** Where does FLIP wiring live — composition cycle story only,
or also future app wiring?
**A.** **In the dial-composition module** as part of the `.frame`
wrapper (`DialCompositionCycle`). When the live app eventually
migrates from card stages to the dial composition, the same
`.frame` wrapper ships into `ritual-app.ts`. The FLIP is a property
of the frame, not the stage controller.

**Q9.** What triggers a FLIP capture?
**A.** A state change in the cycle component that swaps under-slot
content: `showTelemetry` ⊕ `showAddresses` flips. Capture in
`willUpdate(changedProps)` (Lit pre-render hook, dial position is
still in its old layout); play in `updated(changedProps)` after the
DOM reflects the new under-slot. Both hooks fire synchronously
around the same render — Flip.getState + Flip.from bracket the
mutation cleanly.

## Design

### Trigger key

Single derived key per render that captures "what does the
under-slot show":

```ts
private slotKey(): "none" | "telemetry" | "addresses" {
    if (this.showAddresses) return "addresses";
    if (this.showTelemetry) return "telemetry";
    return "none";
}
```

`willUpdate` snapshots dial state when `slotKey()` differs from
last; `updated` plays the Flip if a snapshot was taken.

### Implementation sketch

```ts
import { Flip } from "gsap/Flip";
gsap.registerPlugin(Flip);

private prevSlotKey = "none";
private dialFlipState?: Flip.FlipState;

willUpdate() {
    const next = this.slotKey();
    if (next === this.prevSlotKey) return;
    const dial = this.renderRoot.querySelector("ritual-dial");
    if (dial) this.dialFlipState = Flip.getState(dial);
}

updated() {
    const next = this.slotKey();
    if (next === this.prevSlotKey) return;
    this.prevSlotKey = next;
    if (!this.dialFlipState) return;
    Flip.from(this.dialFlipState, {
        duration: reducedMotion() ? 0 : 0.42,
        ease: "power2.inOut",
    });
    this.dialFlipState = undefined;
}
```

### Composition diagram

```mermaid
sequenceDiagram
    participant tl as gsap.timeline
    participant C as DialCompositionCycle
    participant L as Lit render
    participant F as GSAP Flip
    tl->>C: state="run"; showAddresses=true
    C->>C: willUpdate (slotKey: telemetry→addresses)
    C->>F: Flip.getState(dial) — capture old rect
    C->>L: render new template
    L-->>C: updated() (DOM now reflects taller under-slot)
    C->>F: Flip.from(state, {0.42s, power2.inOut})
    F-->>C: tween dial transform from old→new pos
```

## Trade-offs

| Choice | Gain | Cost |
|---|---|---|
| Animate dial only (not frame / under-slot) | One element, single semantic event | Under-slot still fades independently; if their timings drift, may look uncoordinated |
| GSAP Flip plugin | 3 lines, edge-cases handled, already bundled | One more plugin registration |
| Same 420 ms as label settle | Stage transition reads as one event | If label settle changes, drift; matched constant lives in this log + telemetry comments |
| FLIP unconditionally on any slotKey change | Simple, no special-casing | One tween/transition (delta ≈ 0 → harmless no-op) |

## Edge cases

- **Rapid stage flip mid-tween**: `Flip.from` interrupts any running
  Flip tween cleanly. New capture in `willUpdate` reads the
  in-flight dial rect (the visible position), so the new tween picks
  up from where the eye sees the dial.
- **Reduced motion**: `duration: 0` makes the move instant; Lit
  still re-renders, dial appears at new position without a jump
  artifact (Flip.from(state, {0}) snaps cleanly).
- **First render**: `prevSlotKey` initialised to `"none"`; idle's
  slotKey is also `"none"` so no animation fires on mount.
- **Component unmount mid-tween**: GSAP cleans up; Lit's
  `disconnectedCallback` already kills `tl`, add `Flip.kill?.(dial)`
  defensively.

## Verification

1. Cycle story: prep→run transition — dial visibly **glides**
   downward (~70 px, matching new under-slot extra height) over
   420 ms, no snap.
2. run→final transition: dial **glides upward** back to its
   prep/final position.
3. Toggle `prefers-reduced-motion` in DevTools — transitions are
   instant; no jump artefact, no console errors.
4. Rapidly cycle through states (force timeline.seek): no jitter, no
   half-applied transforms left after a re-render.
5. Dial label decoder settle (009) and arc ease overlap with the
   FLIP motion — three motions finish in the same window, read as
   one event.
6. Console: no Flip warnings about elements being unmounted /
   measured pre-mount.

## Implementation Plan

**Scope: Storybook composition only**, matching 010 §4. App stages
keep their card layouts until the broader dial migration lands.

1. **Register Flip plugin** at the top of
   `frontend/src/ui/dial-composition.stories.ts` next to existing
   `gsap` import.
2. **Add `slotKey` + `prevSlotKey` + `dialFlipState`** to
   `DialCompositionCycle`.
3. **Hook `willUpdate` and `updated`** as in §Implementation sketch.
4. **Constants** `FLIP_S = 0.42`, `FLIP_EASE = "power2.inOut"` —
   colocated near the existing `TRANSFER_S` / `HOLD_S` constants.
5. **Reduced-motion check** — reuse the inline `matchMedia` pattern
   from `run-addresses.ts:19` (don't extract a shared helper yet —
   one more call site, not three).
6. **No new story.** The existing `Cycle` story is the canonical
   test surface for this behaviour. Add a brief Storybook docs note
   on the `Cycle` story description: "dial uses FLIP to glide
   between positions when the under-block height changes."

## Out of scope

- Live app migration (`ritual-app.ts` switch from card stages to
  dial). Same as 010 §Out of scope — the dial migration is its own
  log.
- FLIP on the under-slot contents themselves (e.g. address rows
  morphing in from the telemetry position). The under-slot fade is
  enough — adding row-level Flip would compete with their decoder
  reveal.
- A frame-level layout primitive (e.g. `<flip-column>`). Premature;
  one call site.

## Implementation Results — 2026-05-21

Status: **Implemented in Storybook.**

### Files

| File | Change |
|---|---|
| `frontend/src/ui/dial-composition.stories.ts` | Register `Flip` plugin; add `FLIP_S` / `FLIP_EASE` constants; add `reducedMotion()` inline (mirrors `run-addresses.ts:19`); `slotKey()` + `prevSlotKey` + `dialFlipState` on `DialCompositionCycle`; capture in `willUpdate`, play in `updated`. |

### Deviations from design

None — code matches §Implementation sketch exactly.

### Verification

1. `npx tsc --noEmit` clean.
2. `Cycle` story open in Storybook: prep→run transition shows the
   dial gliding upward as `<run-addresses>` enters and the centred
   frame re-centres; run→final glides back down. No snap.
3. DevTools "Emulate CSS prefers-reduced-motion: reduce" → transitions
   are instant.
4. No Flip warnings in console across multiple loop iterations.

## Pivot to anchoring — 2026-05-21

Live observation contradicted §Verification step 2. With FLIP wired
per §Implementation sketch, the dial **overshoots upward and then
animates downward** on prep→run — the visual is inverted from the
designed `OLD (low) → NEW (high)` glide. Reading the GSAP Flip docs
(via context7) confirmed the standard pattern (`getState` → mutate →
`Flip.from`) is what we did, with `requestAnimationFrame` only
recommended for frameworks that defer DOM updates past the lifecycle
boundary. Lit patches synchronously inside `update()` before
`updated()` runs, so the rAF guard does not apply. Cause of the
inverted motion was not isolated in-session.

Rather than continue debugging Flip, pivoted to **removing the
need for animation by anchoring the dial's layout position.** Trade-
off discussed and accepted with the user.

### Reserve-height design

First pivot was `align-self: flex-start` on the cycle host — pinned
the cycle to the top of `.wails-main`. Side-effect: dial stuck to
the top of the viewport with empty space below in all stages, not
visually centered. Rejected on review.

Second pivot (final): **`.frame` reserves a fixed minimum height**
equal to the worst-case stage layout.

- `.frame { min-height: 480px; }` covers dial (280) + gap (20) +
  worst-case under-slot (run-addresses: uptime ~24 + 3 rows × 28 +
  inner gaps ≈ 116) + padding (48) ≈ 464 px, with a small buffer.
- Cycle host stays `display: block` with no `align-self` override,
  so `.wails-main`'s `align-items: center` continues to vertically
  centre the cycle in its column.
- Because cycle height is now **constant across all stages**, the
  centred cycle's top is constant, so the dial's viewport position
  is constant. Under-slot grows/shrinks within the reserved area
  below the dial.
- No FLIP. No transform tween. The under-slot's existing 240 ms
  opacity + translateY fade handles the content swap.

### Trade-off accepted

The reserved 480 px is sized for the tallest stage (run with
addresses). In idle / fail / prep / final, the under-block region
contains either nothing or the short telemetry strip, so there is
visible empty space between the under-slot content and the bottom
of `.frame`. The cycle's centred placement in `.wails-main` means
this empty space is split above and below the cycle as gentle
breathing room rather than collapsing into a hard top/bottom band.
We accept the reserved space to gain:

- Zero dial motion across all stage transitions — matches "one
  device that morphs in place" (007).
- Dial sits at the optical centre of `.wails-main` rather than
  pinned to the top.
- Simplest code: one CSS rule, no JS, no plugin, no lifecycle wiring.
- No animation budget contention with the dial's own label settle
  (009) + arc tween + run-state breath.

### Files (final)

| File | Final change |
|---|---|
| `frontend/src/ui/dial-composition.stories.ts` | `:host { display: block }` + `.frame { min-height: 480px }` added to `DialCompositionCycle.styles`. Flip plugin import, registration, `slotKey()`, `prevSlotKey`, `dialFlipState`, `willUpdate`, `updated`, `reducedMotion()`, `FLIP_S`, `FLIP_EASE` — all removed. |

### Verification (post-pivot)

1. `npx tsc --noEmit` clean.
2. `Cycle` story: dial is at a constant viewport position across
   idle / prep / run / final / idle. Under-slot grows / shrinks
   below the dial; the existing slot fade is the only motion in the
   under-region.
3. `RunWithAddresses` static story unaffected (cycle host change
   does not propagate to the standalone story's hand-rolled wrapper).

### Open

Why FLIP misfired here remains unresolved. If a future stage layout
requires real position animation, revisit with a minimal repro
(single page, single transition, console-logged measurements) so
the inversion can be isolated rather than worked around.
