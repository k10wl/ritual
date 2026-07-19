# 039 — Retention control plane (editable rules in the GUI)

| | |
|---|---|
| **Status** | Implemented (2026-06-04; live R2 smoke pending) |
| **Date** | 2026-06-04 |
| **Depends on** | [[033-retention-ui]] (segmented + timeline UI), [[038-restore-previous-version]] (`ListVersions` feeds the real timeline), backend spec `docs/superpowers/specs/2026-04-14-backup-retention-design.md` |
| **Scope** | Make retention rules **read fresh from `settings.json` at prune time**, expose `Get/SetRetentionRules` control API, build the [[033]] UI deliverables (`rune-segmented`, `retention-model`, `retention-rules`), and wire a Retention section into Advanced. World-save retention only. |

## Background

The retention engine is **complete and already running live**: `core/retention`
(tiered union `markKeys`), `stages/retaining` (prune + GC), and
`subsystems/retention.Build()` wire local + remote prune Jobs into every
save-path flow (Session/Upload after Committing→Push; Download after Pulling).
Rules live in `domain.Settings.{LocalRetention,RemoteRetention}` (each four ints,
default `KeepLast:2`).

Two gaps stop it being a feature:

1. **Stale at runtime.** `subsystems/retention.Build()` calls `domain.LoadSettings()`
   **once at composition (startup)** and captures the rules **by value** into the
   `refsRetention` structs that go into `pipeline.Deps`. Any edit to the rules would
   not take effect until the app restarts.
2. **No control surface.** Nothing in `ControlService` gets or sets the rules; the
   only path is hand-editing `settings.json`. The [[033]] UI (`rune-segmented`,
   `retention-rules`, `retention-model.ts`) was never built.

## Problem

Make retention **controllable from the app**: the user picks how many backups
survive per tier (local + remote), sees what that keeps vs prunes against their real
history, and the next sync honors it — **without a restart**.

## Questions and Answers

**Q1. How do edits take effect without a restart — and without closures?**
A: **Read `settings.json` at prune time, not at startup.** (User directive,
2026-06-04: *"we don't want closures, we want to read settings when we need them."*)
`refsRetention.Select()` is already the IO boundary (it does `storage.List`); it
loads settings there and applies the current rules. No `func() Rules` closure, no
startup-captured value, no in-memory cache to invalidate. The pure `markKeys` stays
pure; only the impure `Select` adapter gains the read. Pruning runs at most once per
flow, so a fresh `os.ReadFile` per `Select` is negligible.

**Q2. How does `Select` know which side's rules to read?**
A: A scope discriminator chosen at construction (a plain value, not a closure):
```go
// internal/core/retention
type Scope int
const ( ScopeLocal Scope = iota; ScopeRemote )

func NewRefsRetention(storage ports.StorageRepository, scope Scope) Retention
```
`Select` reads `settings.LocalRetention` or `settings.RemoteRetention` by `scope`,
falling back to `DefaultRetentionRules()` on a zero value (preserving today's
behavior). `subsystems/retention.Build()` then drops its `LoadSettings` call and just
wires `NewRefsRetention(localStorage, ScopeLocal)` / `(remoteStorage, ScopeRemote)`.

**Q3. Logs retention reads from settings too?**
A: **No.** The logs tier is `KeepLast: config.MaxLogFiles` — not user-facing, not in
`Settings`. `NewLogsRetention(storage, rules domain.RetentionRules)` keeps its fixed
by-value rule. Only the two refs-retentions become scope-driven. (Self-documenting:
only what the user can edit reads the editable file.)

**Q4. Validation on Set?**
A: `SetRetentionRules` validates each tier `0 ≤ n ≤ 5` (the [[033]] segmented range)
and persists via `settings.Save()`. `KeepLast: 0` is **allowed** (the spec's
documented edge case — "delete everything next prune"); the UI shows the caution
copy ([[033]] §caution), the backend does not block it. Negative or >5 is rejected
(out of the control's range; only a hand-edited file could produce it).

**Q5. Where does the timeline's backup history come from once wired?**
A: [[033]] §Q3 specified synthetic data for Storybook and a real `backups` property
when wired. [[038]]'s `ListVersions` is that source: the **R2** rules preview uses
remote versions, the **Local** preview uses local versions (both via
`ListVersions`'s `Source`-tagged result, or a `scope` arg added there). If [[038]]
isn't wired yet, `retention-rules` falls back to its synthetic `sample()` so the
component degrades gracefully (and the picture is honest about being synthetic).

**Q6. One panel for both scopes, or two?**
A: **One `retention-rules` instance with a scope switch** — a `rune-segmented`
(`Local · R2`) at the top selects which rule set is being edited; one set of four
tier controls + one timeline below. Two stacked full panels (4 controls + timeline
each) overflow the 560×720 window ([[023]]/[[029]]). The component already takes a
`scope` label property ([[033]] §Q7); the host swaps `rules` + `backups` per
selected scope and persists the edited side.

**Q7. [[033]] open Q6 — preset row (Paranoid/Economical/…)?**
A: **Deferred — not in v1.** Adds a row of buttons + a localization decision (spec
lists RU labels) for a teaching aid the four labelled segments already mostly convey.
Re-open once the core picker ships. Recorded here so [[033]] Q6 is resolved (= no)
rather than dangling.

**Q8. When does `SetRetentionRules` fire — per segment tap, or on a Save?**
A: **Per `change`**, debounced in the host (one `setRetentionRules` call after edits
settle). No explicit Save button — HIG-direct manipulation; the preview already shows
the consequence live, and the next prune reads the file. Persisting eagerly also
means a crash before the next prune doesn't lose the setting.

## Design

### Backend

**1. Scope-driven, settings-at-prune-time retention** (`internal/core/retention/retention_refs.go`)
```go
type refsRetention struct {
    storage ports.StorageRepository
    scope   Scope
}
func NewRefsRetention(storage ports.StorageRepository, scope Scope) Retention {
    return &refsRetention{storage: storage, scope: scope}
}
func (r *refsRetention) Select(ctx context.Context) ([]string, error) {
    settings, err := domain.LoadSettings()      // ← read when we need them
    if err != nil { return nil, fmt.Errorf("retention load settings: %w", err) }
    rules := settings.LocalRetention
    if r.scope == ScopeRemote { rules = settings.RemoteRetention }
    if rules == (domain.RetentionRules{}) { rules = domain.DefaultRetentionRules() }
    keys, err := r.storage.List(ctx, refsPrefix)
    if err != nil { return nil, err }
    return markKeys(keys, rules, r.parseTime), nil
}
```

**2. Composition simplification** (`internal/subsystems/retention/retention.go`)
`Build()` loses its `domain.LoadSettings()` call and the local/remote rule plumbing;
it wires `NewRefsRetention(localStorage, coreret.ScopeLocal)` and
`(remoteStorage, coreret.ScopeRemote)`. The `observed.NewRetention` decorator and
GC/logs jobs are unchanged. (`Build` no longer returns a settings error from this
path — signature may drop `err` if nothing else fails; confirm at impl.)

**3. Control API** (`internal/gui/control/control.go` + `retention.go`)
```go
type RetentionConfig struct {
    Local  domain.RetentionRules `json:"local"`
    Remote domain.RetentionRules `json:"remote"`
}
func (c *ControlService) GetRetentionRules() RetentionConfig // from LoadSettings, defaults on miss
func (c *ControlService) SetRetentionRules(local, remote domain.RetentionRules) error
```
`Set` validates each field `0..5`, loads settings, assigns both sides, `Save()`s.
No bus event needed — the next prune `Select` reads the file. (`domain.RetentionRules`
already carries `json:"keep_last"` etc.; Wails generates the TS model.)

### Frontend ([[033]] deliverables, then wiring)

Build [[033]] as drafted, then add the scope switch + wiring:

- **`rune-segmented`** primitive — `role=radiogroup`, roving tabindex, `0..5` and the
  `Local·R2` switch both use it ([[033]] §Q1/§rune-segmented). Two callers ⇒ passes
  the audit gate.
- **`retention-model.ts`** — pure TS port of `markKeys` (union, newest→oldest, UTC
  buckets) + `sample()` + `summarize()` ([[033]] §pure-model). Test cases copied from
  the backend spec's `Mark` table (parity gate).
- **`retention-rules`** component — scope switch + 4 tier controls + summary + legend
  + timeline + keep_last:0 caution. `rules`, `backups`, `now`, `scope` properties;
  emits `change { scope, rules }`.
- **Wiring** — a "Retention" section in `advanced-view`; host (`ritual-app`) feeds
  `getRetentionRules()` + per-scope `listVersions()` backups, handles `change` →
  debounced `setRetentionRules(local, remote)`.
- `wails-api.ts` gains `getRetentionRules()` / `setRetentionRules(local, remote)` +
  the `RetentionConfig` / `RetentionRules` type re-exports.

## Implementation Plan

**Phase A — backend rules-at-prune-time.** `Scope` + scope-driven `NewRefsRetention`;
`Select` reads settings. Update `subsystems/retention.Build()`. Tests: editing the
on-disk settings between two `Select` calls changes the selection (proves no
capture); local vs remote scope reads the right field; zero-value → defaults.

**Phase B — control API.** `RetentionConfig`, `GetRetentionRules`,
`SetRetentionRules` (+ 0..5 validation, keep_last:0 allowed). Tests stub settings IO.

**Phase C — `rune-segmented` primitive.** Per [[033]] Phase B: `.ts`/`.stories`/
`.test` + re-export. Keyboard + ARIA + roving tabindex.

**Phase D — `retention-model.ts`.** Per [[033]] Phase A: port + spec-table tests
(parity is the gate).

**Phase E — `retention-rules` component.** Per [[033]] Phase C + the §Q6 scope
switch. Story knobs: default / paranoid / minimalist / keep_last:0 / custom backups /
Local-vs-R2.

**Phase F — wire into Advanced.** Section in `advanced-view`; host wiring to
`get/setRetentionRules` + `listVersions`; debounce. `wails-api` wrappers.

**Phase G — verify.** Go `go test ./...`. Frontend `skill: verify` (Storybook +
`npm run test` + Wails build). Manual: set `keep_last:1, keep_daily:0…`, run a sync,
confirm older local refs prune to the new policy **without a restart**.

## Examples

✅ Edit `keep_last 2→1` in Advanced → next Download/Session prune keeps 1 (no
restart) because `Select` re-reads `settings.json`.
✅ `keep_last:0` → segmented allows it, caution copy shows, backend prunes per spec.
✅ Timeline survivors tagged by protecting tier hue ([[033]] §Q5) over **real**
versions from [[038]] `ListVersions`.
❌ No `func() Rules` closure and no startup-captured rules value — settings are read
at the point of pruning (Q1 directive).
❌ Logs tier is **not** exposed (fixed `config.MaxLogFiles`).

## Trade-offs

- **`core/retention` now imports `domain.LoadSettings` (filesystem read).** Accepted:
  `Select` is already the impure adapter (it lists storage); reading the settings file
  beside it keeps `markKeys` pure and removes the startup-capture staleness. The
  alternative (closure injection / per-run rebuild) was explicitly rejected.
- **A read per prune.** One `os.ReadFile` per `Select`, at most twice per flow.
  Negligible; always current.
- **Duplicated `Mark` logic (Go + TS).** Same trade-off [[033]] §Trade-offs already
  accepted (live preview needs client computation; spec table keeps them in parity).
- **One panel + scope switch vs two panels.** Fits the fixed window; costs a tap to
  switch sides. Accepted ([[029]] budget).

## Verification Criteria

1. Mutating `settings.json` (or calling `SetRetentionRules`) between two `Select`
   calls changes the returned keys — proves rules are read at prune time, not captured
   (unit test).
2. `ScopeLocal`/`ScopeRemote` read the correct settings field; zero-value → defaults.
3. `SetRetentionRules` persists; `GetRetentionRules` round-trips; `0..5` enforced,
   `keep_last:0` accepted.
4. A live edit changes what the **next** sync prunes with **no app restart**
   (integration/manual).
5. `retention-model.mark()` matches the backend spec `Mark` table ([[033]] §VC2).
6. `rune-segmented` keyboard + ARIA correct; `retention-rules` emits
   `change { scope, rules }`; keep_last:0 caution shows; Storybook + `npm run test`
   green; Wails build passes.

## Open Questions

- **OQ1 — RESOLVED 2026-06-04 (per-scope):** `ListVersions` takes a scope arg
  (`"local"`/`"remote"`) so the Local preview shows local refs and the R2 preview
  shows remote refs — each timeline is honest about the store it governs. [[038]]'s
  `ListVersions` signature carries the scope.
- **OQ2** — Surface a GC/prune *result* ("freed 3 versions, 412 MB") after a sync, or
  keep retention silent as today? (Lean: silent v1; the `RetentionSelectInfo` event
  already exists if we want it later.)

## Implementation Results (2026-06-04)

All phases A–F shipped; backend `go test ./...` + `go vet` green, frontend
`web-test-runner` 107/107 green (incl. 33 new across rune-segmented / model /
retention-rules / advanced-view), `tsc` + vite build clean. [[033]]'s
Storybook deliverables are now built **and** wired end-to-end.

**What landed, by phase:**
- **A — rules at prune time.** `retention.Scope` (`ScopeLocal`/`ScopeRemote`);
  `NewRefsRetention(storage, scope)`; `Select` calls `domain.LoadSettings()`
  fresh, picks the side, defaults on zero. `subsystems/retention.Build` dropped
  its startup `LoadSettings` + the `err` return (now 2 values). Tests prove the
  no-capture property (editing settings between two `Select`s changes the result),
  scope-picks-right-field, and zero→defaults.
- **B — control API.** `control.RetentionConfig`, `GetRetentionRules` (effective,
  defaults on miss/zero), `SetRetentionRules` (0..5 per tier, keep_last:0 allowed)
  in `control/retention.go` + tests. Bindings regenerated.
- **C — `rune-segmented`.** Primitive (radiogroup, roving tabindex, ←/→/Home/End,
  `change`) + story + 7 tests; re-exported from `primitives/index.ts`.
- **D — `retention-model.ts`.** Pure `mark`/`sample`/`summarize` + UTC bucket keys
  (incl. ISO-week) mirroring Go `markKeys`; 10 parity tests.
- **E — `retention-rules`.** Component: Local·R2 scope switch + 4 tier segmented
  controls + live summary/legend/timeline (real `mark()`) + keep_last:0 caution;
  emits `change {local, remote}`. Story (default/paranoid/minimalist/zero/custom)
  + 8 tests.
- **F — wired into Advanced.** "Retention" section in `advanced-view` (loads rules
  + per-scope backups on each open); host (`ritual-app`) injects `loadRules`/
  `loadBackups`, persists via `setRetentionRules`, with snake↔camel mapping at the
  boundary. `wails-api` `getRetentionRules`/`setRetentionRules` + type exports.

**Deviations from the design:**
- **Event-name collision fix (not in the design).** `prep-settings` and
  `retention-rules` both emit `change`; in one parent they'd collide. `advanced-view`
  catches the retention `change`, holds the rules in its own `@state`, and re-emits
  a distinct **`retentionchange`** the host persists. The picker keeps emitting the
  conventional `change` (testable in isolation).
- **§Q1 directive honoured literally.** No closures, no startup capture — the read
  is `domain.LoadSettings()` inside `refsRetention.Select`. The pure `markKeys`
  stays pure; only the impure `Select` adapter gained the read.
- **camelCase model vs snake_case binding.** `retention-model`/component use a clean
  camelCase `RetentionRules`; the Wails binding is snake_case (`keep_last`). Mapped
  at the `ritual-app` boundary (`toModelRules`/`toBindingRules`) rather than
  contaminating the model with wire names.
- **§Q7 presets** — deferred as planned (not built).

**Verification vs criteria:** VC1 (read-at-prune-time, no capture) + VC2 (scope
fields) + VC4-ish (live edit changes next prune) — `core/retention` unit tests.
VC3 (persist/round-trip, 0..5, keep_last:0) — `control` tests. VC5 (`mark()` parity)
— `retention-model` tests. VC6 (segmented a11y/keyboard, `change {scope,rules}`,
caution, builds) — `rune-segmented` + `retention-rules` + `advanced-view` tests +
FE build. **Pending:** live R2 smoke — set rules in the running app and confirm the
next real sync prunes accordingly without a restart.
