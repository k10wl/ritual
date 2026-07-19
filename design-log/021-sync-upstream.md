# 021 — Sync Upstream (force-push local server state)

**Date:** 2026-05-25
**Status:** Draft
**Related:** [[016-live-sync-resurrected]] (commit/push substrate, drain barrier), [[014-prep-advanced-settings]] (IDLE Advanced disclosure host), [[017-stage-bucket-honesty]] (Phase taxonomy — new flow rides `saving` bucket).

## Background

`ritual` today has one happy-path producer of refs: play a session, server stops, `committing.Strategy` writes the new ref, `pushing.Strategy` uploads. `livesync` (016) adds tick-amends inside Running. Both presuppose a Running stage — i.e. the server actually started.

Three real workflows have no producer:

1. **Seeding** — empty remote, user drops a worlds tree into the configured dir and wants ref #1 written without launching the server.
2. **Server-binary upgrade** — user replaces jars / configs externally, wants a new ref so teammates inherit the upgrade on next pull, but does not need to play.
3. **Forced world snapshot** — user edited the world via an external tool (MCEdit, WorldEdit CLI, restore-from-backup) and wants those bytes to be authoritative remote-side, no merge.

All three share a shape: *take local files as truth, write a new ref, push, done.* No Pulling (would overwrite the user's intent with stale remote), no Running (no server lifecycle), no save-handshake.

## Problem

Substrate exists (Committer, Pusher, Retainers, Acquirer/Releaser, HeadResolver). Producer does not. Users today have to either:

- Launch the server, immediately stop it (slow, requires a working server, ticks save-all-flush over already-good files) — works for case 3 only.
- Hand-edit the storage layer outside the app — bypasses lock, retention, and ref-graph invariants.

Neither is acceptable. Need an explicit, single-click flow that reuses the existing stage primitives and respects lock + retention.

## Questions and Answers

**Q1.** Pull first, or treat local as authoritative?
**A.** Local-authoritative. No Pulling stage in the chain. Whole point of the feature is "my bytes win"; pulling would either clobber the user's intent (full pull) or be a no-op (we only need the head ID, which `HeadResolver` already gives us object-free).

**Q2.** How is the new ref's `Parent` determined without Pulling?
**A.** Reuse `pulling.HeadResolver` as a standalone head-probe stage (`probing.Strategy`). It reads the remote ref index and returns the current HEAD `RefID` without downloading objects. Empty result (`ErrNoHead`) ⇒ seeding mode ⇒ `Parent = ""`. Non-empty ⇒ `Parent = remote HEAD`. Linear history; no merge.

**Q3.** Fresh commit or Amend?
**A.** Always Fresh. Amend semantics are for "this session's draft collapses into one final ref." Sync-upstream is not a session — there is no prior draft to collapse. `CommitOpts{Amend: "", Parent: <remoteHead>, Targets: …}`.

**Q4.** Lock acquired?
**A.** Yes — Acquiring stage runs. Prevents two clients from racing a force-push. Same heartbeat semantics; just no Running between Acquiring and Committing.

**Q5.** GUI surface?
**A.** IDLE-only, inside the existing Advanced disclosure (014). Secondary button labelled `Sync upstream` with a confirmation dialog: "Overwrite remote with local files? Remote HEAD will become Parent of a new ref." Cancel default. Disabled while any other stage is active.

**Q6.** Commit targets?
**A.** Same `CommitOpts.Targets` as a normal session. Reuses the existing convention (worlds-only today; if config grows to include jars/mods, sync-upstream picks that up for free). No per-invocation target picker in v1.

**Q7.** Retention?
**A.** Yes — both `LocalRetentions` and `RemoteRetentions` run after Pushing. Otherwise a long sequence of sync-upstream presses would balloon ref count. Same chain tail as a normal session.

**Q8.** What about `livesync.Drainable` — does Draining need to run?
**A.** No. Sync-upstream chain never enters Running, so no livesync tick can be in flight. Draining stage is omitted from this chain (parallel to "no `Drainable` ⇒ pipeline skips Draining" behaviour at `pipeline.Build`).

**Q9.** What about the GUI Phase bucket (017)?
**A.** Reuses existing buckets:

| Stage          | Phase (017)                                 |
|----------------|---------------------------------------------|
| Checking       | `preparing` (brain-cog)                     |
| Probing        | `preparing`                                 |
| Acquiring      | `preparing`                                 |
| Committing     | `saving`                                    |
| Pushing        | `saving`                                    |
| Retaining      | `saving`                                    |
| Unlocking      | `wrapping`                                  |
| Done           | `idle` (with success toast — see OQ2)       |

No new Phase value. Dial visuals already reasonable for these stages.

**Q10.** Confirmation dialog content?
**A.** Two lines max:
- Headline: `Sync local files to remote?`
- Body: `Local worlds will be committed as a new ref. Current remote HEAD becomes the parent. This cannot be undone from inside the app.`
- Buttons: `Cancel` (default), `Sync upstream`.

## Open Questions

**OQ1.** Should sync-upstream refuse to run when the remote HEAD is *newer than what we last pulled* (i.e. a teammate pushed since our last session)?

Rationale: a true "force push" risks burying a teammate's recent ref under our local snapshot. Linear parent semantics survive (their ref is our parent), but the user may not know they're about to bury work.

Proposed default: surface in the confirmation dialog. If we can cheaply detect `remoteHead != localKnownHead` (e.g. compare `HeadResolver.Resolve()` to a stored "last-pulled-head" in config/state), append a third line: `⚠ Remote has changes since your last pull. Your sync will sit on top of them.` Still allow Cancel/Sync.

If we cannot cheaply detect (no stored last-pulled-head), skip the warning in v1, file follow-up.

**OQ2.** Post-success GUI affordance — toast, dial-overlay, or silent return to IDLE?

`017` lifts `dismiss-to-idle` for Failed. Success → IDLE happens silently today. Sync-upstream is a deliberate user action; some feedback ("Pushed ref `abc123…`") seems warranted. Match existing patterns or add a new one?

**OQ3.** Does sync-upstream need its own typed error class for the "lock held by another client" case, distinct from a normal session's Acquiring failure?

Acquiring already publishes structured errors; user-facing copy could differ between "another client is playing" and "another client is syncing." Probably out of scope for v1 — Acquiring's existing message is fine.

**OQ4.** Should the button also surface in the Failed dial (017's dismiss-to-idle screen) as a recovery option? E.g. "session crashed mid-Commit → click Sync upstream to retry with current disk state." Out of scope; revisit if telemetry shows demand.

**OQ5.** Naming — `Sync upstream` vs `Push local` vs `Force snapshot` vs `Upload state`?

`Sync upstream` reads naturally to git-fluent users but conflates "push" with "sync" (which usually implies bidirectional). `Push local` is precise but Minecraft users may not parse it. `Force snapshot` over-promises destructiveness. Lean `Sync upstream` for now — collect feedback in implementation review.

## Design

### Chain shape

```
Checking → Probing → Acquiring → Committing → Pushing → Retaining(local) → Retaining(remote) → Unlocking → Done
                                                                                                  └─ on any fail → Failed (dismiss-to-idle, 017)
```

Side-by-side with the normal session chain:

```
Session :  Checking → Pulling   → Acquiring → Running → Draining → Committing → Pushing → Retain → Unlock → Done
Upstream:  Checking → Probing   → Acquiring →                      Committing → Pushing → Retain → Unlock → Done
```

Only deltas: Pulling → Probing (head-only, no object download), Running + Draining removed.

### New stage: `probing`

```go
// internal/core/stages/probing/strategy.go
type Strategy struct {
    resolve pulling.HeadResolver
    onOK    machine.Strategy[ritual.RunState]
    onFail  machine.Strategy[ritual.RunState]
}

func (s *Strategy) Name() string { return ritual.StageProbing }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) machine.Strategy[ritual.RunState] {
    id, err := s.resolve(ctx)
    switch {
    case errors.Is(err, pulling.ErrNoHead):
        rs.ParentRefID = ""              // seeding
    case err != nil:
        rs.FailedStage = ritual.StageProbing
        rs.Err = err
        return s.onFail
    default:
        rs.ParentRefID = id
    }
    return s.onOK
}
```

`ritual.StageProbing = "Probing"` added to `stages.go`.

### CommitOpts resolver: extend for upstream mode

`NewCommitOptsResolver` today branches `rs.RefID != "" ⇒ Amend` vs `else ⇒ fresh from rs.ParentRefID`. Sync-upstream uses the existing else-branch unchanged: `rs.RefID` stays `""` (never set — no livesync, no prior draft), `rs.ParentRefID` is what Probing wrote.

**No resolver change required.** This is the design's load-bearing reuse: the existing resolver is already correct for "fresh commit with explicit parent."

### Wiring: separate pipeline `Build`?

Two options:

- **Option A.** Second public `Build` in `pipeline` package: `BuildUpstream(d Deps) machine.Strategy[…]`. Skips Pulling/Running/Draining, swaps Pulling for Probing. Independent from `Build`. ~25 LOC.
- **Option B.** Flag on `Deps` (`Deps.Mode = ModeSession | ModeUpstream`), single `Build` branches internally. ~15 LOC but conflates two unrelated topologies in one reader's eye.

**Decision.** Option A. Two functions, two readable chains, zero conditional branching at runtime. Composition root picks which one to call based on which command the user issued.

### Composition root: dispatch the user gesture

Today `lifecycle.Attach` builds one pipeline once and starts it on each `start()`. With sync-upstream, lifecycle owns *two* pipelines and routes the user's gesture to the right one. Sketch:

```go
sessionPipeline := pipeline.Build(deps)
upstreamPipeline := pipeline.BuildUpstream(deps)

lifecycle.Attach(bus, lifecycle.Routes{
    Start:         sessionPipeline,   // existing "Start session" button
    SyncUpstream:  upstreamPipeline,  // new IDLE Advanced button
})
```

Lifecycle's `start()` signature gains a route selector (or two methods: `StartSession` / `StartUpstream`). Exact API pinned in Phase 2.

### GUI

`prep-settings.ts` (the `<details>` Advanced disclosure from 014) gains a `<button>Sync upstream</button>` row near the bottom, separated from the port/memory inputs by a `<hr>`. Click opens a `<rune-dialog>` confirmation (new primitive, or reuse `<rune-sheet>` modal pattern if one already exists — pin in design system review).

On confirm: Wails call `RitualAPI.StartUpstream()` (new method, mirrors existing `Start`). Disabled while `phase !== 'idle'`.

```
┌─ Advanced ─────────────────────────────────┐
│  Port:    25565                            │
│  Memory:  4096 MB                          │
│  ─────────────────────────────────────     │
│  [ Sync upstream ]   ← new                 │
│  Pushes your local worlds as a new ref.    │
└────────────────────────────────────────────┘
```

Helper text below the button is one-line, dim. No emoji.

### Failure modes

Same failed.* terminals as session, attributed to the failed stage:

| Failure                       | Terminal                           |
|-------------------------------|------------------------------------|
| Checking (preflight)          | `failed.New(StageChecking)`        |
| Probing (network / remote)    | `failed.New(StageProbing)` (new)   |
| Acquiring (lock held)         | `failed.New(StageAcquiring)`       |
| Committing (disk / serialize) | `failed.New(StageCommitting)`      |
| Pushing (network)             | `failed.New(StagePushing)`         |
| Retaining (local)             | `failed.New(StageRetainingLocal)`  |
| Retaining (remote)            | `failed.New(StageRetainingRemote)` |

All route to dismiss-to-idle per 017. Unlock-on-stop wired same way as session (`push.OnStop(unlock)`).

## Implementation Plan

Phase 1 — **Probing stage** (~50 LOC + ~80 tests)
- `internal/core/stages/probing/strategy.go` + `strategy_test.go`.
- `ritual.StageProbing` constant.
- Stories: head present → `rs.ParentRefID = id`, `ErrNoHead` → empty parent + onOK, transport error → onFail with `rs.FailedStage` set.

Phase 2 — **`pipeline.BuildUpstream`** (~30 LOC + ~120 tests)
- New `BuildUpstream(d Deps)` in `internal/subsystems/pipeline/pipeline.go`.
- Chain: Checking → Probing → Acquiring → Committing → Pushing → Retain(local) → Retain(remote) → Unlocking → Done. `failed.*` terminals identical to session shape, plus `failed.New(StageProbing)`.
- Tests: end-to-end with fake ports — seeding (empty remote), update (non-empty remote), each failure-mode row.

Phase 3 — **Lifecycle routing + Wails surface** (~40 LOC + ~80 tests)
- Lifecycle: `Routes{Start, SyncUpstream}` (or twin methods), wires both pipelines. Single `*RunState` per gesture; `SessionHook` unchanged.
- Wails: `RitualAPI.StartUpstream()` mirrors `Start()`. Same locking against concurrent gestures.
- Tests: lifecycle picks the right pipeline; concurrent `Start` + `StartUpstream` rejected.

Phase 4 — **GUI: button + confirmation** (~60 LOC + ~40 tests)
- `prep-settings.ts` adds the button + helper text below port/memory rows.
- New `<rune-confirm>` primitive (or reuse — pin in implementation), two-button dialog.
- Disabled when `phase !== 'idle'`.
- Stories: button visible IDLE-only, dialog cancel = no-op, confirm = wails call fires, button disables during upstream run, re-enables on `idle`.

Phase 5 — **Integration story** (~30 LOC + ~150 tests)
- `internal/integration/upstream_integration_test.go`.
- `TestIntegration_Upstream_Seeding_EmptyRemote_WritesFirstRef` — remote starts empty, upstream run produces one ref with Parent="".
- `TestIntegration_Upstream_NonEmptyRemote_ParentsOnHead` — seeded remote ref, upstream run produces a new ref whose Parent = seeded ref id.
- `TestIntegration_Upstream_LockHeldByOther_FailsAcquiring` — second client holds lock, upstream returns Failed(Acquiring), no commit, no push.
- `TestIntegration_Upstream_PushFails_NoRefOnRemote_LocalSweptByRetention` — Pusher errs after Commit, Retain(local) still runs, remote unchanged, local sweep keeps disk bounded.

Phase 6 — **OQ1 warning (conditional on cheap detection)** (~20 LOC + ~40 tests)
- If `lastPulledHead` is already persisted somewhere reachable, surface the "remote moved since your last pull" line in the dialog.
- Otherwise file a follow-up and skip.

Total estimate: ~230 LOC production + ~510 LOC tests across phases 1–5 (Phase 6 conditional).

## Examples

✅ **Good — chain composition reads top-down:**

```go
func BuildUpstream(d Deps) machine.Strategy[ritual.RunState] {
    failCheck   := failed.New(ritual.StageChecking)
    failProbe   := failed.New(ritual.StageProbing)
    failAcq     := failed.New(ritual.StageAcquiring)
    failCommit  := failed.New(ritual.StageCommitting)
    failPush    := failed.New(ritual.StagePushing)
    failRetL    := failed.New(ritual.StageRetainingLocal)
    failRetR    := failed.New(ritual.StageRetainingRemote)

    unlock      := unlocking.New(d.ReleaseFn, nil)
    pruneRemote := retaining.New(d.RemoteRetentions, d.Bus, failRetR, unlock)
    push        := pushing.New(d.Pusher, pruneRemote, failPush)
    push.OnStop(unlock)
    pruneLocal  := retaining.New(d.LocalRetentions, d.Bus, failRetL, push)
    commit      := committing.New(d.Committer, d.CommitOpts, pruneLocal, failCommit)
    acquire     := acquiring.New(d.AcquireFn, d.InspectFn, d.HeartbeatInterval, commit, failAcq)
    probe       := probing.New(d.HeadResolver, acquire, failProbe)
    check       := checking.New(d.Checks, probe, failCheck)

    return check
}
```

✅ **Good — Probing reuses `pulling.HeadResolver`:**

```go
probe := probing.New(d.HeadResolver, acquire, failProbe)
```

No new port. Same callable that Pulling uses.

❌ **Bad — branching the existing Build:**

```go
// DON'T: conflates two chains, hides the topology
func Build(d Deps) … {
    if d.Mode == ModeUpstream { skipPull = true; skipRun = true; … }
}
```

❌ **Bad — Committer learning about "upstream mode":**

```go
// DON'T: resolver is already correct; do not add a Mode field
type CommitOpts struct { … Mode UpstreamMode }
```

Existing `rs.RefID == "" && rs.ParentRefID != ""` shape is enough.

❌ **Bad — pulling first "just to be safe":**

```go
// DON'T: defeats the feature; clobbers user's local intent.
chain: Checking → Pulling → Probing → Acquiring → Committing → …
```

## Trade-offs

| Decision | Cost | Benefit |
|----------|------|---------|
| Local-authoritative (no Pulling) | Risk of accidental teammate-clobber | Matches the three real use cases; warning surfaced via OQ1 |
| Separate `BuildUpstream` vs. flagged `Build` | One more public function | Each chain reads top-to-bottom with zero runtime branching |
| Probing reuses `pulling.HeadResolver` | None — already exists | No new port; one less surface to mock |
| Always-Fresh commit (no Amend) | None — no prior draft to amend | Resolver unchanged; simplest mental model |
| Lock acquired same as session | Slightly slower than skipping Acquiring | Prevents two-client races; symmetric with the rest of the app |
| Phase taxonomy reuses `saving` bucket (017) | None — already exists | GUI dial visuals correct for free |
| IDLE-only, behind Advanced disclosure | Discoverability cost (intentional) | Power-user gesture; matches 014 placement |
| Two-button confirmation dialog | One extra click | Force-push is destructive in spirit; default Cancel prevents accidents |

## Verification

A correct implementation:

1. **Seeding (empty remote).** Empty R2 bucket, local worlds populated, user clicks Sync upstream → confirmation → confirm. End state: remote has exactly one ref with `Parent = ""`; objects uploaded; lock released.
2. **Update (non-empty remote).** Remote HEAD = ref `A`. User clicks Sync upstream → confirm. End state: remote has refs `{A, B}`; `B.Parent = A`; B's objects reflect current local worlds; lock released.
3. **Lock contention.** Second client holds the lock. User clicks Sync upstream. Failed(Acquiring); no commit; no push; no ref written; dial shows Failed → dismiss-to-idle (017).
4. **Push failure.** Pusher stubbed to fail. Commit succeeds locally; Push fails; Retain(local) still runs (sweeps the orphan draft per existing retention semantics); Unlock runs; remote unchanged. Dial shows Failed(Pushing) → dismiss-to-idle.
5. **GUI gate.** During an active session (`phase != 'idle'`), Sync upstream button is disabled. After Done/Failed → IDLE, re-enabled.
6. **Confirmation cancel.** Click button → dialog opens → Cancel → no Wails call fired, no stage change.
7. **`go test ./internal/core/stages/probing/... -race` clean.**
8. **`go test ./internal/subsystems/pipeline/... -race` clean (covers `BuildUpstream`).**
9. **`go test ./internal/integration/... -race -timeout 60s` clean (covers Phase 5 stories).**
