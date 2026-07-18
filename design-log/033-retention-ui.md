# 033 — Retention Rules UI (Borg-style tier picker + survival visualization)

| | |
|---|---|
| **Status** | Implemented (2026-06-04; built + wired via [[039-retention-control-plane]]; visualization redesigned to a per-tier cascade — see §Redesign) |
| **Date** | 2026-05-30 |
| **Depends on** | [[031-bidirectional-sync]] (Retain stages), backend spec `docs/superpowers/specs/2026-04-14-backup-retention-design.md` |
| **Scope** | Storybook UI only. No app wiring, no Wails bindings, no backend change. |

## Background

The backend retention engine (spec `2026-04-14-backup-retention-design.md`) is a
Borg/restic-style **tiered, union-protection** model. Four knobs, each `0–5`:

```go
type RetentionRules struct {
    KeepLast    int // N most recent backups
    KeepDaily   int // 1 per UTC calendar day, up to N days
    KeepWeekly  int // 1 per ISO week, up to N weeks
    KeepMonthly int // 1 per UTC calendar month, up to N months
}
```

A backup survives if **any** tier wants it. Tiers never conflict — more tiers =
more protection. Local rules live per-host in `settings.json`; R2 rules live in
`manifest.json` (admin). Default `KeepLast: 2`, rest zero.

The spec's only UI note is one line:

> **UI mapping:** 4 sliders (0-5 each) + computed "total protected" preview + timeline visualization. Labels self-document.

## Problem

The numbers are easy to render; the **consequences are not legible**. Union of
four tiers over an irregular backup history is genuinely hard to reason about:

- "I set keep_weekly:2 — which of my 40 backups actually survive, and which die?"
- keep_last and keep_daily overlap on recent days, then separate as time passes.
- All-zero (or keep_last:0) silently deletes everything next session.

A bare set of sliders ships the knobs but **not the understanding**. The UI must
answer, at a glance: *given my current backups, what is kept, what is pruned, and
why.*

## Questions and Answers

**Q1. Slider, stepper, or segmented for the 0–5 pick?**
A: **Segmented** (`0·1·2·3·4·5`, one tap). Decided with user 2026-05-30.
Deviation from spec's "sliders" — HIG steers to a stepper/segmented for tiny
discrete integer ranges; a 6-stop slider is imprecise and adds motion for no
gain. Segmented shows the whole range at once and reads as a self-documenting
scale. Spec text is advisory on UI; backend unaffected.

**Q2. What visualization makes kept-vs-pruned legible?**
A: A **retention timeline**, not a calendar grid. Decided with user 2026-05-30.
Rationale: weekly/monthly tiers *spread across months* — exactly the spec's own
ASCII diagram (§Tier Algorithm). A month-grid hides that spread; a single time
axis shows it. Each past backup is a marker positioned by date; survivors are
solid and color-tagged by the **protecting tier**, pruned ones dim to a hollow
tick. (Calendar grid considered and rejected: can't show >1-month weekly/monthly
reach without scrolling, and clusters recent backups into one cell.)

**Q3. Where does the preview's backup history come from?**
A: For Storybook, a **deterministic synthetic history** (~30 backups over ~95
days, intra-day multiples on recent days to exercise keep_last). The preview runs
a **faithful TS port of the backend `Mark` union algorithm** against it — the
picture is computed, never faked. When wired to the app later, the same component
takes a real `backups` list as a property (out of scope here).

**Q4. Primitive or component?**
A: One new **primitive** `rune-segmented` (generic mutually-exclusive small set —
≥2 callers: the 4 tiers satisfy the audit gate). The retention panel itself is a
**component** `retention-rules` (composes 4× `rune-segmented` + the timeline).
The timeline is retention-specific → stays inside the component, not promoted to a
primitive.

**Q5. Tier → color?** A: Reuse the state palette for 4 distinct hues:
`last → --state-run` (green), `daily → --state-prep` (amber),
`weekly → --state-idle` (blue), `monthly → --state-final` (purple); pruned →
`--text-faint` hollow. A backup protected by several tiers takes its
highest-priority hue (last > daily > weekly > monthly).

**Q6. Show the spec's named presets (Paranoid/Economical/Minimalist/Archivist)?**
A: **Open.** They teach the model fast ("Archivist = keep_last:1, keep_monthly:4")
and map 1:1 to spec examples. Proposed: a compact preset row of `rune-button
plain` that sets all four knobs. Localized labels? Spec lists them in Russian
(Параноик/Экономный/Минималіст/Архіваріус). **Need answer:** include presets? EN
or RU labels?

**Q7. Surface local + R2 as two instances or one?** A: Out of scope. This log
delivers a single reusable `retention-rules`. The screen that shows it twice
(per-host local + admin R2) is a later wiring task. Component stays scope-agnostic
(a `scope` label property is the only hook needed).

**Q8. `now` and render purity.** A: `now` is a **property** (default a fixed
`2026-05-30T12:00:00Z` for deterministic stories/tests), never `new Date()` in
`render()` — honors [[020-lit-render-purity]]. App passes live now later.

## Design

### Component tree

```
retention-rules (Components / Retention Rules)
├─ header        — title + one-line explainer + optional preset row (Q6)
├─ tiers
│   ├─ tier-row  "Keep last"    <rune-segmented 0..5>
│   ├─ tier-row  "Keep daily"   <rune-segmented 0..5>
│   ├─ tier-row  "Keep weekly"  <rune-segmented 0..5>
│   └─ tier-row  "Keep monthly" <rune-segmented 0..5>
└─ preview
    ├─ summary   "Keeping 9 of 31 backups · 78 days of history"
    ├─ legend    color swatch + tier name + survivor count, + pruned
    ├─ timeline  month-ruled axis; markers = backups (kept solid / pruned hollow)
    └─ caution   only when keep_last = 0 (see spec edge case)
```

### `rune-segmented` (primitive)

HIG: segmented control = mutually-exclusive set. https://developer.apple.com/design/human-interface-guidelines/segmented-controls

```ts
type SegmentOption = { value: string; label: string };

@property({ attribute: false }) options: SegmentOption[] = [];
@property() value = "";                     // selected option value
@property({ type: Boolean, reflect: true }) disabled = false;
@property() label: string | null = null;    // a11y group label
// emits: change { value: string }  (composed, bubbles)
```

- `role="radiogroup"`; each segment `role="radio"` + `aria-checked`.
- **Roving tabindex**: only the selected segment is tab-stop; `←/→`/`Home`/`End`
  move + select (HIG/ARIA radiogroup behavior).
- Pure presentation: no domain knowledge, no number assumptions (component maps
  `number ↔ string`). Override surface via `--rune-segmented-*` + `::part()`.
- Files: `rune-segmented.ts` / `.stories.ts` / `.test.ts`; re-export from
  `primitives/index.ts`.

### `retention-rules` (component)

```ts
type RetentionRules = { keepLast: number; keepDaily: number; keepWeekly: number; keepMonthly: number };

@property({ attribute: false }) rules: RetentionRules = { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 };
@property({ attribute: false }) backups: Backup[] | null = null;  // null → synthetic sample
@property({ attribute: false }) now: Date = FIXED_NOW;            // render-pure (Q8)
@property() scope = "";                                            // "Local" | "R2" caption (Q7)
// emits: change { rules }  on any tier edit
```

Render derives `marked = mark(backups ?? sample(now), rules)` — a pure function of
props/state (Lit-pure). No internal mutation outside the `change` handler.

### Pure model — `src/ui/retention-model.ts`

TS port of backend `Mark` (union walk, newest→oldest), plus UTC bucket keys and a
deterministic sample generator. **No IO. Unit-testable without the DOM.**

```ts
type Tier = "last" | "daily" | "weekly" | "monthly";
type Backup = { id: string; date: Date };
type Marked = Backup & { tiers: Tier[]; kept: boolean };

function mark(backups: Backup[], rules: RetentionRules): Marked[];   // mirrors Go Mark()
function sample(now: Date): Backup[];                                // deterministic ~30 over ~95d
function summarize(m: Marked[]): { kept: number; total: number; oldestSurvivor: Date | null; spanDays: number };
// bucket keys (UTC): dayKey → "YYYY-MM-DD", isoWeekKey → "YYYY-Www", monthKey → "YYYY-MM"
```

`mark` parity with Go: sort newest-first; per backup, a tier protects iff
`tierCount < ruleN` **and** (for daily/weekly/monthly) the UTC bucket is unseen;
push protecting tiers; `kept = tiers.length > 0`. Same union semantics, same
budget-per-tier, same UTC boundaries (`Intl`/`Date.UTC`, no custom calendar).

## Implementation Plan

**Phase A — model.** `retention-model.ts` + `retention-model.test.ts`. Port the
spec's `Mark` table cases (empty / all-protected / all-zero / single-tier /
overlap / boundaries / default). Parity is the gate; everything else renders this.

**Phase B — primitive.** `rune-segmented` + story (variants/states/keyboard) +
test (attr→render, click/key→`change`, roving tabindex, a11y roles). Re-export.

**Phase C — component.** `retention-rules` + story. Wire 4 segmented controls →
`rules` → `change`. Build summary + legend + timeline + caution from `marked`.
Story knobs: default, paranoid, minimalist, keep_last:0 (caution), custom
`backups`.

**Phase D — verify.** `skill: verify` (Storybook renders, `npm run test` green).
No Wails build needed — UI-only, no bindings touched.

## Examples

Summary + legend (rules `last:2 daily:3 weekly:2 monthly:2`, sample history):

```
Keeping 9 of 31 backups · 78 days of history (oldest survivor 13 Mar)
 ● last 2   ● daily 3   ● weekly 2   ● monthly 2   ○ pruned 22
```

Timeline (schematic — ● solid survivor by tier hue, · dim pruned tick):

```
 Mar            Apr                     May
 │              │                       │
 ●      ●       ●        ●   ·· ··  ·●·● ·●●●
 month  month   weekly   weekly      daily/last cluster (overlap, newest = last)
```

✅ Survivors are tagged by **why** they live (tier hue), so the union is visible.
✅ keep_last:0 → caution: *"Without 'keep last', the newest backup can be pruned
after the next session."* (spec edge case).
❌ Don't render a faked count — the preview must run the real `mark`, or it lies.
❌ Don't read `new Date()` in render — `now` is a property (Q8 / [[020]]).

## Trade-offs

- **Duplicated `Mark` logic (Go + TS).** Accepted: a live preview needs client-side
  computation; a round-trip per keystroke is worse UX. Risk = drift. Mitigation:
  TS port mirrors the spec table 1:1 and its test cases are copied from the spec,
  so divergence breaks a test. Single source of *truth* stays the spec.
- **Synthetic history.** The Storybook picture isn't the user's real backups.
  Accepted for the design-system deliverable; real `backups` is a property the app
  feeds later (Q3/Q7).
- **Segmented vs spec's "slider."** Documented deviation (Q1). Backend untouched.
- **Timeline marker density.** ~30 markers across one axis is fine; a host with
  hundreds of backups needs clustering/zoom — deferred until a real data volume
  justifies it (noted, not silently capped).

## Verification Criteria

1. Editing any segment updates the preview **and** emits `change { rules }`.
2. `mark()` matches the backend spec's `Mark` table (all listed cases) — unit test.
3. Every survivor in the timeline is reachable by at least one enabled tier;
   every pruned marker by none (asserted against `mark` output).
4. `keep_last:0` (and all-zero) shows the caution copy.
5. `rune-segmented` is keyboard-operable (←/→/Home/End, roving tabindex) with
   correct radiogroup ARIA.
6. No wall-clock read in `render()` (grep `new Date(` in render paths = none).
7. Storybook renders all stories; `npm run test` green; no app/backend files
   touched.

## Open Questions (need answers before Phase C)

- **Q6** — include preset row? EN or RU labels?
- **Q2-follow** — month ruler only, or add faint week ticks? (lean: month labels +
  unlabeled week gridlines, kept subtle.)
- Anything else on the visualization you pictured ("calendar or other") that the
  timeline doesn't cover?

## Redesign — explanatory calendar cascade, no dry-run (2026-06-04)

This section records several rounds of user feedback that **supersede §Q1, §Q2,
§Q5, and §Q8's preview semantics**. The original single-axis timeline read as
**cramped**, and — more fundamentally — framing the preview as a *dry-run over the
user's real backups* (with a "Deleted" lane) was wrong: it implied the setting
would **destroy the user's local backups**. Iterations:

1. **Not a dry-run; explain the policy.** The preview must *teach what the policy
   keeps*, not show "we will delete these N of your backups." So: **no Deleted
   lane**, **no real-backup feed** (drop the §Q5/OQ1 `ListVersions` history — the
   policy is universal), and **no "X of Y deleted" count**. The visualization is an
   **illustration** over a representative, synthetic-but-honest history.
2. **The calendar cascade IS the control (collapse the duplicate).** The first
   collapsed attempt put dots *inline on stepper rows* and dropped the positioned
   time axis — the user wanted the opposite: **keep the calendar-like cascade**
   (lanes with dots positioned by date on a shared recent→older axis with **month
   labels**) and **remove the separate stepper block**, moving the count control
   **onto each lane**. Final layout per lane: `label · stepper · time-track`.
3. **`rune-segmented` (0–5) → `rune-stepper` (− N +), uncapped.** New primitive
   `rune-stepper`; the tier count has **no max** (Infinity); the segmented control
   is kept only for the Local·R2 **scope switch** (a true either/or). Reverses §Q1
   for the tier knobs.
4. **Calendar dates.** Each lane's dots sit at representative calendar dates
   stepping back from `now` by the tier's cadence (daily/weekly/monthly; last
   shares the daily cadence); the **axis carries month labels** as the calendar
   reference. Per-dot ISO date in `title`.
5. **Less prose.** The plain-English sentence is dropped from the visual and kept
   only as the cascade's **a11y label** (`describePolicy`, exported + tested) so
   screen readers still get the full meaning. keep_last:0 caution stays.

**Implementation:** `retention-rules` renders a scope `rune-segmented` + a
`.cascade` of an `.axis` (month labels) and four `.lane`s (`96px | 84px | 1fr`
grid: label, `rune-stepper`, positioned-dot track); `pos = (now − date)/span`,
recent on the left. Dots cap at 8 with a `+N` overflow (count uncapped). The
`retention-model` `mark()/sample()` port is no longer rendered (the preview is
illustrative, not a real union) but is retained as the tested canonical mirror of
the Go engine. New primitive `rune-stepper` (.ts/.stories/.test). Supersedes the
single-axis timeline, the legend, the Deleted lane, and the real-backup feed.

## Re-revision — honest union timeline, no per-tier lanes (2026-06-04)

The per-tier cascade (four lanes of cadence dots) was **misleading**: it drew each
tier's reach independently, so a viewer naturally **sums** the rows. Real
Borg/restic retention keeps a **union** — one backup can satisfy several tiers at
once (today's backup is simultaneously `last`, newest `daily`, `weekly`, and
`monthly`), so the distinct kept count is far below the sum, and weekly/monthly
often add nothing over daily in the recent window. The cascade hid that.

**Decision (supersedes the §Redesign cascade visual):** render the **true union**,
honest-by-construction, using the already-present parity-tested port — no Wails
round-trip (§Q3 stands). The view computes `mark(sample(now), rules)` and draws
**one timeline** of the representative history; each survivor is colored by its
**strongest** protecting tier (`last > daily > weekly > monthly`, `tiers[0]`),
pruned backups render hollow/dim. Overlap is now visible by construction.

Kept from the redesign: the Local·R2 scope `rune-segmented`, the uncapped
`rune-stepper` per tier (now in a compact knob block, each row carrying a tier-hue
**swatch** that doubles as the timeline legend), `describePolicy` as the a11y
label, the `keep_last:0` caution, and `now`-as-property render purity ([[020]]).

Reverted: the four positioned-dot lanes, the per-lane `+N` cap, the calendar
gridlines move to span the single timeline. Still **not** a dry-run over the
user's real backups (the §Redesign objection holds) — the history is explicitly
*representative*; legend says so. Real-backup feed remains deferred (would need a
`ListVersions(scope)` binding and reframing; rejected here).

