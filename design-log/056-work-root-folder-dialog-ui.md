# 056 — Work root: native folder dialog + Advanced UI (Phase F of #055)

**Status:** Implemented
**Date:** 2026-08-11

## Background

[[055-operational-functional-root-split]] shipped the whole content-root
relocation mechanism (`ControlService.GetWorkRoot`/`ChangeWorkRoot(path
string)`/`ResetWorkRoot`, the swappable-storage swap, ACID crash safety) but
explicitly deferred its own Phase F: "native folder dialog + Advanced-settings
UI section, wiring the Phase E API" (055 §Q5, "API now, UI later — per user
direction"). No frontend surface exists today — the only caller of
`ChangeWorkRoot` is Go tests and `internal/integration`.

## Problem

User asked for native folder selection for the workroot in the Settings UI.
This is Phase F itself: reachable only via direct backend calls today, no way
for a user to change their workroot without editing `settings.json` by hand.

## Verified externals (this session)

**Wails v3 alpha.77 dialog API**, `pkg/application`:
```go
app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
    CanChooseDirectories: true,
    CanChooseFiles:       false,
    CanCreateDirectories: true,
    Title:                "Choose a folder",
    Directory:            config.WorkRoot, // start at current
}).PromptForSingleSelection() // (string, error)
```
Read `dialogs.go` + `dialogs_darwin.go` (macOS is the dev platform) directly:
on **Cancel**, macOS's `NSOpenPanel` completion handler skips
`openFileDialogCallback` entirely and calls only
`openFileDialogCallbackEnd`, which `close()`s the response channel without
ever sending — so `PromptForSingleSelection()` returns `("", nil)` on cancel,
**not an error**. Callers must treat empty-path-and-nil-error as "user
cancelled," distinct from a real dialog failure (non-nil `err`).

`application.Get()` returns the process-wide `*App` singleton (set by
`application.New()` in `cmd/gui/main.go`), which carries the same `Dialog
*DialogManager` field — no window handle strictly required
(`OpenFileDialogOptions.Window` is optional; unset shows an unattached panel).

## Design constraints from the existing codebase

- **`internal/gui/control` never imports `wails/v3/pkg/application`** (0 hits
  today) — it stays a plain Go driving adapter, testable without a real Wails
  app. `cmd/gui/main.go` is the only place that constructs `application.App`
  and wires Wails-specific callbacks into `ControlService` via setters
  (`wailsViewEmitter.bind`, `SetLocalStatsFn`, `SetConsoleReader`,
  `SetWorkRoot` itself). The dialog must be wired the same way: a plain Go
  function type injected via a new setter, not a direct `application` import
  in `control.go`.
- **house UX rule (sync-view.ts, versions-view.ts doc comments): no modal
  confirmation dialogs for destructive actions** — "inline two-step reveal…
  no dialog, no popup, no toast." `ChangeWorkRoot`/`ResetWorkRoot` are
  data-moving and slower than the existing Restore/Delete/Revert actions they
  sit beside, but the confirm affordance should follow the same inline
  pattern, not a native `<dialog>`.
- **Advanced pane composition** (`advanced-view.ts`): flat `<section>`s,
  host injects async thunks as `@property`, child re-emits typed
  `CustomEvent`s the host (`ritual-app.ts`) wires to `wails-api` calls. New
  `<work-root>` section follows the same shape as `<retention-rules>`
  (staged value + explicit action, not autosave) and `<versions-view>`
  (inline confirm reveal for a consequential action).

## Questions and Answers

**Q1. Does the picker call belong on `ControlService`, or should the
frontend call Wails' own generated dialog binding directly?**
**A: `ControlService` gains a new method, `PickWorkRootFolder() (path
string, ok bool)`.** Keeps the frontend on the single `wails-api.ts` surface
(house convention — every other backend call goes through `Control.*`), and
keeps validation/business rules server-side. `ok=false` means cancelled (no
error to surface); a real OS-level dialog failure is logged server-side and
also returns `ok=false` (dialog failures are not actionable by the user —
same treatment as `revealFolder`'s ignored `cmd.Start()` errors elsewhere in
this file). Wired via a new setter `SetDirectoryPicker(fn func(dir string)
(string, error))`, following the `SetConsoleReader`/`SetLocalStatsFn`
convention — `cmd/gui/main.go` supplies the real closure:
```go
controlSvc.SetDirectoryPicker(func(dir string) (string, error) {
    return wailsApp.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
        CanChooseDirectories: true,
        CanChooseFiles:       false,
        CanCreateDirectories: true,
        Title:                "Choose workroot folder",
        Directory:            dir,
    }).PromptForSingleSelection()
})
```
A nil picker (unset, e.g. in tests) makes `PickWorkRootFolder` return
`("", false)` immediately — same "degrade explicitly" convention the struct
doc comment already states for `sync`/`dirty`/`versions`.

**Q2. One combined "pick + change" call, or two round-trips (pick, then a
separate change with the chosen path)?**
**A: Two separate calls**, mirroring `ChangeWorkRoot(path string)`'s own
existing signature (055 §Q5: "takes an explicit path argument so it's
callable/testable without any dialog"). Frontend flow: `PickWorkRootFolder()`
→ if cancelled, stop; else show the chosen path + Change/Cancel inline
confirm → on confirm, `ChangeWorkRoot(path)`. Keeps the two concerns
(picking vs. committing) independently testable, and lets the UI show the
chosen path before committing to the move.

**Q3. What does the UI show while `ChangeWorkRoot` runs?**
**A: The existing relocate telemetry already on the dial.** [[055]]'s
addendum wired `StageRelocating`/`PhaseRelocating` into `projection.ViewModel`
— the main dial already renders "Moving files" + `N of M files` for any
in-flight relocate, sourced from `onView`. The new Advanced section itself
only needs a **pending/error** local state around the `ChangeWorkRoot` call
(disable the button, show its error inline) — it does not need to duplicate
progress polling; the dial is already the progress surface, exactly as it is
for Download/Upload today.

**Q4. RUNNING-gate / relocate-in-flight — does the frontend need its own
guard, or does the backend's existing gate suffice?**
**A: Backend gate suffices — surface its error.** `ChangeWorkRoot` already
returns `ErrSessionRunning`/`ErrRelocateInProgress` (055 post-review fixes
#4/#5). The UI's only job is to disable the path click + `Change` affordance
while `!isIdle` (same `canUpdate`-style phase gate `advanced-view.ts`
already uses for "Check for update"), and render whatever error string comes
back verbatim if a race slips through.

**Q5. "Use default" (Reset) affordance in this UI pass?**
**A (revised, live design review 2026-08-11): dropped.** `ResetWorkRoot`
stays a real, tested Control-layer method (055 §Q5) but this pass ships no
UI entry point for it — a user who wants the default back can `Change` to it
like any other folder. Simplifies the section to two affordances (path,
Change) instead of three. Can be reinstated later without any backend work
if it turns out to be missed.

**Q5b. Path affordance and button label — separate "Open folder" button and
"Change folder…" label, or something tighter?**
**A (revised, live design review 2026-08-11): the displayed path itself is
the "open folder" control** (click → `OpenRootFolder`) — no separate button.
The relocate action is a single-word **`Change`** button, not "Change
folder…" — the section is already labelled "Work folder" (advanced-view.ts's
`<p class="label">`), so the button doesn't need to repeat "folder."

**Q6. Error surface for `ChangeWorkRoot` failures (validation rejects,
permission errors, corrupted-blob abort)?**
**A: Inline error text under the section**, same placement as
`sync-view.ts`'s `case "error":` branch (`Try again` button + message) —
house convention, no toast/banner component exists elsewhere in this
codebase for this class of error.

## Design

### Backend (`internal/gui/control`)

```go
// control.go — new field + setter, same convention as SetConsoleReader
directoryPicker func(dir string) (string, error) // nil ⇒ PickWorkRootFolder degrades to (‑, false)

func (c *ControlService) SetDirectoryPicker(fn func(dir string) (string, error)) {
    c.mu.Lock(); defer c.mu.Unlock()
    c.directoryPicker = fn
}

// PickWorkRootFolder opens the native OS directory picker, seeded at the
// current WorkRoot. ok=false covers both user-cancel and an unset/failing
// picker — neither is user-actionable, so callers show nothing rather than
// an error (mirrors OpenRootFolder's ignored exec error).
func (c *ControlService) PickWorkRootFolder() (path string, ok bool) {
    c.mu.Lock(); fn := c.directoryPicker; c.mu.Unlock()
    if fn == nil {
        return "", false
    }
    p, err := fn(config.WorkRoot)
    if err != nil || p == "" {
        return "", false
    }
    return p, true
}
```

`cmd/gui/main.go`: after `wailsApp := application.New(appOptions)`, call
`controlSvc.SetDirectoryPicker(...)` (the Q1 closure) alongside the existing
`SetWorkRoot`/`SetConsoleReader` wiring block.

### Frontend

**`wails-api.ts`** — three new thin exports, same shape as the existing
`getWorkFolder`-style calls (naming follows the Go method names 1:1, as
every other export in this file already does). No `resetWorkRoot` export
(Q5 — no UI entry point in this pass):
```ts
export const getWorkRoot = () => Control.GetWorkRoot();
export const pickWorkRootFolder = () => Control.PickWorkRootFolder();
export const changeWorkRoot = (path: string) => Control.ChangeWorkRoot(path);
```

**New `frontend/src/ui/work-root.ts`** (`<work-root>`), presentational like
`sync-view.ts`/`versions-view.ts` — no `wails-api` import, thunks injected:
```ts
export interface WorkRootInfo { path: string; isDefault: boolean }

@customElement("work-root")
class WorkRootEl extends LitElement {
  @property({ attribute: false }) get: () => Promise<WorkRootInfo>;
  @property({ attribute: false }) open: () => Promise<void>; // path-click target
  @property({ attribute: false }) pick: () => Promise<{ path: string; ok: boolean }>;
  @property({ attribute: false }) change: (path: string) => Promise<void>;
  @property({ type: Boolean }) idle = false; // gate, Q4

  @state() private _info: WorkRootInfo | null = null;
  @state() private _pendingPath: string | null = null; // picked, awaiting confirm (Q2)
  @state() private _phase: "loading" | "idle" | "confirm" | "busy" | "error" = "loading";
  @state() private _error = "";
}
```
Render: current path, itself a clickable control (`<button class="path">`,
Q5b) → `openRootFolder`; ellipsised via plain CSS `text-overflow: ellipsis`
(a tail-truncating `direction: rtl` variant was tried and dropped — it
reorders leading/trailing path separators under the bidi algorithm when the
text doesn't actually overflow, confirmed live in Storybook: `/Users/…`
rendered as `Users/…/`). Single **`Change`** button (Q5b) →
`pick` → inline confirm bar with the chosen path → Change/Cancel (Q2/Q6).
Disabled whenever `!idle` (Q4). No reset affordance (Q5).

**`advanced-view.ts`**: new `<section><p class="label">Work folder</p>
<work-root .get=... .open=... .pick=... .change=... ?idle=${...}>` sibling
of the existing sections, `idle` threaded down the same way `canUpdate`
already is.

**`ritual-app.ts`**: wires `getWorkRoot`/`openRootFolder`/
`pickWorkRootFolder`/`changeWorkRoot` from `wails-api.ts` into the new props,
`idle` derived from the same phase check `canUpdate` already uses.

```mermaid
flowchart TD
    A["<work-root> mount"] --> B["get() -> current path + isDefault"]
    P["path click"] --> O["open() -> Control.OpenRootFolder"]
    C["Change press"] --> D["pick() -> native dialog"]
    D -- "ok=false (cancel)" --> A
    D -- "ok=true" --> E["inline confirm: chosen path"]
    E -- "Cancel" --> A
    E -- "Change" --> F["change(path) -> Control.ChangeWorkRoot"]
    F -- "ok" --> A
    F -- "err" --> G["inline error text (Q6)"]
    F -.->|"progress"| J["main dial: PhaseRelocating (055 addendum, already shipped)"]
```

## Implementation Plan

- **Phase A** — `ControlService.PickWorkRootFolder` + `SetDirectoryPicker`;
  `cmd/gui/main.go` wiring with the verified `OpenFileWithOptions` call.
  Tests: nil picker degrades to `("", false)`; injected picker returning
  `("", nil)` (cancel) also degrades to `("", false)`; injected picker
  returning a path passes through; injected picker error degrades to
  `("", false)`.
- **Phase B** — `wails-api.ts` exports; `work-root.ts` component + story
  (mirrors `sync-view.stories.ts`'s verdict-matrix approach: idle/default,
  idle/non-default, picking, confirm-pending, busy, error) + behavior tests
  (`@web/test-runner`).
- **Phase C** — `advanced-view.ts` section + `ritual-app.ts` wiring.
  Manual verify (per `frontend/CLAUDE.md`, "start the dev server… test the
  golden path and edge cases"): Change to a real second folder, confirm the
  dial shows `PhaseRelocating`, confirm the section reflects the new path
  after; RUNNING-gate rejection surfaces its error text; path click reveals
  the folder in the OS file manager.

## Verification criteria

- Native OS folder picker opens seeded at the current workroot; Cancel
  returns to the section unchanged, no error shown.
- Choosing a folder shows an inline confirm with the chosen path before any
  data moves.
- Confirming triggers `ChangeWorkRoot`; the main dial shows the existing
  `PhaseRelocating` progress (055 addendum) for the duration; on success the
  section reflects the new path and `isDefault=false`.
- A rejection (`ErrSessionRunning`, `ErrRelocateInProgress`, validation,
  permission, corrupted-blob abort) renders inline error text; no partial UI
  state (path unchanged).
- Clicking the displayed path reveals the workroot in the OS file manager
  (`OpenRootFolder`).
- Path click and Change are both disabled whenever the session is not idle.

## Trade-offs

- **Pro:** all business logic (validation, ACID swap, RUNNING-gate) already
  landed in 055 — this phase is purely a thin dialog adapter + presentational
  component, no new backend risk surface.
- **Pro:** reuses the dial's existing `PhaseRelocating` telemetry instead of
  building a second progress UI inside Advanced.
- **Con:** two round-trips (pick, then change) instead of one combined
  dialog-and-commit call — deliberate (Q2), trades one extra IPC call for
  independently testable pick/commit and a visible pre-commit confirm.
- **Con:** `PickWorkRootFolder`'s `ok=false` conflates "user cancelled" with
  "dialog failed to open" — accepted (Q1): a dialog-open failure is not
  something the user can act on differently than a cancel, and Wails itself
  doesn't distinguish the failure mode further on macOS (empty channel close
  either way).

## Implementation Results

Phases A–C implemented and landed: `ControlService.PickWorkRootFolder`/
`SetDirectoryPicker`/`OpenControlFolder`, `cmd/gui/main.go` dialog wiring,
`work-root`/Advanced-view UI, `ritual-app.ts` wiring incl. `popToRoot()` on
change so the relocate takeover is visible over a pushed Advanced screen.
`go build ./...`, `go vet ./...`, `gofmt -l` clean; `tsc --noEmit` and
`vite build` clean; frontend test suite green.

Live testing against a real dev workroot (`ritualdev`) surfaced four bugs
beyond the original design's scope, in order:

1. **`verify()` false-positives on legitimate empty files.** A non-empty
   check reused across `objects/` and `server/worlds/` rejected a genuinely
   empty content-addressed blob (`xxhash64("") == "ef46db3751d8e999"`, a
   real, correctly-hashed key) and genuinely 0-byte `.mca` region files (a
   real Paper world legitimately has dozens — lazily-allocated POI/entity
   files for sparse areas like the Nether). Root-caused via a standalone
   `xxhash.Sum64([]byte{})` check confirming the "corrupted" object was not
   corrupted at all. Fixed by dropping the `server/worlds/` verify pass
   entirely (`copyContent` already errors on any stream failure — a second
   read-through only re-derived a signal copy already gave) and by giving
   `objects/` its own `verifyStreamIntegrity` with no non-empty check,
   relying solely on `CompressingStorage`'s decode+hash-on-`Close` integrity
   check.
2. **`cleanup()` deleted the CONTROL root on a never-relocated install.**
   `cleanup(oldRoot, oldDir)` called `os.RemoveAll(oldDir)` on the whole old
   root directory. On any install that had never relocated before,
   `config.WorkRoot == config.RootPath` — so this deleted `settings.json`
   moments after `commit()` had durably written the new `work_root` into it,
   plus the lock file and prior logs. The relocated content itself landed
   safely at the new destination; the pointer to it, and the whole CONTROL
   root, was wiped — confirmed via `.log` analysis (a `relocate: finished`
   entry immediately followed by a next-boot log showing `work_root: ""`,
   i.e. regenerated defaults). The most severe bug found this session. Fixed
   by scoping `cleanup` to `contentDirs` only, never the directory itself.
   Regression-guarded by
   `TestRelocating_FirstRelocateFromDefaultRoot_PreservesControlFiles`,
   which sets up the exact old-root-equals-`RootPath` topology the
   pre-existing tests structurally couldn't reach.
3. **Dial progress frozen during a large single file (2026-08-15).**
   `copyContent` originally published `RelocateProgress` only once per
   completed file, with `BytesDone` estimated proportionally from the
   file-count ratio (`BytesTotal × FilesDone / FilesTotal`). A world save's
   region files or `level.dat` can each dwarf the time between two file
   completions, so the dial's arc, size telemetry, and ETA all sat frozen
   for that file's entire transfer, then jumped — reported live as
   "progress not moving while transferring." Fixed by wrapping the
   destination write in the same `adapters.CounterStorage` tap
   `progress.Ticker` already uses for pull/push (`internal/adapters/
   counter.go`), so `RelocateProgress.BytesDone` is real bytes flushed to
   disk, not an estimate, and publishing on both a per-file boundary and a
   500ms heartbeat ticker (`relocateHeartbeat`, a `var` so tests can shrink
   it) so a single slow file still moves the dial mid-copy. `projection.go`'s
   `foldRelocate` now reads `BytesDone` straight off the event instead of
   deriving it. Regression-guarded by
   `TestCopyContent_HeartbeatPublishesProgressMidLargeFile`, which drips a
   slow fake source out over several chunks and asserts a heartbeat tick
   reports partial `BytesDone > 0` while `FilesDone` is still 0 — i.e.
   before any file-boundary event could have fired.
4. **Size + ETA telemetry (2026-08-11).** `RelocateProgress` gained an
   `Elapsed time.Duration` field so `foldRelocate` could feed
   `Projection.etaFromSessionAvg` — the identical beat-wide-average function
   `onTick` already uses for pull/push — rather than inventing a second ETA
   implementation. Frontend: `dial-telemetry.ts` gained a `hideSpeed` flag
   (relocate has no rate counter to show), `relocateSub()` renders the ETA
   countdown the same shape as `etaSub`, size moved into the dial's
   `underSlot: "telemetry-size"` block reusing `<dial-telemetry>` verbatim.

All four fixes are additive to the shipped Phase A–C surface, not deviations
from the Draft's design — `verify`/`cleanup`/progress-publish internals were
implementation detail the Draft didn't specify at this level.

5. **ETA frozen for 30+ seconds mid-transfer (2026-08-15, same day as #3).**
   Live testing of the CounterStorage-tap fix above (a 2021-file, 2.2GB
   relocate) showed `EtaSeconds` stuck at the exact same integer for 34 real
   seconds while ~930MB visibly flowed (`done=` climbing continuously in the
   on-disk log the whole time) — then a second, shorter freeze later in the
   same transfer. Not a stale-event or concurrency bug: `etaFromSessionAvg`
   (shared with pull/push, `internal/gui/projection/projection.go`) averages
   over the WHOLE beat since its absolute start, and a cumulative average
   becomes progressively less sensitive to new samples as elapsed grows —
   each additional second is a shrinking fraction of an increasingly long
   history. Pull/push don't show this visibly (network throughput is
   comparatively uniform across a transfer), but relocate's local-disk copy
   apparently isn't, running long enough for the effect to be visible as a
   dead flat number. Fixed by giving `etaFromSessionAvg` a 5-second sliding
   window (`etaWindowSeconds`, chosen to match `progress.Ticker`'s existing
   `DefaultWindowN=5` at its 1s cadence — the same "5-ish seconds of
   history" convention already used for the speed metric): once
   elapsed-since-anchor crosses the window, the anchor slides forward to the
   current sample so the next estimate reflects recent throughput rather
   than the whole history. Shared with pull/push (same function), but their
   existing tests all use elapsed spans well under 5s so behavior there is
   unchanged — confirmed by the full existing suite passing unmodified.
   Regression-guarded by
   `TestProjection_RelocateProgress_LateWindowSlide_ReactsToThroughputBurstFasterThanLifetimeAverageWould`,
   which constructs a throughput burst after the window has slid and asserts
   the estimate reacts to it immediately rather than being diluted by a
   6.5-second-old anchor.
