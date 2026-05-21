# 010 — RUN-stage addresses: copy-on-row IP list, identity-decoder reveal

**Date:** 2026-05-21
**Status:** In Progress
**Refines:** [007 — HIG UX Coherence](007-hig-ux-coherence.md), [009 — Telemetry hierarchy](009-telemetry-hierarchy.md)

## Background

The dial UX (007 → 009) replaced the five-stage card layouts with a
single morphing dial + a quiet under-block. The under-block currently
carries transfer telemetry (speed · size) **only during prep + final**
(009 §2). RUN's dial reads `Ready to play` / `Hold to stop`; nothing
else.

The old `stage-running` card surfaced a "Share to join" address list
populated from `projection.AddressProvider` (localhost first, then
each non-virtual, non-loopback IPv4 with the bound port). As the dial
supplants `stage-running`, that surface drops on the floor.

## Problem

RUN means the player can be joined. The two questions a host
actually has during RUN are:

1. **Is the server up?** — answered by the dial (label + ring + glyph).
2. **What do I tell my friends?** — currently unanswered in the dial UX.

Without (2), the host has to leave the app (check IP via OS) or trust
that whoever joins already knows the address. Both are friction at
exactly the moment the app should feel "done — go play."

## Questions and Answers

**Q1.** Why all interfaces and not just the best LAN guess?
**A.** AddressProvider already filters down to reachable IPv4s; the
host knows which interface their friends are on, the app doesn't. A
single "best" guess that's unreachable from the peer's vantage is
worse than a short row list. ([Confirmed by user 2026-05-21.])

**Q2.** Where does the list live relative to the dial?
**A.** The under-block slot below the dial, swapping in when state
flips to RUN. Same vertical rhythm as 009's telemetry slot; one slot
that switches role by stage avoids stacking a third sibling under the
dial and matches the calm composition.

**Q3.** Decoder-v2 on an address row — does this violate
[[decoder-cadence-rule]]?
**A.** No. The rule forbids decoder on strings that **tick**
sub-settle-time. Addresses are pure identity: they reveal once on RUN
entry, then sit constant for the entire session. Settling once on
reveal is the desired animation (the rune-stone engraving is read).
This is the same shape as dial `label` settling on stage transition.

**Q4.** Decoder spans digits — doesn't 009's digit-presence heuristic
forbid that?
**A.** The heuristic exists inside `ritual-dial`'s `sub` slot, where
content alternates between identity copy and live measurement on the
same slot. The address row isn't that slot — its content never
switches roles. Decoder wraps the whole row (label + address) on
reveal, then is idle. No tick, no storm.

**Q5.** Whole row clickable vs. trailing copy button?
**A.** Whole row (HIG — large, forgiving hit targets; the row is the
unit of meaning). Trailing affordance still rendered (icon, not a
button border) as a visual hint that the row copies.

**Q6.** What about `127.0.0.1:25565` — useful enough to keep first?
**A.** Yes. Solo / local-multiplayer flow; guaranteed-valid even
offline; matches the existing AddressProvider contract. Order stays
localhost → host interfaces (provider already enforces).

**Q7.** Hover / focus / active treatment?
**A.** Hover: row background lifts `rgba(255,255,255,0.04 → 0.08)`,
no transform (calmer than the dial's own translateY; under-block
remains optically below the dial). Focus follows the global
focus-visible token. Active (copying): see Q9.
*User left the hover option open ("ain't sure") — calmest default
picked; revisit if it reads underweight against the engraved-stone
brand.*

**Q8.** Decoder reveal intensity?
**A.** Resolved: **reveal once + very slow per-element idle on both
parts.** Label and address each splash → settle on row mount, then
keep a generous idle profile (6–14 s per element, radius 1). Per-row
cadence is intentionally slow because aggregate motion compounds:
with N rows × 2 spans there are 2N independent idle timers running
in parallel, so the visible cadence in the address block is roughly
1/(2N) of any single decoder's interval. At N = 3 → ≈ 1.7 s aggregate
between tweaks, at N = 1 → ≈ 10 s. Each scramble reads as a single
deliberate event, never a storm. [[decoder-cadence-rule]] holds.

**Q9.** What does a successful copy look like on the row?
**A.** Four layers fused into one motion event (~700 ms total):

1. **Breath** — single rise-and-fall pulse of the row border + outer
   glow in `--state-run` via the existing `--radiance` /
   `--radiance-hi` token chain. Row briefly picks up the dial's
   living-color identity.
2. **Icon swap** — trailing `copy` → `check` for the duration of the
   breath, then back.
3. **Address highlight** — monospaced address span flashes to full
   opacity + `--state-run` tint at the breath crest, fades with the
   release.
4. **Row micro-bounce** — GSAP `back.out` scale tween
   `1 → 1.018 → 1` over ~140 ms triggered at copy moment. Reads as
   a physical tap response on top of the optical glow.

No inline "Copied" label — the four-layer event speaks for itself.
All layers respect `prefers-reduced-motion`: breath becomes a 200 ms
opacity flip, micro-bounce suppressed, icon swap is instantaneous.

**Q8.** Keyboard support?
**A.** Each row is `role="button"`, `tabindex=0`, activates on
Enter / Space. Aria-label reads `"Copy <label> address <address>"`.

## Design

### Slot map

| Stage | Under-block content |
|---|---|
| idle | hidden |
| prep | `<dial-telemetry>` (speed + size) — unchanged |
| **run** | **`<run-addresses>` — new** |
| final | `<dial-telemetry>` (speed + size) — unchanged |
| fail | hidden |

Reuses the existing fade-in / translateY-on-enter behaviour already
applied to the telemetry slot (see `dial-composition.stories.ts`
`.telemetry-slot[data-shown]`).

### Component contract

```ts
// frontend/src/ui/run-addresses.ts
@customElement("run-addresses")
class RunAddresses extends LitElement {
  @property({ attribute: false }) addresses: JoinAddress[] = [];
}
```

Source of truth: `ViewModel.addresses` (already populated when the
projection enters StageRunning — see
`internal/gui/projection/projection_test.go:103`).

### Visual structure (per row)

```
┌──────────────────────────────────────────┐
│  localhost     127.0.0.1:25565       ⧉   │
├──────────────────────────────────────────┤
│  Wi-Fi         192.168.1.42:25565    ⧉   │
├──────────────────────────────────────────┤
│  Ethernet      10.0.0.7:25565        ⧉   │
└──────────────────────────────────────────┘
                                       ↑ copy icon (lucide `copy`)
                                          becomes ✓ + "Copied" on action
```

Typography:

| Slot | Treatment |
|---|---|
| label (`localhost`, `Wi-Fi`) | identity copy, decoder-v2 reveal + very slow idle (6–14 s per element, radius 1), ~70% opacity |
| address (`192.168.1.42:25565`) | `ui-monospace`, tabular-nums, 95% opacity, decoder-v2 reveal + very slow idle (6–14 s per element, radius 1) |
| trailing icon | lucide `copy` → `check` on action, ~55% opacity |

Width: rows fill the dial's column width (`min(420px, 100%)`) to
match the dial's optical column. Row vertical padding ≥ 12 px so the
hit target clears HIG's ~44 px touch guidance even at the smallest
window.

### Decoder usage

The reveal pattern matches dial `label` (009 §4: "scrambles only on
stage transition — now actually settles, so the scramble reads as an
event"). Each address row mounts on RUN entry, decoder-v2 plays its
splash → settle, then the row sits plain for the rest of RUN. Idle
profile is slow (1.8–3.6 s, radius 1) — identical to the under-block
unit slot in 009 §Final decoder map, ensuring the wider page rhythm
matches.

The address text contains digits but the decoder-cadence-rule's
concern (decoder on per-frame measurements) does not apply: an
address never changes during a session. The
content-based digit-presence heuristic in `ritual-dial.renderSub()`
is a same-slot mode-switch detector, not a universal ban — it does
not apply here.

### Interaction

```mermaid
stateDiagram-v2
    [*] --> Idle: row mounted
    Idle --> Pressed: pointerdown / Enter / Space
    Pressed --> Copied: clipboard.write OK
    Pressed --> Idle: clipboard.write fail
    Copied --> Idle: 1.4 s elapsed
```

- Whole row is the hit target (no nested `<button>`); element is
  `role="button"`, `tabindex=0`, `aria-label="Copy <label> address
  <address>"`.
- `Copied` state: four-layer event over ~700 ms — see §Q9. Breath
  (border + outer glow in `--state-run`), icon swap `copy → check`,
  address span highlight, and a GSAP `back.out` row micro-bounce
  `1 → 1.018 → 1` over ~140 ms.
- Clipboard failure: no toast, no banner; icon flashes back to copy
  and the address stays selectable for manual copy. The browser
  already surfaces permission failures; we do not duplicate.

### HIG fit

- Hit target ≥ 44 px (HIG iOS touch guidance; macOS row interactions
  use the same generous shape).
- Single primary action per row (copy); no secondary affordance
  competes — matches "Make sure controls have clear, single purposes."
- Visual feedback on every interaction (hover lift, active glow,
  Copied state) — HIG Tips, Feedback.
- Identity (label) and value (address) split into roles via opacity
  hierarchy — HIG Typography, "establish a clear hierarchy."
- No filename / path / UUID anywhere — addresses are user-meaningful
  identifiers, distinct from leaky storage identifiers banned by
  [[no-user-filenames]].

### Brand fit

Rows read as horizontally-laid rune-stones beneath the dial — the
three (or more) flanking stones from the brand-language composition,
re-cast as the "things the wizard hands you for the journey." Cyan
glow on the dial picks up only on row hover, never ambient — the
dial remains the visual centre.

## Trade-offs

| Choice | Gain | Cost |
|---|---|---|
| Under-block slot swapped by stage | One slot, two roles; no extra column rhythm | Slot component dispatches by stage (small switch in ritual-app or a wrapper) |
| All reachable IPs as rows | Honest about host topology; user picks per peer | Two or three rows take vertical space — acceptable, RUN under-block is otherwise empty |
| Whole row clickable | HIG-grade hit target; quicker for users | Row needs `role="button"` + keyboard handling; can't be a `<ul><li>` of plain text |
| Decoder-v2 on row reveal | Continuity with dial label settle; brand-coherent | Adds decoder mount per row; cost is one-shot per session, not per frame |
| Trailing icon (not bordered button) | Calmer; row is the button | Slightly less affordance than a bordered button — compensated by hover lift |

## Edge cases

- **Zero addresses**: AddressProvider always returns at least
  localhost (addresses.go:70). The empty-list branch from
  `stage-running.ts:43` is not reachable in practice but the
  component renders nothing if list empty (defensive, not user-facing).
- **Clipboard API unavailable / denied**: silent fall-through to
  "selectable text" — `user-select: text` on the address span so the
  user can highlight + ⌘C / Ctrl+C manually. No banner.
- **Very long address strings** (IPv6 literal in future, or port >
  65535 typo): row wraps under address column; label column stays one
  line via `white-space: nowrap`.
- **Reduced motion**: decoder-v2 already honors
  `prefers-reduced-motion`; row hover lift uses transform — also
  suppressed under reduced motion via existing token.

## Verification

1. RUN entry in real app: under-block contains `<run-addresses>` with
   ≥ 1 row; each row plays decoder reveal once, then sits idle.
2. Click any row → clipboard contains exact `address` string; row
   shows "Copied" affordance for ~1.4 s.
3. Tab through addresses; Enter / Space copies the focused row. Aria
   label announces label + address.
4. Toggle prep → run → final live: telemetry / addresses / telemetry
   swap cleanly with the existing fade.
5. Reduced-motion: rows render plain text, copy still works, no
   decoder splash.
6. Stage-running.stories.ts ALL_ADDRESSES three-row fixture renders
   identically under the dial in a new
   `dial-composition` "Run with addresses" story.

## Implementation Plan

**Scope confirmed: Storybook only.** Legacy `stage-running.ts`
continues to ship its card; the dial migration that switches the live
RUN screen will pull `<run-addresses>` in as part of its own work.

1. **New component** `frontend/src/ui/run-addresses.ts` —
   contract above; decoder-v2 on label + address (reveal + slow idle);
   copy logic ported from `stage-running.ts:12-22`; row breath
   animation in `--state-run`.
2. **Storybook stories** `run-addresses.stories.ts` — Playground
   (address count slider), Empty (defensive render), CopyFlow (manual
   trigger to show the breath + Copied affordance without needing a
   real clipboard grant).
3. **Composition story** — extend `dial-composition.stories.ts`
   `Cycle` so RUN renders `<run-addresses>` in the under-block slot,
   and add `Playground` arg or new story `RunWithAddresses` for the
   static composition.
4. **No app wiring in this log.** `ritual-app.ts` / `stage-running.ts`
   stay on the card layout. The follow-up dial migration mounts the
   under-block slot as: `<dial-telemetry>` for prep / final,
   `<run-addresses>` for run.

## Out of scope

- Replacing `stage-*` components in `ritual-app.ts` with the dial
  composition — that's the broader dial migration.
- IPv6 / link-local UX — AddressProvider already filters them out;
  revisit if/when scope expands.
- QR code for join address — possible future; not required for the
  laconic "what do I share?" answer.
- Auto-detect best primary interface and pin it to the top with a
  star / highlight — speculative; AddressProvider order is enough.

## Implementation Results — 2026-05-21

Status: **Implemented in Storybook.** App wiring still deferred to the
broader dial migration (per Implementation Plan §4).

### Files

| File | Role |
|---|---|
| `frontend/src/ui/run-addresses.ts` | new component |
| `frontend/src/ui/run-addresses.stories.ts` | Playground / Empty / SingleLocalhost / FullList |
| `frontend/src/ui/dial-composition.stories.ts` | `Cycle` swaps under-block to `<run-addresses>` on RUN; added static `RunWithAddresses` story; renamed `.telemetry-slot` → `.under-slot` |

### Deviations from prior design sections

- **Trailing icon** now uses **GSAP `MorphSVGPlugin`** on a single
  `<path class="icon-path">` per row (mirroring `ritual-dial.ts`
  compoundD pattern) — not a Lit-driven SVG re-render swap. Lucide
  `Copy` and `Check` are compiled once to `D_COPY` / `D_CHECK` at
  module load; row morphs between them in 220 ms on copy / release.
  Honors `prefers-reduced-motion` by setting `d` directly.
- **Morph-back timing** lock-stepped to the breath: morph-back fires
  at `BREATH_MS - ICON_MORPH_MS` so the icon completes its
  `check → copy` morph at the same instant the breath ends. Earlier
  draft had morph-back run at the breath's end, extending visible
  motion past the highlight window.
- **Breath duration** widened from 700 ms → **1000 ms** for a more
  legible "Copied" highlight. `BREATH_MS = 1000`,
  `MORPH_BACK_DELAY_MS = 1000 - 220 = 780 ms`. Whole copied event
  remains tightly framed inside one window; bounce + click→breath
  start unchanged.

### Iteration 2026-05-21 — heading dropped, uptime adopted, rows go bare

Live screenshot review flagged three issues with the first cut:

1. **Redundant status copy.** Dial sub already conveys "Ready to play";
   a header "Server is running" repeated the idea. Resolved by
   removing the heading text entirely.
2. **Address block read as a detached panel.** Original draft had row
   backgrounds + borders + a divider line. Briefly stripped all of
   that to bare-text rows — but on review the backgrounded panel rows
   read better (more affordance that each row is its own copy
   target, more brand-coherent rune-stone feel). Reverted to
   backgrounded rows; the heading divider was the actual disconnect.
3. **Uptime was disconnected.** Now lives as a small dim
   `tabular-nums` line centered directly above the address rows
   (replacing the heading row). Acts as the address-block's only
   chrome.

Additionally:

- The component **owns its own uptime ticker.** Sole contract is a
  `start-offset` attribute (default `0`) — component captures
  `performance.now()` on `connectedCallback`, runs
  `setInterval(1000)`, renders `start-offset + elapsed` via
  `formatEta`. No external `uptimeSeconds` / `startedAtMs` prop
  — earlier two-prop attempt was undone once it was clear there
  is no backend signal feeding this view; pure UI ticker is the
  right shape until a real `serverStartedAt` lands in the ViewModel.
- Uptime caption typography upsized (`11px → 15px`,
  `line-height: 20px`, opacity bumped to ~55%) so it reads as a
  small but legible counter, not a metadata footnote.
- "Hold to stop" **stays on dial sub** (rejected merging uptime into
  the dial sub) — the gesture cue is the discoverability path for the
  hold interaction; removing it would silently break that.
- Copied state keeps the `--state-run` breath glow (box-shadow) +
  icon morph + address highlight; the row background tint that was
  part of the panel look is gone with the panel.
- **Row spacing tightened** from initial HIG-iOS 44 px floor to a
  desktop-optimised 28 px minimum (5 px × 10 px padding, 12 px
  body type, 14 px icon). Mouse-first surface; the 44 px floor was a
  touch heuristic. Rows still respect Fitts' law via full-width
  click area + keyboard activation. Container max-width 380 px.
- **No address-highlight text-shadow** intensity beyond a quick tint
  flash; full opacity already differentiates the copied row.
- **Address selectability** under failure: dropped from CSS scope —
  the whole row is `user-select: none` to keep keyboard activation
  clean. Clipboard failure leaves the row idle without affordance;
  if this becomes a real friction we revisit.

### Verification

1. `npx tsc --noEmit` clean.
2. `npm run build-storybook` clean (no warnings beyond Storybook's
   own DocsRenderer chunk-size note).
3. Stories present: `Run / Addresses` (Playground, Empty,
   SingleLocalhost, FullList) and `Dial / Composition`
   (RunWithAddresses + Cycle now drops `<run-addresses>` into RUN).
4. Live in Storybook: clicking a row → icon morphs `copy → check`
   via MorphSVG, row plays `--state-run` breath + GSAP back.out
   bounce, decoder-v2 idle continues throughout.
