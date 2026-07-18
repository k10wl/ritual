# 038 — Restore to a previous version (world-save rollback)

| | |
|---|---|
| **Status** | Implemented (2026-06-04; live R2 smoke pending) |
| **Date** | 2026-06-04 |
| **Depends on** | [[031-bidirectional-sync]] (Download flow shape), [[035-publish-local-changes]] (data-sacred principle, dirty→Publish recovery), [[034-immersive-view-stack]] (Advanced placement) |
| **Scope** | Backend `ListVersions` + `Restore` control API, a new `RestoreRequested` command + `BuildRestore` pipeline, a target-pinned pulling resolver, and a `versions-view` section inside Advanced. World saves (refs) only — **not** the app binary ([[037-autoupdate]] owns that axis). |

## Background

Worlds are snapshotted as immutable, content-addressed **refs** (`refs/{id}.json`
on R2; `id` = `domain.RefID`, a dash-separated UTC timestamp that sorts
chronologically as a string). Each ref carries `Parent` (lineage), `Targets`
(scope globs) and `Objects` (path → {Hash, Size}). HEAD is resolved as the
lexicographically greatest id (`pulling.NewHeadResolver` → `storage.List("refs/")`
→ max).

`refs.Puller.Pull(ctx, id)` + `refs.Applier.Apply(ctx, id)` already work for **any**
ref id — they are generic and idempotent (pull fetches missing blobs, apply writes
`Objects` into the workdir and prunes in-scope paths not in the ref). The Pulling
stage simply always feeds them HEAD.

[[031-bidirectional-sync]] OQ2 explicitly **deferred** navigable history: the
lineage chain exists but nothing reads it; only HEAD is ever pulled. This log
delivers the deferred capability in its simplest honest form.

## Problem

A user who wants yesterday's world — after a creeper wipeout, a bad mod, a griefed
build — has no way back. The mechanics are 95% present (`Pull(id)`/`Apply(id)`),
but there is **no listing of past versions and no gesture to restore one**. The
gaps:

1. No control method enumerates historical refs with human metadata.
2. No command/flow pulls-and-applies an **arbitrary** ref (Pulling is HEAD-pinned).
3. No UI to pick a version and restore it.

## Questions and Answers

**Q1. Restore overwrites the workdir — doesn't that violate "user data is sacred"
([[035]])?**
A: No, by construction. Restore is **non-destructive at the version layer**: it
moves the *workdir* back to an older ref but **never moves HEAD and never deletes a
ref**. The newest version stays canonical on storage. After a restore the workdir
differs from local HEAD ⇒ the existing **dirty** detector ([[035]] §Q5) lights the
"Unpublished changes" cue, and **Publish** ([[035]]) makes the restored state the
new HEAD *parented on local HEAD* — a truthful "I went back to X and chose to keep
it" lineage. The only data at risk is **uncommitted workdir edits** present at
restore time; the confirm copy warns and points at Publish first (Q6).

**Q2. List local refs, remote refs, or both?**
A: **Remote** (R2) is the canonical version history — it holds the retention tiers
([[033]]) and teammates' versions; local keeps only `KeepLast:2` by default. So
`ListVersions` enumerates **remote** refs. Restore then `Pull(id)`s the chosen ref
(fetching any missing blobs from remote into local) before `Apply`. On a remote
listing error, degrade to **local** refs so an offline user can still roll back to
what's cached (the `Source` field tells the UI which it got). Union of both is
deferred — one source per call keeps the list unambiguous.

**Q3. How does an arbitrary target reach the Pulling stage (HeadResolver is
`func(ctx)` — no run state)?**
A: Add `TargetRefID domain.RefID` to `ritual.RunState` (mirrors the existing
`ParentRefID`). Generalize `pulling.Strategy`'s resolver to
`func(context.Context, *ritual.RunState) (domain.RefID, error)` and ship two
adapters: `pulling.FromHead(HeadResolver)` (ignores rs — every existing caller) and
`pulling.FromTarget()` (returns `rs.TargetRefID`, `ErrNoTarget` if empty). The
public `HeadResolver` type and `NewHeadResolver` are untouched; `control.go`'s
SyncProber keeps using them as-is. Lifecycle sets `rs.TargetRefID` from
`RestoreRequested` before dispatching the runner.

**Q4. What is the restore pipeline chain?**
A: `Checking → Pulling(target) → Done`. Same family as Download
([[031]] `BuildDownload`) but **target-pinned** and **without** the trailing local
retention: a restore pulls an *existing* ref (adds no new ref), so there is nothing
new to prune, and pruning the just-restored old ref from the local store is
harmless to the already-applied workdir. Read-only on remote — **no Acquiring (no
lock), no Probing, no Committing, no Pushing, no Unlocking** — exactly Download's
"never block a teammate" posture.

**Q5. New dial stage, or reuse downloading?**
A: **Reuse** `StageDownloading` / `PhaseDownloading` (bytes flow in; ETA visible) —
restore *is* a download of an older ref, visually identical. Add only a new
`ritual.FlowRestore` flow value so the projection/logs can name the gesture; the
dial copy stays the download beat. A bespoke "restoring" colour is unjustified
(007/017 economy).

**Q6. Confirm copy + dirty interaction.**
A: Inline two-step reveal, same pattern as `sync-view` (no dialog/popup/toast). Body
spells the consequence in data-sacred language:
> *"Bring back this older world. Your current world isn't deleted — but unsaved
> changes since your last version will be replaced. Publish first to keep them."*
When `GetSyncStatus().Dirty` is true the picker shows a **Publish first** secondary
action above Restore (non-blocking nudge, never a gate — [[035]] §Q4c posture).

**Q7. How much metadata per version row?**
A: Enough to choose, no more: relative + absolute time ("yesterday · 3 Jun 14:02"),
file count, total size, a **current** badge on HEAD. Parent lineage is read into the
model (future tree view) but **not rendered** in v1 — a flat reverse-chronological
list is the HIG-honest minimum. No diff/preview of contents (deferred).

**Q8. Bound the listing.**
A: `ListVersions` reads each ref JSON to get counts/sizes — N round-trips for N
refs. Retention caps remote refs to the tier union (tens, not thousands), so this is
fine. Hard-cap at the **newest 100** and `log()` if truncated (no silent cap —
[[028]]/methodology). Reads run concurrently (bounded fan-out) behind the 5s-style
timeout used by `GetSyncStatus`.

## Design

### Backend

```mermaid
flowchart LR
  UI[versions-view] -->|ListVersions| CS[ControlService]
  UI -->|Restore refID| CS
  CS -->|RestoreRequested{RefID}| BUS[(EventBus)]
  BUS --> LC[lifecycle.controller]
  LC -->|rs.TargetRefID = RefID| RUN[Runner]
  RUN --> CHK[Checking] --> PULL["Pulling(FromTarget)"] --> DONE((Done))
```

**1. Run state + commands** (`internal/core/ritual/`)
```go
// runstate.go
type RunState struct { /* … */ ParentRefID domain.RefID; TargetRefID domain.RefID }

// commands.go
type RestoreRequested struct{ RefID domain.RefID }
func (r RestoreRequested) String() string { return "restore requested " + string(r.RefID) }

// events.go
const FlowRestore Flow = "restore"
```

**2. Pulling resolver generalization** (`internal/core/stages/pulling/strategy.go`)
```go
type RefResolver func(ctx context.Context, rs *ritual.RunState) (domain.RefID, error)

func FromHead(h HeadResolver) RefResolver { return func(ctx context.Context, _ *ritual.RunState) (domain.RefID, error) { return h(ctx) } }

var ErrNoTarget = errors.New("pulling: no target ref on run state")
func FromTarget() RefResolver {
    return func(_ context.Context, rs *ritual.RunState) (domain.RefID, error) {
        if rs.TargetRefID == "" { return "", ErrNoTarget }
        return rs.TargetRefID, nil
    }
}
```
`Strategy.resolve` becomes a `RefResolver`; `New(...)` keeps its signature but stores
`FromHead(resolve)` internally so every existing call site is unchanged. A new
`NewWithResolver(puller, applier, RefResolver, onOK, onFail)` is what `BuildRestore`
uses. `Run` calls `s.resolve(stopCtx, rs)`. `ErrNoTarget` routes to onFail (a
restore with no id is a programmer error, not a no-op like `ErrNoHead`).

**3. Pipeline** (`internal/subsystems/pipeline/pipeline.go`)
```go
func BuildRestore(d Deps) machine.Strategy[ritual.RunState] {
    failCheck := failed.New(ritual.StageChecking)
    failPull  := failed.New(ritual.StagePulling)
    pull := pulling.NewWithResolver(d.Puller, d.Applier, pulling.FromTarget(), nil, failPull) // nil onOK ⇒ Done
    return checking.New(d.Checks, pull, failCheck)
}
```

**4. Lifecycle** (`internal/subsystems/lifecycle/lifecycle.go`)
```go
type Entries struct { Session, LocalSession, Download, Upload, Restore machine.Strategy[ritual.RunState] }
// in the event switch:
case ritual.RestoreRequested:
    c.startWith(ctx, c.entries.Restore, ritual.FlowRestore, false, withTarget(e.RefID))
```
`startWith` gains an optional run-state mutator so the target is set on the fresh
`RunState` before the runner spins (kept variadic so other gestures pass none).
`runHooks=false` (no livesync outside a Running session, like Download).

**5. Control API** (`internal/gui/control/control.go` + `versions.go`)
```go
type Version struct {
    ID        string `json:"id"`        // RefID timestamp
    UnixMs    int64  `json:"unixMs"`    // parsed for the UI to format locally
    Parent    string `json:"parent"`
    Files     int    `json:"files"`
    SizeBytes int64  `json:"sizeBytes"`
    IsHead    bool   `json:"isHead"`
    Source    string `json:"source"`    // "remote" | "local"
}

// VersionLister enumerates refs with metadata for a scope; injected over the
// matching store. scope "remote" degrades to "local" on error (Q2).
type VersionLister func(ctx context.Context, scope string) ([]Version, error)

func (c *ControlService) ListVersions(scope string) []Version // scope "remote"|"local"; degrades to nil on error/timeout
func (c *ControlService) Restore(refID string) error          // validate non-empty + parseable, publish RestoreRequested
```
`Restore` validates the id parses as `domain.RefIDFormat` before publishing, so a
malformed id never reaches the FSM. Lifecycle rejects the gesture while another flow
is Running (existing shared-status guard).

### Frontend

New **`versions-view`** component (`frontend/src/ui/versions-view.ts`), pushed as a
new section row inside `advanced-view` ("Versions"). Presentational + `wails-api`
wiring stays in the host (`ritual-app.ts`):

- On mount: `listVersions()` → render reverse-chronological rows (reuse `rune-row`:
  `leading` = relative time, `meta` = "N files · X MB", `trailing` = **current**
  badge on HEAD or a Restore affordance).
- Restore = inline two-step reveal (lift `sync-view`'s confirm shape; no new modal).
  When `dirty`, show **Publish first** secondary above Restore.
- Confirm → emit `restore { refID }`; host calls `restore(refID)`, then
  `popToRoot()` so the dial takes over with the download beat.
- Empty / single-version / error states each render explicit copy (no blank pane).

`wails-api.ts` gains `listVersions()` and `restore(id)` thin wrappers + the
`Version` type re-export.

## Implementation Plan

**Phase A — pulling resolver.** `RefResolver` + `FromHead`/`FromTarget` + `ErrNoTarget`;
`Strategy` stores a `RefResolver`; `NewWithResolver`. Update existing `New` callers
(none change behavior). Unit tests: target hit, empty-target → onFail, head adapter
parity with current tests.

**Phase B — run state + command + flow.** `RunState.TargetRefID`,
`RestoreRequested`, `FlowRestore`. `startWith` run-state mutator. Lifecycle route +
`Entries.Restore`. Integration test: `RestoreRequested{old}` pulls+applies the old
ref, HEAD unchanged, workdir == old ref's Objects.

**Phase C — pipeline + composition.** `BuildRestore`; wire `Entries.Restore` in
`cmd/gui`. Confirm `BuildRestore` shares the same Deps.

**Phase D — control API.** `Version`, `VersionLister` (remote-first, local
fallback, newest-100 cap + log), `ListVersions`, `Restore`. Wire the lister closure
in `cmd/gui` over remote+local stores. Tests stub the lister.

**Phase E — frontend.** `versions-view` + story (versions / empty / dirty / error) +
test. `listVersions`/`restore` in `wails-api`. Wire the section into `advanced-view`
+ `ritual-app` (`restore` event → `restore()` → `popToRoot`).

**Phase F — verify.** Go: `go test ./...`. Frontend: `skill: verify` (Storybook +
`npm run test` + Wails dev build). Manual: pick an older version, restore, confirm
workdir reverts and "Unpublished changes" + Publish recover it.

## Examples

✅ Restore old ref → workdir reverts, HEAD untouched, dirty cue lights, Publish makes
it canonical with `Parent = previous local HEAD` (truthful rollback lineage).
✅ Offline → `ListVersions` degrades to local refs; restore works on cached refs.
❌ Restore does **not** delete refs or move HEAD (that would destroy history — the
035 sin).
❌ No new dial colour for restore — it reuses the download beat (007/017 economy).

## Trade-offs

- **Reuses the download dial beat** — a restore reads as a download to the user.
  Accepted: it *is* a transfer of an older ref; a bespoke colour adds states for no
  comprehension gain. `FlowRestore` keeps logs honest.
- **No lineage/tree view, no content diff** — flat reverse-chronological list only.
  Accepted v1; `Parent` is parsed into the model so a future tree is non-breaking.
- **Per-ref metadata round-trips** — N reads per `ListVersions`. Bounded by retention
  (tens of refs) + newest-100 cap; concurrent + timeout-guarded.
- **Resolver generalization touches every Pulling call site** — but `FromHead`
  preserves exact current behavior, so the blast radius is mechanical and test-covered.

## Verification Criteria

1. `RestoreRequested{id}` pulls+applies `id`; workdir Objects == ref `id` Objects;
   local HEAD (max timestamp) **unchanged**; no ref deleted (integration test).
2. After restore, `GetSyncStatus().Dirty == true`; a subsequent Publish writes a new
   ref with `Parent == previous local HEAD` ([[035]] parity test).
3. Restore is read-only on remote: no lock acquired, no push, no unlock (assert no
   Acquiring/Pushing events on the bus during a restore run).
4. `ListVersions` returns reverse-chronological versions with correct `IsHead`,
   counts, sizes; degrades to local on remote error; caps at 100 with a log.
5. `Restore("")` / malformed id is rejected before any bus publish.
6. `versions-view` renders versions / empty / dirty / error; Restore emits
   `restore { refID }`; Storybook + `npm run test` green; Wails build passes.

## Open Questions

- **OQ1** — Should a restore also be offered for the **local-only** ([[036]]) cache
  when remote is reachable but the user wants a *cached* older state specifically?
  (Lean: no — `Source` already exposes which list was returned; a source toggle is a
  later refinement.)
- **OQ2** — Retain the restored ref locally so a second restore is offline-instant,
  or let local retention prune it? (Lean: let retention prune; Pull re-fetches.)

## Implementation Results (2026-06-04)

All phases A–E shipped; backend `go test ./...` green (incl. a restore
integration test), frontend `web-test-runner` 81/81 green, `tsc` + vite build
clean.

**What landed, by phase:**
- **A — resolver.** `pulling.RefResolver` + `FromHead`/`FromTarget` + `ErrNoTarget`
  (`strategy.go`); `Strategy.resolve` is now a `RefResolver`; `New` wraps
  `FromHead` (every Session/Download call site unchanged); `NewWithResolver` for
  restore. `RunState.TargetRefID` added. Tests: target pull, empty-target→onFail,
  FromHead-ignores-target.
- **B — command/flow/lifecycle.** `ritual.RestoreRequested{RefID}`, `FlowRestore`;
  `Entries.Restore`; `startWith` gained a variadic run-state mutator (restore sets
  `rs.TargetRefID`); switch case routes `RestoreRequested` with `runHooks=false`.
- **C — pipeline.** `pipeline.BuildRestore` (`Checking → Pulling(FromTarget) →
  Done`, no retention, read-only on remote); wired `Entries.Restore` in `cmd/gui`.
- **D — control.** `control.Version`, `VersionScope`, `VersionLister`,
  `NewVersionLister` (`versions.go`); `ListVersions(scope)` + `Restore(id)` on
  `ControlService`; lister wired in `cmd/gui` over local+remote stores via a
  `makeRefReader` factory (refactored from the old single-store `readRef`).
  Constructor gained a `versions` param (test/call sites updated). Unit tests:
  newest-first + HEAD flag + metadata sums, remote→local degrade, empty, key
  filtering, Restore validation/publish.
- **E — frontend.** `versions-view` component (list/empty/error/confirm states,
  inline two-step restore, dirty→"Publish first" nudge) + story + 8 tests;
  `wails-api` `listVersions`/`restore`/`Version`/`VersionScope`; "Versions" section
  in `advanced-view`; host wires `.versions`/`?dirty` + `@restore`/`@publishfirst`
  in `ritual-app`. Wails bindings regenerated (`wails3 generate bindings`).

**Deviations from the design:**
- **§Q8 newest-100 cap dropped.** `listScope` reads all refs **sequentially**
  under the 8s `ListVersions` timeout rather than capping at 100 with a concurrent
  fan-out. Rationale: retention ([039]) bounds ref counts to tens; a cap + log + a
  concurrency pool was unjustified complexity for the real data volume, and a
  silent cap would have violated the no-silent-truncation rule anyway. If volumes
  grow, re-introduce a cap **with** a surfaced "showing newest N" note.
- **§Q5 `RefReader` reuse.** The existing `cmd/gui` `readRef` closure was
  generalised into a `makeRefReader(store)` factory so local + remote readers share
  one body (the dirty prober keeps the local instance).
- **No deviation** on the core invariants: HEAD never moves, no ref deleted,
  restore reuses the download dial beat, restored workdir reads as dirty
  (integration-asserted).

**Verification vs criteria:** VC1 (pull+apply target, HEAD unchanged, no ref
deleted, read-only/no-lock) — integration test
`TestIntegration_Restore_OlderRef_RevertsWorkdirHeadUnchangedNoLock`. VC4 (list
newest-first, IsHead, sums, degrade, empty) + VC5 (reject empty/malformed) — control
unit tests. VC6 (versions/empty/dirty/error states, `restore` event, build) —
`versions-view` tests + FE build. VC2/VC3 dirty→Publish parity rely on the existing
[035] dirty path (unchanged). **Pending:** live R2 smoke (VC manual) — a real
multi-version remote restore in the running app.
