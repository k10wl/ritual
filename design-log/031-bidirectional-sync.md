# 031 — Bidirectional Sync (Download / Upload + launch staleness)

**Date:** 2026-05-29
**Status:** Draft
**Supersedes:** [[021-sync-upstream]] (single local-authoritative "Sync upstream" force-push).
**Related:** [[016-live-sync-resurrected]] (commit/push substrate, retention tail), [[014-prep-advanced-settings]] (IDLE Advanced disclosure host), [[017-stage-bucket-honesty]] (Phase taxonomy), [[007-hig-ux-coherence]] (one-dial metaphor), [[009-telemetry-hierarchy]] (dial `sub` caption).

## Background

`ritual` moves worlds only inside a session: pull HEAD → run server → commit + push. Three needs have no producer **without launching the server**, and one need has no surface at all:

1. **Get latest** without playing — a teammate pushed; I want their world on disk.
2. **Publish local** without playing — seed an empty remote, or make externally-edited worlds authoritative.
3. **Know I'm stale** — at rest on the IDLE screen, surface that the remote moved since my last sync.

021 solved only #2, local-authoritative, via a new `Probing` stage + a single prominent "Sync upstream" button. This log replaces it with **two directional flows over shared stage nodes** plus a **passive launch staleness check** — and a deliberately *un-prominent* UI, because the normal session flow already downloads (§Q-UI).

## Problem

Need server-free world movement in **both** directions, plus a passive "you're behind" cue, without bloating the one-dial IDLE screen (007) or destroying local edits silently.

## Questions and Answers

**Q1. Does Upload need to resolve the remote HEAD (a dedicated stage), or can it commit `Parent=""`?**
**A. It resolves HEAD — `Parent = remote HEAD` (lineage chosen, 2026-05-29).** Refs are self-contained, so `Parent=""` would be *functionally* fine today (HEAD = max timestamp, GC walks each ref's own `Objects`, retention by keyspace — nothing reads ancestry). But we want lineage links for a future history/rollback feature, so Upload parents on the current remote HEAD. Resolving it object-free (no pull, no apply — local files win) is a thin `probing` stage over the existing `pulling.HeadResolver`: `id,err := resolve(ctx)` → `rs.ParentRefID = id`; `ErrNoHead` → `""` (seed); error → `Failed(Probing)`. Doing it in the `CommitOptsResolver` instead is rejected: that signature is `func(rs) CommitOpts` — no `ctx`, no error return, so a failed HEAD read couldn't route to Failed. Fallible I/O belongs in a stage, not a pure mapper.

**Q2. Can one "Sync" button do both directions in one chain?**
**A. No.** `Applier` is not an overlay (`ports.go:45`: *"pruning paths out-of-ref but in-scope"*). A full Pulling **deletes local-only in-scope files**, so a linear `Pull → Commit → Push` is remote-wins and silently destroys local edits before the push (no merge — region files are binary). The directions are different operations ⇒ two flows.

**Q3. Download chain?**
**A.** `Checking → Pulling → Retaining(local) → Done`. Read-only on remote ⇒ **no lock, no push**. Local retention after the pull bounds accumulated local refs (user constraint, 2026-05-29).

**Q4. Upload chain?**
**A.** `Checking → Probing → Acquiring → Committing → Pushing → Retaining(local) → Retaining(remote) → Unlocking → Done`. = session chain with **Pulling→Probing** (head-only, no download/apply) and **Running + Draining removed**.

**Q5. Upload `CommitOpts`?**
**A. Existing resolver, unchanged.** `NewCommitOptsResolver`: `rs.RefID=="" ⇒ CommitOpts{Parent: rs.ParentRefID}`. Probing writes `rs.ParentRefID`; Upload never sets `rs.RefID`. So the existing else-branch produces `Parent = remote HEAD` with zero new resolver code.

**Q6. Staleness check — what, when, where computed?**
**A.** A **control-level query**, not a pipeline (like `GetPrep`). On app launch the frontend calls `GetSyncStatus()`:
```
localHEAD  = HeadResolver(localStore)
remoteHEAD = HeadResolver(remoteStore)
behind     = remoteHEAD > localHEAD     // RefID is a timestamp; lexical == chronological
```
Returns `{Behind bool, LocalHead, RemoteHead string}`. **Boolean only** — no "behind by N" (that needs sequence ids, deferred — OQ2). Once on launch; polling is the same call on a timer if ever wanted. Offline/error ⇒ `Behind=false` + surfaced error ⇒ UI shows nothing (degrade silently, OQ3).

**Q7. Does Download need the lock?**
**A. No.** Pulling only reads remote; the session takes the lock only just before `Running`. A locking Download would needlessly block a teammate about to play.

**Q8. Lifecycle routing for the new gestures?**
**A.** Replace `Attach(…, entry, hooks…)`'s single entry with `Entries{Session, Download, Upload}`. New bus commands `ritual.DownloadRequested` / `ritual.UploadRequested` route to `startDownload` / `startUpload`, both via a shared `startWith(ctx, entry, runHooks)` with `runHooks=false` (sessionHooks bind the livesync dispatcher, which only ticks during `Running`). Mutual exclusion is free — all flows share the controller's single `status` field, so any gesture is rejected while another is `Running`.

**Q-arch. Separate machines or derive from the session graph?**
**A. (A) Separate builders, shared nodes.** `Build` / `BuildDownload` / `BuildUpload` each wire the *same stage constructors* with different edges. Three graphs, one node set, zero runtime branching, explicit per-flow failure routing. Rejected (B) edge-surgery on one mutable session graph: couples three topologies and makes any one unreadable in isolation. (Same call as 021.)

**Q-UI. How prominent is this in the GUI?**
**A. Not prominent at all.** The session **already pulls before it runs**, so "get latest" is the default path — a primary Download button would duplicate the normal flow and fight the one-dial metaphor (007). Therefore:
- **Staleness = a quiet caption, not a control.** IDLE-only, when behind: the dial's muted `sub` slot (009/017) reads *"Remote is newer."* Nothing to click. It self-resolves the moment you press the dial (pull-before-run).
- **Deliberate gestures live in the collapsed Advanced disclosure** (014 — non-obvious by construction): Download = "refresh without playing", Upload = "publish without playing". Rare, hidden, non-interrupting. **No obvious/primary Download affordance anywhere.**

**Q-phase. Phase buckets (017)?**
**A.** Reuse the shared `onStateChanged` map, keyed on stage name. Add `StageProbing → preparing` (alongside Acquiring). Download: `Pulling → downloading`, `ApplyStartedInfo → preparing`, tail `Retaining → saving` (sub-second cosmetic wart — OQ1). Upload: `Probing/Acquiring → preparing`, `Committing → wrapping`, `Pushing/Retaining/Unlocking → saving`. No new `Phase` value.

## Open Questions

**OQ1.** Download's tail `Retaining(local)` flashes the `saving` phase (shared projection can't tell direction by stage name). Sub-second; defer direction-aware projection.

**OQ2 (deferred to a future log).** Full **navigable** history — sequence ids + reconciling with retention (pruning a middle ref loses position; a monotonic seq fixes position but needs cross-client assignment). Out of scope: #1–#3 need only the boolean HEAD compare. Lineage links written by Upload are *best-effort* (a prune can orphan a middle link; nothing reads the chain yet).

**OQ3.** Offline at launch → show nothing. Confirmed default.

**OQ-UI.** Exact home of the staleness caption — dial `sub` on the resting IDLE screen, or quieter still (only inside the opened Advanced disclosure). Pin during the Storybook phase.

## Design

### Chains side-by-side

```
Session :  Checking → Pulling → Acquiring → Running → Draining → Committing → Pushing → Retain(L) → Retain(R) → Unlock → Done
Download:  Checking → Pulling →                                                          Retain(L)             →        Done
Upload  :  Checking → Probing → Acquiring →                       Committing → Pushing → Retain(L) → Retain(R) → Unlock → Done
                                                                                            └─ on any fail → Failed (dismiss-to-idle, 017)
```

Only `Probing` is new (a head-only `Pulling`); every other box is an existing stage.

### `probing` stage

```go
// internal/core/stages/probing/strategy.go
func (s *Strategy) Run(ctx, rs) (next, error) {
    id, err := s.resolve(ctx)            // pulling.HeadResolver, pointed at remote
    switch {
    case errors.Is(err, pulling.ErrNoHead): rs.ParentRefID = ""   // seed
    case err != nil:                        rs.Err = err; return s.onFail, nil
    default:                                 rs.ParentRefID = id
    }
    return s.onOK, nil
}
func (*Strategy) Name() string { return ritual.StageProbing }
```
`ritual.StageProbing = "Probing"`. No new port — reuses `pulling.HeadResolver`.

### Pipelines (separate builders, shared nodes)

```go
func BuildDownload(d Deps) machine.Strategy[ritual.RunState] {
    failCheck, failPull, failRetL := failed.New(ritual.StageChecking), failed.New(ritual.StagePulling), failed.New(ritual.StageRetainingLocal)
    pruneLocal := retaining.New(d.LocalRetentions, d.Bus, failRetL, nil) // nil onOK ⇒ Done
    pull       := pulling.New(d.Puller, d.Applier, d.HeadResolver, pruneLocal, failPull)
    return checking.New(d.Checks, pull, failCheck)
}

func BuildUpload(d Deps) machine.Strategy[ritual.RunState] {
    failCheck, failProbe, failAcq := failed.New(ritual.StageChecking), failed.New(ritual.StageProbing), failed.New(ritual.StageAcquiring)
    failCommit, failPush := failed.New(ritual.StageCommitting), failed.New(ritual.StagePushing)
    failRetL, failRetR   := failed.New(ritual.StageRetainingLocal), failed.New(ritual.StageRetainingRemote)
    unlock      := unlocking.New(d.ReleaseFn, nil)
    pruneRemote := retaining.New(d.RemoteRetentions, d.Bus, failRetR, unlock)
    push        := pushing.New(d.Pusher, pruneRemote, failPush); push.OnStop(unlock)
    pruneLocal  := retaining.New(d.LocalRetentions, d.Bus, failRetL, push)
    commit      := committing.New(d.Committer, d.CommitOpts, pruneLocal, failCommit)
    acquire     := acquiring.New(d.AcquireFn, d.InspectFn, d.HeartbeatInterval, commit, failAcq)
    probe       := probing.New(d.HeadResolver, acquire, failProbe)
    return checking.New(d.Checks, probe, failCheck)
}
```
`Deps` unchanged — both reuse the session's ports.

### Lifecycle: `Entries` + `startWith`

```go
type Entries struct{ Session, Download, Upload machine.Strategy[ritual.RunState] }
func Attach(parent context.Context, bus ports.EventBus, e Entries, sessionHooks ...SessionHook) func()
// consumer switch:
case ritual.StartRequested:    c.startWith(ctx, c.entries.Session,  true)
case ritual.DownloadRequested: c.startWith(ctx, c.entries.Download, false)
case ritual.UploadRequested:   c.startWith(ctx, c.entries.Upload,   false)
// startWith rejects a nil entry and rejects while status==Running.
```

### Control surface

```go
func (c *ControlService) Download() { c.bus.Publish(ritual.DownloadRequested{}) }
func (c *ControlService) Upload()   { c.bus.Publish(ritual.UploadRequested{}) }

type SyncStatus struct { Behind bool `json:"behind"`; LocalHead, RemoteHead string `json:"localHead","remoteHead"` }
func (c *ControlService) GetSyncStatus() SyncStatus   // resolves local+remote HEAD, compares; error ⇒ Behind:false
```
`GetSyncStatus` needs a `SyncProber` injected at composition (two `HeadResolver`s over local + remote stores). No port/memory validation on Download/Upload — neither launches a server.

### GUI (built in Storybook first — see Plan)

Resting IDLE screen: unchanged one-dial, **plus** a muted `sub` line *"Remote is newer"* only when `behind`. The collapsed Advanced disclosure gains a `Download` and an `Upload` row (each with a `<rune-sheet>` confirm), below port/memory, behind an `<hr>`. `prep-settings` emits `download`/`upload`; `ritual-app` calls `wails-api`. No primary button.

## Implementation Plan

**Logic first (this pass), then UI in Storybook.**

Phase L1 — **Stage + commands + pipelines** (~70 LOC + tests)
- `ritual.StageProbing`; `internal/core/stages/probing/` (+ test: head→Parent, ErrNoHead→seed, error→Fail, cancelled→Fail).
- `ritual.DownloadRequested` / `UploadRequested`.
- `pipeline.BuildDownload` / `BuildUpload` (+ chain-shape + failure-row tests).

Phase L2 — **Lifecycle routing** (~40 LOC + tests)
- `Entries`; `Attach` signature; `startWith`; route two commands.
- Update 1 prod + 4 test callers → `Entries{Session: entry}`.
- Tests: right pipeline per gesture; mutual-exclusion reject while Running.

Phase L3 — **Control + staleness query + composition root** (~50 LOC + tests)
- `Download()` / `Upload()`; `GetSyncStatus()` + `SyncProber`.
- `cmd/gui/main.go`: build all three pipelines → `Entries{…}`; wire `SyncProber` (local+remote HeadResolvers).
- Projection: `StageProbing → preparing`.

Phase L4 — **Integration** (tests)
- `Upload_Seeding_EmptyRemote_WritesRootRef` (Parent="").
- `Upload_PopulatedRemote_ParentsOnHead` (B.Parent == seeded A; B newest ⇒ HEAD).
- `Upload_LockHeldByOther_FailsAcquiring`.
- `Download_PullsHeadAndSweepsLocal_NoLock` (workdir==HEAD, local retention ran, remote untouched, no lock taken).
- `Download_EmptyRemote_NoOp`.
- `GetSyncStatus_BehindWhenRemoteNewer` / `_UpToDateWhenEqual`.

Phase U1 — **UI in Storybook** (later pass)
- `prep-settings` Download/Upload rows + confirm; staleness `sub` caption story; `ritual-app` wiring; `wails-api` `download()`/`upload()`/`getSyncStatus()`; regen bindings. Pin OQ-UI.

## Examples

✅ Download has no lock and no push; Upload parents via the existing resolver because Probing set `rs.ParentRefID`.
❌ Don't resolve HEAD inside `CommitOptsResolver` — no ctx, no error path (Q1).
❌ Don't fold the two directions into one chain — Apply prunes (Q2).
❌ Don't run `sessionHooks` for Download/Upload — dead livesync coupling (Q8).
❌ Don't add a primary Download button — duplicates the session's pull, fights one-dial (Q-UI).

## Trade-offs

| Decision | Cost | Benefit |
|----------|------|---------|
| Two directional flows (not one Sync) | Two confirms, two pipelines | Honest; no silent local-edit loss |
| Upload `Parent = remote HEAD` via `probing` | One head-resolve round-trip + a thin stage | Lineage for a future history feature; clean error routing |
| Boolean staleness (no count) | Can't say "behind by N" | Zero model change; defers seq-id/retention problem (OQ2) |
| Quiet `sub` caption, gestures in Advanced | Low discoverability (intentional) | Preserves one-dial; "get latest" stays the default path |
| Download takes no lock | Can read a torn HEAD set mid-push | Doesn't block players |
| `Entries` struct | Touches 5 `Attach` callers | Readable routing; variadic hooks stay last |

## Verification

1. **Download, populated remote** → workdir matches HEAD; local ref exists; **no lock acquired**; remote unchanged; local retention ran.
2. **Download, empty remote** (`ErrNoHead`) → clean Done; nothing written.
3. **Upload, empty remote** → one ref, `Parent=""`; objects uploaded; lock released.
4. **Upload, populated remote** (HEAD `A`) → new ref `B`, `B.Parent==A`, `B` newest ⇒ HEAD; local+remote retention ran; lock released.
5. **Upload, lock held** → Failed(Acquiring); no commit; no push; dismiss-to-idle.
6. **Mutual exclusion** → during a Running session both buttons gated (idle-only render) *and* lifecycle rejects stray `Download/UploadRequested`.
7. **`GetSyncStatus`** → `Behind=true` iff remoteHEAD > localHEAD; offline ⇒ `Behind=false`, no throw.
8. `go test ./internal/core/stages/probing/... ./internal/subsystems/pipeline/... -race` clean.
9. `go test ./internal/integration/... -race -timeout 60s` clean.

## Implementation Results — Logic (2026-05-30)

Backend Phases L1–L4 shipped; UI Phase U1 deferred (logic-first per the user). Full suite `go test ./... -race` green (exit 0).

**Landed**
- L1: `ritual.StageProbing`; `internal/core/stages/probing/` (4 tests). `ritual.DownloadRequested` / `UploadRequested`. `pipeline.BuildDownload` / `BuildUpload`.
- L2: `lifecycle.Entries{Session,Download,Upload}`; `Attach` signature change; `startWith(ctx, entry, runHooks)`; routes the two commands; 5 `Attach` callers updated. Routing + mutual-exclusion covered by `lifecycle/routing_test.go` (4 tests, fake strategies).
- L3: `control.Download()` / `Upload()` / `GetSyncStatus()`; projection `StageProbing → preparing`; `cmd/gui/main.go` builds all three pipelines into `Entries` and wires the prober.
- L4: `internal/integration/bidirectional_sync_integration_test.go` (5 tests — Upload seed/parent-on-HEAD/lock-held, Download pull-no-lock/empty-noop).

**Deviations from the plan**
1. **Staleness prober extracted to `control.NewHeadSyncProber(localHead, remoteHead)`** (a tested seam: `control/control_test.go`, 9 tests) instead of an inline closure in `main.go`. Keeps `main.go` thin and makes the local/remote-HEAD compare + `ErrNoHead`-as-empty + error-propagation directly testable. `main.go` now calls the constructor.
2. **Mutual exclusion verified at the lifecycle level** (fake blocking strategy) rather than a full-session integration test — the integration harness doesn't run a session concurrently with sync flows wired, and the status-guard logic is what's under test.
3. **No `pipeline_test.go`** — the package has no unit test by existing convention; both new builders are exercised end-to-end by the L4 integration tests.

**Test tally:** probing 4 · control 9 · lifecycle routing 4 · integration (new) 5 = 22 new, all `-race` clean. Verification criteria 1–9 met (criterion 6's idle-only render half lands in U1).

## Implementation Results — UI / Phase U1 (2026-05-30)

Storybook-first per the frontend conventions. Frontend `npm run build:dev` (tsc + vite) clean; `web-test-runner` 44/44 green.

**Landed**
- Wails bindings regenerated (`task gui:bindings`): `Download` / `Upload` / `GetSyncStatus` + `SyncStatus`. `wails-api.ts` exports `download()` / `upload()` / `getSyncStatus()` + `SyncStatus`.
- `prep-settings.ts`: Download + Upload rows below the port/memory grid (`<hr>` separator), each opens a `<rune-sheet>` confirm with the §Q8 copy. Emits a `sync` event on confirm only. Story `SyncGestures` added; `prep-settings.test.ts` (5 tests — opens-without-firing, confirm fires download/upload, cancel no-op).
- `ritual-app.ts`: `@sync` → `download()`/`upload()` (same `onView` stream animates the dial); launch `getSyncStatus()` → `behind` `@state` → IDLE dial `sub` caption *"Remote is newer"* (degrades to "" offline).
- Projection wart from L3 (Download tail flashes `saving`) left as designed — OQ1 deferred.
- **Storybook harness** (`.storybook/preview.ts` mock transport): added `Download` / `Upload` / `GetSyncStatus` method-ID cases so the **Screens / Ritual** story animates the sync flows without a backend (Download → `downloading → preparing → idle`; Upload → `preparing → wrapping → saving → idle`; `GetSyncStatus` fixture returns `behind:true` to surface the IDLE caption). The **Components / Prep Settings** story has no dial — it only logs the `sync` event, by design.

**Deviations from the plan**
1. **`prep-settings` emits one `sync` event with `{direction}`** instead of two `download`/`upload` events. One listener in `ritual-app`, tidier; the prep-settings layer stays presentational (no `wails-api` import) as the conventions require.
2. **One `<rune-sheet>` whose content swaps by `_confirming` direction**, not two sheets — less DOM, same confirm UX.
3. **`rune-button` emits `press`, not native click** — the gesture test dispatches `press` (clicking the host is a no-op).

**OQ-UI resolved:** staleness caption lives on the **resting IDLE dial `sub`** (`PHASE_VIEW[PhaseIdle].sub` reads `ctx.behind`), not inside the Advanced disclosure. Quietest HIG-aligned spot; self-resolves on play.

**Operational note:** the buttons call newly-bound Go methods — a running dev app must be **restarted** (Go recompile) to expose them; a vite hot-reload alone won't.

**Status:** all phases (L1–L4 + U1) shipped. Verification criteria 1–9 met end-to-end (criterion 6's idle-only-render half now lives in `ritual-app`'s `dial.state === "idle"` guard). Ready to flip to Implemented after a live smoke test.

## Design addendum — direction-aware dial (2026-05-30) · resolves OQ1

**Problem (the full wart).** The projection's `onStateChanged` keys phase purely on stage *name*, so the sync flows inherit session-centric beats. Tracing the real chains through the switch:

- **Download** (`Checking → Pulling → Retaining`): `Checking/Pulling → downloading`, `ApplyStartedInfo → preparing`, then **`Retaining → saving`** — the dial ends on the ⬆ upload glyph + "Saving" while it's only pruning local refs.
- **Upload** (`Checking → Probing → … → Unlocking`): **`Checking → downloading`** — the dial *starts* on the ⬇ download glyph while nothing downloads; `Committing → wrapping` ("Spinning down") also borrows server-shutdown copy.

Both halves are the same root cause: a shared name-keyed map can't tell which flow is running.

**Decision.** Each sync flow renders as **one honest phase** — Download → `PhaseDownloading` throughout, Upload → `PhaseSaving` throughout. These two phases already carry direction-neutral copy ("Downloading" / "Saving") and the right glyph; the server-only beats (`Preparing`'s "Spinning up / Almost live", `Wrapping`'s "Spinning down") are simply never entered by a sync flow. **Zero frontend change** — the existing `PHASE_VIEW` for those two phases is already correct.

**Mechanism.**
- `ritual.Flow` (`FlowSession` / `FlowDownload` / `FlowUpload`) + `ritual.FlowStartedInfo{Flow}`.
- `lifecycle.startWith` gains a `flow` arg and publishes `FlowStartedInfo` before driving the runner (each route passes its flow).
- Projection tracks `activeFlow` (folded from `FlowStartedInfo`); `onStateChanged` short-circuits to the single phase for Download/Upload and falls through to the existing session switch otherwise. `ApplyStartedInfo`'s download→preparing flip is guarded to `FlowSession` (a Download must not flip to the brain-cog). `activeFlow` resets to `FlowSession` on the idle/done/dismissed reset.
- The progress ring still fills from Ticks during the transfer stage (Pulling for Download, Pushing for Upload); the brief non-transfer stages sit at 0/100% under the same honest glyph.

**Result.** Download = ⬇ "Downloading" → Idle. Upload = ⬆ "Saving" → Idle. No direction lie, no server copy. Storybook fixtures simplified to match (single-phase ramps).

**Follow-up fix — ETA placeholder jitter (2026-05-30).** Collapsing the flows to one transfer phase exposed a latent `etaSub` bug: it returned the `·····` decoder placeholder whenever `etaSeconds <= 0`, which the dial decodes *fast* (50–120ms). With Upload now spanning its non-byte stages (Checking/Probing/Acquiring/**Committing**) under `PhaseSaving`, that placeholder jittered for the whole pre-push span ("decoding all the time"). Fixed `etaSub` (`ritual-app.ts`) to gate on the plan: `bytesTotal <= 0 → ""` (no sub, no decode — non-transfer beat), `bytesDone >= bytesTotal → "Almost done"` (calm save-tail), `etaSeconds <= 0 → placeholder` (only the genuine first-tick grace, design-log/009 §Q5), else `MM:SS`. `PhaseSaving.sub` simplified to plain `etaSub`; `ritual-dial.renderSub` now renders empty/whitespace subs plain (never hands `""` to the decoder). Storybook fixtures gained a real `etaSeconds` countdown so the mock shows plain digits, not the placeholder. Net: digits stay plain (the "safe regex" holds), and the decoder only runs on the brief grace tick, the calm "Almost done"/idle captions — never continuously.
