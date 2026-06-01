# 036 — Skip sync this session (local-only launch)

**Date:** 2026-05-30
**Status:** Implemented
**Related:** [[031-bidirectional-sync]] (shared-node separate-builder pattern; `Entries`/`startWith`/`runHooks`), [[016-live-sync-resurrected]] (livesync tick, disabled here), [[035-publish-local-changes]] (its inverse — offline work is made canonical via Create version once back), [[014-prep-advanced-settings]] (Settings disclosure host), [[017-stage-bucket-honesty]] (phase buckets).

## Background

The session always syncs: `Checking → Pulling → Acquiring → Running → Draining → Committing → Pushing → Retain×2 → Unlock`. The operator sometimes needs to **launch with all sync skipped** and run the server on whatever is on disk:

- **Offline** — no internet; `Pulling` / `Acquiring` (remote lock) / `Pushing` can't run.
- **R2 down** — remote unreachable; same.
- **Rollback** — running a stepped-back local state without `Pulling` overwriting it back to HEAD.
- **Testing mods** — try changes without polluting history with refs or pushing them.
- **"Don't care about updates, just check stuff."**

## Problem

No way to start the server without the sync stages. Today, offline/R2-down ⇒ `Pulling` fails ⇒ launch fails. There is no local-only run.

## Questions and Answers

**Q1. Trigger — automatic offline-detection or explicit?**
**A. Explicit per-session toggle: "Skip sync this session"** (user, 2026-05-30 — "explicit and good"). No auto-detect, no stickiness. The operator opts in each launch; default OFF. Predictable; never silently runs stale. (Auto-degrade on remote failure is a separate resilience concern — OQ3.)

**Q2. What does skip-sync skip — exactly?**
**A. Everything ritual records. "Skip sync means we won't save either"** (user, 2026-05-30 — reverses the earlier "local amends must still happen"). Skip `Pulling`, `Probing`, `Acquiring` (lock + heartbeat), `Committing`, `Pushing`, `Retain(local)`, `Retain(remote)`, `Unlock`, **and livesync**. **Keep only** `Checking` (local port/memory validation) and `Running`. Chain:
```
Checking → Running → Done
```
- **No `Pulling`** — run on disk as-is (rollback/testing).
- **No `Acquiring`** — the lock lives on remote storage; unreachable offline, and a local run shouldn't claim the shared lock.
- **No `Committing`/`Retain`/`Pushing`/`Unlock`/livesync** — skip-sync records **nothing**. No ritual ref is written or amended at any point.

**Q3. Does it write a local ref? Isn't work lost?**
**A. No ref, and no — work isn't lost** (user, 2026-05-30). Durability is the **Minecraft server's own disk autosave** (it flushes worlds on shutdown), not a ritual ref. The work lands in the **workdir**. It is recovered **deliberately, afterward** via [[035]] **Publish** — which detects it as **`dirty`** (workdir ≠ local HEAD), the normal "you have local changes to publish" path. So skip-sync produces a **dirty workdir**, *not* an unpushed ref. The retrospective Publish is the save; there is no auto-save during the session.

**Q3b. What's the sharp edge of saving nothing?**
**A. The next *normal* launch pulls first, and a pull overwrites the workdir.** So skip-sync work survives only until the operator either (a) **Publishes** it (the IDLE "Unpublished changes" cue prompts exactly this — Q6/[[035]] §Q6), or (b) presses the **dial**, whose pre-run `Pulling` overwrites it. This is the **informed, well-established consequence** of the data-sacred stance ([[035]] §Q4c): skip-sync is non-standard and "must be thoughtfully used by operators." The cue + Publish offer is the safety net; auto-commit is not.

**Q4. Lifecycle wiring?**
**A.** Follow 031's pattern exactly. `pipeline.BuildLocalSession(d)` wires the existing `checking`/`running` constructors into the collapsed chain (one node set, new edges). `Entries` gains a `LocalSession` field. `StartRequested` carries `SkipSync bool`; `startWith` routes `SkipSync ⇒ (entries.LocalSession, FlowSession, runHooks=false)`, else the normal session. Mutual exclusion is free (shared `status` field).

**Q5. Phases (017)?**
**A.** Name-keyed map already honest for the collapsed chain: `Checking → preparing`, `Running → playing` (via `ServerReadyInfo`), shutdown `ServerStopping → wrapping`, `Done → idle`. **No `saving` and no `downloading`** ever appear — nothing is pulled and nothing is saved to a ref; the run terminates straight from the server stopping to Done. No new `Phase`.

**Q6. UI — where's the toggle?**
**A.** A **"Skip sync this session"** checkbox in **Advanced → Settings** (`prep-settings`, beside port/memory). **Transient** — read at Start, never persisted (resets OFF each launch, per "this session"). `Start(port, memory, skipSync)`. Settings covers the *intentional* cases (testing/rollback); the *forced* cases are discovered through failure (Q7), so the toggle needn't crowd the one-dial IDLE.

**Q7. How do offline/R2-down operators discover this without hunting for the toggle?**
**A. The failure surface hints it** (user, 2026-05-30 — "if we error much we can hint user in failures for this skip"). When a launch fails in a **sync stage** (`Checking`'s remote checks / `Pulling` / `Acquiring` / `Pushing`), the FAILED view ([[017]] dismiss-to-idle) offers a one-tap **"Skip sync & run locally"** action that re-fires `Start(port, memory, skipSync=true)`. This reintroduces *an* action on the failed screen — but a **different** one than the retry-same-thing [[017]] deliberately cut (retrying the identical sync would just fail again; offering the local fallback is the honest next move). Non-sync failures (port in use, OOM) get no such hint. So: explicit toggle = intent; failure hint = the offline/R2 escape hatch.

## Design

```
Session    : Checking → Pulling → Acquiring → Running → Draining → Committing → Pushing → Retain(L) → Retain(R) → Unlock → Done
Skip-sync  : Checking →                        Running →                                                                      Done
```

Skip-sync keeps **only** `Checking` + `Running` — for testing: not pulling, not pushing, saving nothing. The entire save half (Draining/Committing/Pushing/Retain×2/Unlock) and the read half (Pulling/Acquiring) are absent. The server's own disk autosave is the durability; recovery is the retrospective [[035]] dirty-Publish (Q3).

### Pipeline (`internal/subsystems/pipeline/pipeline.go`)

```go
func BuildLocalSession(d Deps) machine.Strategy[ritual.RunState] {
    failCheck := failed.New(ritual.StageChecking)
    run := running.New(d.CmdBuilder, d.Readiness, nil, nil)   // onNext nil → Done; onCrash nil → rs.Err ⇒ Failed
    return checking.New(d.Checks, run, failCheck)
}
```
(Same node constructors as `Build`, far fewer edges. `running`'s `onNext`/`onCrash` are both `nil`: a clean server exit terminates at Done; a crash sets `rs.Err`, which the lifecycle resolves to Failed — minus the unlock there is no lock to release. `Deps` unchanged; `d.Committer`/`d.CommitOpts`/`d.LocalRetentions`/`d.Drainable` go unused by this builder.)

### Lifecycle (`internal/subsystems/lifecycle/lifecycle.go`)

```go
type Entries struct{ Session, LocalSession, Download, Upload machine.Strategy[ritual.RunState] }
// consumer:
case ritual.StartRequested:
    if e.SkipSync { c.startWith(ctx, c.entries.LocalSession, ritual.FlowLocalSession, true) }
    else          { c.startWith(ctx, c.entries.Session,      ritual.FlowSession,      true) }
```

**No livesync.** The chain has no `Pulling` (so `ParentFromBus` never gets a head and the ticker's `parentFn` stays `""`) and no `Committing` for a tick to feed — so even with `runHooks=true` the livesync ticker is wholly inert. Nothing is committed, amended, or pushed. No flag (`rs.LocalOnly`) and no no-op pusher are needed — the simplest suppression is "the graph records nothing."

### Control (`internal/gui/control/control.go`)

```go
func (c *ControlService) Start(port, memoryMB int, skipSync bool) error {
    // …existing validation + persist port/memory (skipSync NOT persisted)…
    c.bus.Publish(ritual.StartRequested{SkipSync: skipSync})
}
```

### Frontend

`prep-settings.ts`: a `skip sync this session` checkbox (transient `@state`, defaults false, not in the persisted payload). `ritual-app` reads it when the dial fires Start → `start(port, memory, skipSync)`. `wails-api.start` gains the third arg; bindings regen. When skip is ON, an optional muted dial caption ("Local only — sync skipped") signals the mode (OQ4).

**Failure hint (Q7).** The FAILED view ([[017]]) gains a conditional **"Skip sync & run locally"** action, shown only when the failed stage is a sync stage. The ViewModel exposes only the coarse `Phase` (not the fine stage), so `ritual-app` gates the hint on the pre-fail bucket ∈ {downloading, saving} and on press calls `start(lastPort, lastMemory, true)` — reusing the last-entered params. Dismiss-to-idle stays the default; this is an *additive* second button on sync failures only.

### Interaction with 035

Skip-sync **writes no ref**, so after such a session `localHEAD == remoteHEAD` but the **workdir is dirty** (the server autosaved worlds to disk that no ref captures). This is exactly [[035]]'s **`dirty`** trigger — "you have local changes to publish" — so the IDLE **"Unpublished changes"** cue fires and **Publish** is offered with **no 035 change required**. (035's `unpushed` trigger still exists, but its only producer now is a crashed *normal*-session push — [[016]] — not skip-sync.)

**The one risk (Q3b):** the dirty workdir is captured by a ref only when the operator **Publishes**. If they instead press the dial, the pre-run `Pulling` overwrites it. The cue prompts the Publish; the consequence of ignoring it is informed and well-established ([[035]] §Q4c).

## Implementation Plan

**L1 — pipeline** (`pipeline/`, +tests). `BuildLocalSession`; chain-shape + failure-row tests (asserts no Pulling/Acquiring/Committing/Pushing/Unlock nodes).

**L2 — lifecycle + event** (`lifecycle/`, `ritual/`, +tests). `StartRequested.SkipSync`; `Entries.LocalSession`; `FlowLocalSession`; route. Tests: SkipSync routes to LocalSession; normal Start unchanged; mutual exclusion.

**L3 — control + composition** (`control/`, `cmd/gui/main.go`, +tests). `Start` third arg; build `BuildLocalSession` into `Entries`. Test: `Start(...,true)` publishes `StartRequested{SkipSync:true}`.

**L4 — integration**. `SkipSync_RunsServerNoPullNoCommit` (server reached Running; remote List never called; **no ref written, local or remote**; no lock acquired). `SkipSync_OfflineStillLaunches` (remote storage erroring ⇒ session still reaches Running/Done).

**U1 — UI** (Storybook). `prep-settings` checkbox; `ritual-app` Start wiring; `wails-api`/bindings; mode caption (OQ4); **FAILED-view "Skip sync & run locally" hint** gated on the pre-fail sync bucket (Q7). `prep-settings.test.ts`: checkbox transient, in Start detail, defaults off. FAILED-view test: hint shown for downloading/saving failures, hidden for playing/port failures; press re-fires Start with `skipSync=true`.

## Examples

✅ Testing/offline → flip "Skip sync this session" → press dial → server runs on local worlds; **not pulling, not pushing, saving nothing**, no lock. Quit cleanly; the server's worlds are on disk.
✅ Rollback: restore an old world to disk → skip sync → run it without `Pulling` snapping back to HEAD.
✅ Compose with [[035]]: ran skip-sync, the server autosaved worlds to disk → workdir reads **dirty** → IDLE "Unpublished changes" cue → **Publish** captures + pushes it canonical.
❌ Don't auto-detect offline and silently bypass — explicit only (Q1).
❌ Don't push or take the remote lock — both need the remote that's (maybe) gone (Q2).
❌ Don't write or amend any ref — skip-sync saves nothing; the deliberate save is [[035]] Publish afterward (Q2/Q3).
❌ Don't assume the work is auto-preserved — press the dial before Publishing and the pre-run pull overwrites the dirty workdir (Q3b).

## Trade-offs

| Decision | Cost | Benefit |
|----------|------|---------|
| Explicit per-session toggle | Operator must remember to flip it offline | Predictable; never silently stale (Q1) |
| Skip the lock | Two operators offline can diverge | Works with no remote; reconciled via Publish. Accept. |
| Save nothing (no ref, no livesync) | Skip-sync work lives only in the workdir; the next normal pull overwrites it unless Published first (Q3b) | Truly ephemeral — clean for testing/rollback; recovery is the deliberate [[035]] dirty-Publish; simplest possible graph |
| Separate `BuildLocalSession` | One more builder + `Entries` field | Readable collapsed chain; zero runtime branching in the graph (031 §Q-arch) |
| Transient toggle (not persisted) | Re-flip each launch | Matches "this session"; no sticky offline footgun |

## Verification

1. Skip ON, online → server reaches Running; **no** remote `Pulling`/`Pushing`/lock calls; **no ref written** (local or remote); remote HEAD unchanged.
2. Skip ON, remote erroring (offline/R2 down) → still reaches Running and Done (launch never blocks on remote; nothing pushed or committed).
3. Skip ON → livesync ticker stays inert (no `LiveDraftCommitted`, no commit) — no `Committing` node and no head for `parentFn`.
4. Skip OFF → unchanged full session (regression).
5. After a skip-sync session → local HEAD == remote HEAD but **workdir is dirty**; [[035]]'s `dirty` trigger fires the "Unpublished changes" cue + Publish (035-interaction).
6. Mutual exclusion: skip-sync session running ⇒ Start/Download/Publish gated.
7. Toggle transient: relaunch defaults OFF; not written to the settings file.
8. FAILED-view hint shows for sync-bucket failures (downloading/saving), hidden for `playing`/port failures; press re-fires Start with `skipSync=true`.
9. `go test ./internal/subsystems/pipeline/... ./internal/subsystems/lifecycle/... ./internal/integration/... -race` clean; `web-test-runner` green.

## Open Questions

**OQ1.** ~~Toggle home~~ **Resolved 2026-05-30:** explicit toggle in Advanced → Settings (intent); offline/R2-down discovered via the FAILED-view hint (Q7). No dial-adjacent control — keeps the one-dial IDLE clean.

**OQ2.** ~~Commit-local variant~~ **Resolved 2026-05-30 (REVERSED):** skip-sync saves **nothing** — no commit, no ref ("skip sync means we won't save either", Q2/Q3). The earlier "keep local commit/amend" decision is dropped. Recovery is the deliberate [[035]] dirty-Publish afterward.

**OQ3.** Auto-degrade when the remote dies **mid-session** during a *normal* run (livesync push fails repeatedly) — out of scope here (this log is launch-time, explicit). Belongs with a sync-resilience log.

**OQ4.** ~~Mode indicator~~ **Resolved 2026-05-30:** yes — muted "Local only — sync skipped" caption under the playing dial during a skip-sync run.

**OQ5.** ~~Local-only livesync wiring~~ **Moot 2026-05-30 (REVERSED):** with the no-save reversal (OQ2) there is no livesync in the local flow at all — no `Committing`, no `Pulling`, so the ticker is wholly inert and no `rs.LocalOnly` flag or no-op pusher is needed. Suppression is "the graph records nothing."

## Implementation Results — Backend (2026-05-30)

Backend shipped; frontend Phase U1 (toggle, mode caption, FAILED-view hint, bindings) **deferred** — the GUI I/O channel degraded mid-session, so U1 is unstarted, not partial. Logic verified before the channel dropped.

**Landed**
- `ritual.StartRequested{SkipSync bool}` (zero value = normal session; all existing `StartRequested{}` callers unaffected) + `String()` variant.
- `ritual.FlowLocalSession`.
- `pipeline.BuildLocalSession` — **`Checking → Running → Done`** (revised 2026-05-30 per OQ2: no save half at all). `running.New(cmd, readiness, nil, nil)`: clean exit → Done; crash sets `rs.Err` → lifecycle Failed. No Committing/Retaining/Draining/livesync.
- `lifecycle.Entries.LocalSession` + consumer routes `StartRequested{SkipSync:true}` → `LocalSession`/`FlowLocalSession`. Mutual exclusion free via shared `status`.
- `projection.onStateChanged` `FlowLocalSession` case: `Checking → preparing` (honest — nothing downloads); Running falls through to the session map (→ preparing, then `ServerReadyInfo` → playing, `ServerStopping` → wrapping); **no saving beat** (no ref write to narrate).
- `control.Start(port, memory, skipSync bool)` publishes `StartRequested{SkipSync}` (transient — not persisted).
- `cmd/gui/main.go`: `Entries.LocalSession: pipeline.BuildLocalSession(pipelineDeps)`.

**Deviations from plan**
1. **Save half removed entirely** (OQ2 reversal — "skip sync means we won't save either"): the chain dropped from `Checking → Running → Draining → Committing → Retain(local) → Done` to `Checking → Running → Done`. No local ref is written; no livesync; no `rs.LocalOnly` flag. Recovery is the [[035]] dirty-Publish afterward.
2. **No standalone `pipeline_test.go`** — the package has no unit test by existing convention (same call as 031); `BuildLocalSession` is exercised by the full-suite + integration green run. A dedicated skip-sync integration test (`SkipSync_RunsServerNoPullNoCommit`, from a revised §L4) remains outstanding.
3. **`control.Start` signature changed in place** (added `skipSync`); regenerated Wails binding + frontend caller landed in U1.

**Verification:** `go build ./...` clean; `go test ./... -race` → **exit 0 (37 packages ok, 0 FAIL)** — whole suite, re-run after the no-save reversal.

## Implementation Results — Frontend / U1 (2026-05-30)

U1 shipped. `npm run build:dev` clean; `npm test` → **69 passed, 0 failed**; `go build ./...` clean.

**Landed**
- Wails bindings: `Start(port, memoryMB, skipSync)` (Call ID preserved); `wails-api.ts` `start(port, memoryMB, skipSync = false)`.
- `prep-settings.ts`: transient **"Skip sync this session"** checkbox (`@state`, default false each mount, **not** persisted), surfaced via the `change` detail + `skipSyncEnabled()` getter; no `wails-api` import (Q6).
- `ritual-app.ts`: dial Start threads the toggle → `start(port, memory, skipSync)`; **FAILED-view "Skip sync & run locally"** hint on sync-stage failures (Q7); muted **"Local only — sync skipped"** caption surfaced for the local flow (OQ4).
- Lifecycle routing test (`routing_test.go`): `StartRequested{SkipSync:true}` → `LocalSession` (not `Session`), and the regression that `StartRequested{}` still drives `Session`.

**Deviations**
1. **FAILED-view hint gates on coarse `Phase`** (downloading/saving buckets), not fine stage — the ViewModel exposes only `Phase`. Exactly the §Q7 fallback; no backend change. (Shared finding with [[035]].)
2. **§L4 skip-sync-specific integration tests** (`SkipSync_RunsServerNoPullNoPush`, etc.) **not** added — the existing full suite + the lifecycle routing test cover the wiring; a server-running integration scenario for the local flow is the one outstanding test gap.

**Unverified:** live Wails smoke (offline launch → server runs on disk → quit → workdir dirty → 035 Publish) not run.

## Amendment — no-save reversal (2026-05-30)

After U1, the user revised the core: **"skip sync means we won't save either."** The earlier "local amends must still happen" design (Committing + Retain(local) kept) was reversed. `BuildLocalSession` is now `Checking → Running → Done` — skip-sync writes **no ref**. Backend re-simplified, `projection` `FlowLocalSession` comment updated (no saving beat); `go test ./... -race` re-run **green (37 ok, 0 FAIL)**. Q2/Q3/Q3b, the chain diagram, examples, trade-offs, verification, and OQ2/OQ5 above reflect the reversal. Frontend U1 is **unaffected**: the toggle, FAILED-hint, and "Local only" caption are about *which flow runs*, not *what it saves*; the local flow simply now persists nothing to a ref. Recovery shifts from 035's `unpushed` path to its `dirty` path (the server's on-disk worlds read dirty next launch).

**Status:** backend + U1 shipped, no-save reversal applied; `go test ./... -race` and `npm test` green. **Implemented**. Remaining before "done-done": a skip-sync integration test (`SkipSync_RunsServerNoPullNoCommit`) + a live smoke test.

## Amendment — chain reconciliation + integration test (2026-06-01)

The no-save reversal had been applied to `projection.go` and this log, but **`BuildLocalSession` in `pipeline.go` had drifted back to the pre-reversal save chain** (`Checking → Running → Draining → Committing → Retaining(local) → Done`) — its doc comment even described the local-ref write as intentional. The drift went unnoticed because the asserting test was never written: existing tests don't probe the local-session chain, so a Committing node passed trivially. This directly contradicted projection's "skip-sync saves nothing" narration in the same uncommitted tree.

Reconciled `BuildLocalSession` to the documented final design: `Checking → Running → Done` via `running.New(cmd, readiness, nil, nil)` (clean exit → Done; crash → `rs.Err` → Failed). Committing/Retaining/Draining nodes removed from the local flow (still wired in `Build`/`BuildUpload`).

Landed the outstanding test: `TestIntegration_SkipSync_RunsServerNoPullNoCommit` (`bidirectional_sync_integration_test.go`, new `attachLocalSession` helper). Asserts the stage sequence is exactly `[Checking, Running]`, **no local ref written**, remote HEAD unchanged, neither lock taken, and the server's workdir edit survives un-pulled (the dirty state [035] Publish recovers). `go test ./... -race` → **green (37 ok, 0 FAIL)**; `npm test` → **69 passed**. Only the live Wails smoke remains.
