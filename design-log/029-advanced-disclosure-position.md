# 029 — Advanced disclosure clipping below viewport

**Date:** 2026-05-25
**Status:** Draft
**Related:** [[014-prep-advanced-settings]] (the disclosure being repositioned), [[023-disable-resize]] (locks viewport at 560×720 so the overflow is no longer escapable by resize), [[007-hig-ux-coherence]] (canvas tuning), [[011-dial-frame-flip]] (`.frame { min-height: 480px }` constraint).

## Background

`<prep-settings>` from [[014-prep-advanced-settings]] renders below the dial on IDLE only:

```ts
// ritual-app.ts:377-379
${d.dial.state === "idle"
    ? html`<prep-settings .config=${this.prep}></prep-settings>`
    : null}
```

It sits inside `<ritual-shell>`'s `.stage` slotted region, which is a vertical flex column with `padding: 150px var(--space-4) var(--space-4)` (ritual-shell.ts:122) and a 480px max-width per slotted child.

Vertical budget at the locked 560×720 viewport ([[023-disable-resize]]):

| Element                                | Approx. height |
|----------------------------------------|----------------|
| Stage top padding (150px)              | 150            |
| Dial canvas (`.dial` 280×280 + cluster)| ~340           |
| `gap: var(--space-5)`                  | ~24            |
| Under-slot (idle = empty, min-height) | 24             |
| `gap`                                  | 24             |
| `<prep-settings>` collapsed disclosure | ~48            |
| `<prep-settings>` expanded             | ~210           |
| Stage bottom padding (~16)             | 16             |
| **Total expanded**                     | **~836px**     |

720 - 836 = -116px overflow when expanded. Collapsed fits with ~22px to spare, but the user has to hunt for the disclosure summary near the bottom edge and the form clips on click.

## Problem

The Advanced disclosure clips off the bottom of the fixed window when expanded. Three contributing factors:

1. **150px top padding** is tuned for a centred dial-only layout from [[007-hig-ux-coherence]] / [[011-dial-frame-flip]] — pre-dates [[014-prep-advanced-settings]] adding a second under-dial block.
2. **`min-height: 480px`** on the dial frame ([[011-dial-frame-flip]]) reserves worst-case stage height — necessary for non-IDLE stages to avoid centre drift, but on IDLE the dial occupies less and the reserved space is dead weight pushing the disclosure down.
3. **No special-case for IDLE.** Other phases don't render the disclosure, so the layout never had to budget for it.

## Questions and Answers

**Q1.** Move prep-settings somewhere else entirely, or reposition within the existing flow?
**A.** Reposition within the existing flow. Moving to a corner / popover would conflict with [[007]]'s "single dial, no banners" doctrine and [[014]]'s explicit "inline disclosure, no popovers" decision. Keep it under the dial; just give it room.

**Q2.** Reduce the 150px top padding globally, or only on IDLE?
**A.** Only on IDLE. Other phases were tuned at 150px and the dial's vertical centring across stages depends on it ([[011]] §pivot). Conditioning by state attribute is cheap (the shell already takes `.state`).

**Q3.** How much to reduce?
**A.** Drop to ~60px on IDLE. Recovers 90px — enough to seat the expanded disclosure with ~-26px to spare; if that's still tight, also trim the `min-height: 480px` on the dial frame when on IDLE (the dial frame currently reserves space for the worst-case non-IDLE stage; IDLE doesn't need it).

**Q4.** Will the dial visually jump when transitioning IDLE → non-IDLE if padding differs?
**A.** Yes, slightly. Mitigation: CSS `transition: padding-top var(--motion-base, 220ms ease)` on `.stage` so the change animates instead of snapping. [[011]] §pivot already accepted small position shifts at stage boundaries; this is the same shape of motion.

**Q5.** Touch the dial frame `min-height: 480px`?
**A.** Only if Q3's padding-only fix doesn't fully clear the overflow. Per [[011]] the 480px was tuned to cover the worst-case non-IDLE stage. On IDLE the dial body is shorter; conditioning min-height by state is safe. Defer to Phase B if Phase A's padding change is insufficient.

**Q6.** Should the disclosure be open-by-default?
**A.** Out of scope for this log. [[014]] §JS open-state decided it stays closed-by-default. Reopening that question is a separate log.

**Q7.** Does this affect [[024-custom-titlebar]] (if Variant A/B ever ships)?
**A.** Yes, marginally. A custom titlebar steals ~50px from the top of the webview. Phase A's padding reduction makes that even tighter. If 024 happens, re-budget the IDLE padding (e.g. 30px instead of 60px) at that time.

**Q8.** Storybook coverage?
**A.** [[014]] shipped a `<prep-settings>` story standalone but not in-shell. Add an IDLE-state shell story showing dial + disclosure together so the layout regression is visible in storybook, not just in the live app.

## Design

### Phase A — IDLE-conditional padding

```ts
// frontend/src/ui/ritual-shell.ts — within static styles
.stage {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-5);
    padding: 150px var(--space-4) var(--space-4);
    box-sizing: border-box;
    transition: padding-top var(--motion-base, 220ms ease);
}
:host([state="idle"]) .stage {
    padding-top: 60px;  /* recovers 90px for the disclosure */
}
```

`ritual-shell` already takes `state` as a reflected attribute (per its `:host([state="..."])` halo color rules); the new selector slots in next to those.

### Phase B (conditional, only if Phase A insufficient)

Trim the dial frame's reserved canvas on IDLE:

```ts
// wherever .frame { min-height: 480px } lives — likely in <ritual-dial> or its host
:host([state="idle"]) .frame {
    min-height: 360px;  /* dial body + label/sub, no worst-case reservation */
}
```

## Implementation Plan

**Phase A.**

1. Edit `ritual-shell.ts` styles per Design.
2. Verify: open the app on IDLE, expand `<details>` — disclosure now fits with margin.
3. Verify: transition IDLE → PhaseDownloading → padding animates back to 150px, dial centres.

**Phase B (only if needed).**

1. Locate the `.frame { min-height: 480px }` declaration ([[011]]).
2. Add the IDLE override.
3. Re-verify in storybook + live.

**Phase C — storybook.**

1. New story: `ritual-shell.stories.ts` "IDLE with Advanced expanded" — shell + dial + expanded prep-settings, visible inside the 560×720 viewport frame.
2. Regression intent: catch future contributors changing padding / min-height without considering this overflow.

## Verification

- IDLE state with `<prep-settings>` collapsed: disclosure summary visible with ≥40px of bottom margin.
- IDLE state with disclosure expanded: form fully visible inside the 560×720 fixed viewport ([[023]]).
- Non-IDLE states unchanged — dial centring per [[011]] preserved.
- Transition IDLE → any non-IDLE state: padding animates over `--motion-base`; no visible jump.

## Trade-offs

- **State-conditional padding adds a layout branch.** Cost: one CSS rule. Worth it.
- **Dial visually shifts up on IDLE.** Counter: the user sees the dial *and* the affordance they came to use (configure port/memory). Net win.
- **Phase B (dial frame min-height) re-opens [[011]]'s "no animation needed" claim.** Acceptable — [[011]] reserved one canvas size; making IDLE's canvas smaller doesn't introduce per-stage churn, just shrinks one specific state. Defer unless Phase A is insufficient.

## Out of scope

- Repositioning prep-settings to a popover / sidebar / titlebar gear — see [[024-custom-titlebar]] Q5 for the gear option, deferred there.
- Adding more advanced fields beyond port + memory — separate log when a new field is actually needed.
- Default-open disclosure — [[014]] decision stands.
