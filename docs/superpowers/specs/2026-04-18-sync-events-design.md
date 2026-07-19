# Sync Events & Sync State Machine

## Problem

`SyncService.Upload`/`Download` are inline procedures that emit only `StartInfo`/`UpdateInfo`/`FinishInfo` keyed by `"sync-<prefix>"`. No staging events, no commit events, no plan event, no per-phase failures, no per-call storage observability. Operators cannot tell what the app did. GUI work cannot subscribe to a meaningful event stream.

Stories #4 (UUIDv4 staging), #10 (commit must complete/override), #13 (archiving saves upstream), #14 (sync observability) all sit on the same code surface and currently each demand independent edits to `internal/core/services/sync.go`.

## Solution Overview

Three coordinated changes:

1. **Lift sync into a state machine** — `internal/core/sync/` package mirroring the `internal/core/stages/*` pattern. Each phase becomes its own `machine.Strategy[SyncRunState]`. Ordering becomes structural.
2. **Direction-neutral typed event family** — `Sync*Info` types (~17), embedded payload struct, `Source`/`Destination` derived from `fmt.Sprint(store)`.
3. **Storage decorator** — `internal/adapters/observed/storage.go` wraps any `StorageRepository`, publishes `Storage*Info` per call. Composition root wraps every store.

Engine is DRY: `sync(src, dst)` parameterized by stores. Direction is wiring (`upload := NewMachine(local, remote, bus)`, `download := NewMachine(remote, local, bus)`). Same engine supports `sync(local, local)` for tests.

## Architecture

```
                         ┌──────────────────────────────────┐
                         │  ports.EventBus  (one instance)  │
                         └──────────────┬───────────────────┘
                                        │
            ┌───────────────────────────┼───────────────────────────┐
            │                           │                           │
   Engine layer                  Storage decorator           Existing run state machine
   (Sync*Info events,            (Storage*Info events,       (StateChanged*, lock,
    structural ordering)          one per adapter call)       readiness — unchanged)

   internal/core/sync/           internal/adapters/observed/  internal/core/stages/
```

Two event families on one bus. Subscribers filter by type. Bus contract unchanged (non-blocking, fan-out).

## Components

### `internal/core/sync/`

```
sync/
  engine.go            Machine[SyncRunState] builder
  runstate.go          SyncRunState struct
  events.go            ~17 typed events with embedded payload
  scanning/strategy.go scan src → SrcMap; load dst manifest → DstMap
  planning/strategy.go diff, totals, plan events; short-circuits to Done if empty
  stagedirinit/        UUIDv4 staging dir/prefix
  staging/             per-file src.Get → dst.Put(staging key)
  committing/          per-file dst.Copy(staging → final) + dst.Delete(staging)
  orphancleanup/       dst.Delete(orphan keys batch) — upload only
  ghostcleanup/        per-file dst.Delete — download only
  stagedircleanup/     dst.Delete(stagingPath) — runs on success AND failure
  done/                emit SyncFinishedInfo
  failed/              emit SyncFailedInfo, run staging cleanup, return rs.Err
```

Each strategy: `func (s *Strategy) Run(ctx, *SyncRunState) (machine.Strategy[SyncRunState], error)`. Pattern matches `internal/core/stages/fetching/strategy.go`.

### `internal/adapters/observed/`

```
observed/
  storage.go    observedStorage{inner, bus}
  events.go     7 Storage*Info types
```

```go
func NewStorage(inner ports.StorageRepository, bus ports.EventBus) ports.StorageRepository
```

Each method delegates to `inner`, publishes a single completion event after return:

```go
func (o *observedStorage) Get(ctx context.Context, key string) ([]byte, error) {
    start := time.Now()
    data, err := o.inner.Get(ctx, key)
    o.bus.Publish(StorageGetInfo{
        Store: fmt.Sprint(o.inner), Key: key,
        Bytes: len(data), DurationMs: time.Since(start).Milliseconds(), Err: err,
    })
    return data, err
}
```

`fmt.Sprint(o.inner)` calls inner adapter's `String()` — produces `"fs::./worlds"` etc.

### Port + adapter changes

**`internal/core/ports/ports.go`** — embed `fmt.Stringer`, add `Rename`:

```go
type StorageRepository interface {
    fmt.Stringer
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, data []byte) error
    Copy(ctx context.Context, src, dst string) error
    Rename(ctx context.Context, src, dst string) error
    Delete(ctx context.Context, key string) error
    DeleteBatch(ctx context.Context, keys []string) error
    List(ctx context.Context, prefix string) ([]string, error)
}
```

**`internal/adapters/fs.go`** — add `String() string` returning `"fs::<root>"`, add `Rename`.

**`internal/adapters/r2.go`**:
- Add `String() string` returning `"r2::<bucket>/<prefix>"`.
- Add `Rename` (S3 `CopyObject` + `DeleteObject`).
- **Change `Delete` semantics to tree-delete:** `List(prefix) + DeleteBatch(keys)`. Single-key delete still works (List returns 1 key). Aligns r2 with localfs `RemoveAll` behavior. Returns `"key not found"` error when prefix yields zero keys (matches localfs).

### Manifest schema

**`internal/core/domain/manifest.go`:**

```go
type FileEntry struct {
    Hash string
    Size int64
}

type SyncState struct {
    XXHashMap    map[string]FileEntry  // was map[string]string
    XXHashSyncAt time.Time
}
```

`domain.ComputeDiff` adapts to compare `entry.Hash`. No migration concern (no released `XXHashMap` schema in production).

### Updater alignment

`internal/core/services/updater_ritual.go` replaces direct `os.WriteFile`/`os.ReadFile`/`os.Rename`/`os.Remove` with `StorageRepository` calls (`Put`/`Get`/`Rename`/`Delete`). Receives a `StorageRepository` for the local exe directory. Becomes observable via decorator like everything else.

### Out-of-scope file CRUD

These remain direct (pre-bus or domain-layer concerns); flagged as separate cleanups:

- `internal/subsystems/logging/logging.go` — log file bootstrap, before bus exists.
- `internal/subsystems/sync/kit.go` — wire-time directory prep.
- `internal/core/domain/settings.go` — settings JSON I/O (layer violation; future `SettingsRepository` port).
- All `_test.go` and `internal/testhelpers/` direct file ops — tests need raw FS for setup/teardown.

## Event Taxonomy

### Engine events (`internal/core/sync/events.go`)

Embedded payload struct keeps boilerplate small:

```go
type syncBase struct {
    Source, Destination string  // fmt.Sprint(store) — "fs::./worlds" etc.
    StagingID           string  // UUIDv4, empty until StageDirInit ran
}

// One-shot
type SyncStartedInfo            struct{ syncBase }
type SyncFinishedInfo           struct{ syncBase; Files int; Bytes int64; DurationMs int64 }
type SyncFailedInfo             struct{ syncBase; Phase string; Err string }

// Plan
type SyncPlanInfo               struct{ syncBase
    Adds, Updates, Deletes int
    AddBytes, UpdateBytes, DeleteBytes int64
}
type SyncPlanFileInfo           struct{ syncBase; Path string; Action string; Size int64 }  // Action: add|update|delete

// StageDirInit
type SyncStagingDirCreatedInfo  struct{ syncBase; StagingPath string }

// Staging (per-file ops)
type SyncStageStartedInfo       struct{ syncBase; Files int; Bytes int64 }
type SyncStageProgressInfo      struct{ syncBase; File string; FilesDone, FilesTotal int; BytesDone, BytesTotal int64 }
type SyncStageFinishedInfo      struct{ syncBase; Files int; Bytes int64; DurationMs int64 }
type SyncStageFailedInfo        struct{ syncBase; File string; Err string }

// Committing (per-file ops)
type SyncCommitStartedInfo      struct{ syncBase; Files int; Bytes int64 }
type SyncCommitProgressInfo     struct{ syncBase; File string; FilesDone, FilesTotal int; BytesDone, BytesTotal int64 }
type SyncCommitFinishedInfo     struct{ syncBase; Files int; Bytes int64; DurationMs int64 }
type SyncCommitFailedInfo       struct{ syncBase; File string; Err string }

// OrphanCleanup (batch op)
type SyncOrphanCleanupInfo      struct{ syncBase; Keys []string; Failed []string; DurationMs int64; Err string }

// GhostCleanup (per-file op, download only)
type SyncGhostDeletedInfo       struct{ syncBase; File string }
type SyncGhostCleanupFailedInfo struct{ syncBase; File string; Err string }

// StagingDirCleanup (always runs)
type SyncStagingDirCleanedInfo  struct{ syncBase; StagingPath string; Outcome string; DurationMs int64; Err string }
```

All implement `String() string` returning a greppable single line including `src=...→dst=...`.

### Storage events (`internal/adapters/observed/events.go`)

```go
type StorageGetInfo         struct{ Store, Key string; Bytes int; DurationMs int64; Err error }
type StoragePutInfo         struct{ Store, Key string; Bytes int; DurationMs int64; Err error }
type StorageCopyInfo        struct{ Store, SrcKey, DstKey string; DurationMs int64; Err error }
type StorageRenameInfo      struct{ Store, SrcKey, DstKey string; DurationMs int64; Err error }
type StorageDeleteInfo      struct{ Store, Key string; DurationMs int64; Err error }
type StorageDeleteBatchInfo struct{ Store string; Keys []string; DurationMs int64; Err error }
type StorageListInfo        struct{ Store, Prefix string; Count int; DurationMs int64; Err error }
```

Single completion event per call (no started/finished split — calls are short; per-byte progress for big Puts handled separately if needed).

## Data Flow

### Wiring

```go
// composition root
local  := observed.NewStorage(localfs.NewStorage(localRoot, slog), bus)
remote := observed.NewStorage(r2.NewStorage(...), bus)

upload   := sync.NewMachine(local, remote, bus)   // src=local,  dst=remote
download := sync.NewMachine(remote, local, bus)   // src=remote, dst=local
```

### Engine chain (upload path)

```
Scanning → Planning ─┬──> Done                            (no diff)
                     │
                     └──> StageDirInit → Staging → Committing → OrphanCleanup
                                                                      │
                                                                      ▼
                                                                StagingDirCleanup → Done

Failure from any phase → Failed (always runs StagingDirCleanup before terminating)
```

Download path identical except `OrphanCleanup` slot is `GhostCleanup`.

### Per-strategy responsibilities

**Scanning:** entry strategy. Emits `SyncStartedInfo` first. Then `src.List` (or scanner if local) → build `SrcMap[path]FileEntry{Hash, Size}`. Load dst manifest → `DstMap`. Routes to Planning.

**Planning:**
```go
diff := domain.ComputeDiff(rs.SrcMap, rs.DstMap)
var addBytes, updateBytes, deleteBytes int64
for _, f := range diff.Add    { addBytes    += rs.SrcMap[f].Size }
for _, f := range diff.Update { updateBytes += rs.SrcMap[f].Size }
for _, f := range diff.Delete { deleteBytes += rs.DstMap[f].Size }

rs.Diff = diff
rs.TransferBytes = addBytes + updateBytes
rs.DeleteBytes   = deleteBytes

bus.Publish(SyncPlanInfo{...})
for _, f := range diff.Add    { bus.Publish(SyncPlanFileInfo{Path: f, Action: "add",    Size: rs.SrcMap[f].Size, ...}) }
for _, f := range diff.Update { bus.Publish(SyncPlanFileInfo{Path: f, Action: "update", Size: rs.SrcMap[f].Size, ...}) }
for _, f := range diff.Delete { bus.Publish(SyncPlanFileInfo{Path: f, Action: "delete", Size: rs.DstMap[f].Size, ...}) }

if diff.Empty() { return doneStrategy, nil }
return stageDirInitStrategy, nil
```

**StageDirInit:** generate `rs.StagingID = uuid.NewString()`, set `rs.StagingPath = stagingRoot + "/" + rs.StagingID`. Emit `SyncStagingDirCreatedInfo`. (Local FS: `os.MkdirAll`. R2: no-op until first `Put`.)

**Staging:** loop `Diff.Add ∪ Diff.Update`:
```go
bus.Publish(SyncStageStartedInfo{Files: len(transfer), Bytes: rs.TransferBytes, ...})
var bytesDone int64
for i, f := range transfer {
    if err := ctx.Err(); err != nil { ... }
    data, err := rs.src.Get(ctx, f); if err != nil { return failedFor("stage", f, err) }
    err = rs.dst.Put(ctx, rs.StagingPath+"/"+f, data); if err != nil { return failedFor("stage", f, err) }
    bytesDone += rs.SrcMap[f].Size
    bus.Publish(SyncStageProgressInfo{File: f, FilesDone: i+1, FilesTotal: len(transfer), BytesDone: bytesDone, BytesTotal: rs.TransferBytes, ...})
}
bus.Publish(SyncStageFinishedInfo{Files: len(transfer), Bytes: rs.TransferBytes, DurationMs: ...})
```

**Committing:** loop same files, `dst.Copy(staging → final)` then `dst.Delete(staging key)`. Same accumulator pattern. Emits `SyncCommit{Started,Progress,Finished,Failed}Info`.

**OrphanCleanup (upload):** `dst.Delete(diff.Delete batch)` — uses `DeleteBatch`. Single `SyncOrphanCleanupInfo` carries per-key results.

**GhostCleanup (download):** loop `diff.Delete`, per-file `dst.Delete`, per-file `SyncGhostDeletedInfo`.

**StagingDirCleanup:** `dst.Delete(rs.StagingPath)` — relies on adapter tree-delete semantics (localfs: `RemoveAll`; r2: `List + DeleteBatch`). Single `SyncStagingDirCleanedInfo` with `Outcome` and `DurationMs`.

**Done:** emit `SyncFinishedInfo{Files, Bytes, DurationMs}` derived from rs.TransferBytes + transfer file count + run start.

**Failed:** emit `SyncFailedInfo{Phase, Err}`, **then run staging cleanup** (`dst.Delete(rs.StagingPath)`), then return `rs.Err`.

### Event interleaving (sample upload trace)

```
SyncStartedInfo            src=fs::./worlds dst=r2::bucket/worlds
SyncPlanInfo               adds=2 updates=1 deletes=0 totalBytes=1024
SyncPlanFileInfo           path=world/level.dat action=update size=512
SyncPlanFileInfo           path=world/region/r.0.0.mca action=add size=400
SyncPlanFileInfo           path=server/server.properties action=add size=112
SyncStagingDirCreatedInfo  staging=550e8400-e29b-41d4-a716-446655440000
SyncStageStartedInfo       files=3 bytes=1024
StorageGetInfo             store=fs::./worlds key=world/level.dat bytes=512 durMs=2
StoragePutInfo             store=r2::bucket/worlds key=.staging/550e.../world/level.dat bytes=512 durMs=18
SyncStageProgressInfo      file=world/level.dat filesDone=1 filesTotal=3 bytesDone=512 bytesTotal=1024
...
SyncStageFinishedInfo      files=3 bytes=1024 durMs=63
SyncCommitStartedInfo      files=3 bytes=1024
StorageCopyInfo            store=r2::bucket/worlds src=.staging/.../world/level.dat dst=worlds/world/level.dat durMs=12
StorageDeleteInfo          store=r2::bucket/worlds key=.staging/.../world/level.dat durMs=8
SyncCommitProgressInfo     file=world/level.dat filesDone=1 filesTotal=3 bytesDone=512 bytesTotal=1024
...
SyncCommitFinishedInfo     files=3 bytes=1024 durMs=58
SyncStagingDirCleanedInfo  staging=550e... outcome=success durMs=18
SyncFinishedInfo           files=3 bytes=1024 durMs=210
```

## Error Handling

### Strict propagation, no swallowing

- Engine code: every storage call's `err` is checked or returned. No `_ = ...`.
- Decorator: never swallows; publishes event with `Err` and returns the err verbatim.
- Strategy `Run()` returns `(Strategy, error)`; non-nil error reaches the run-level machine via Failed terminal.

### Failure flow (override of story #10)

**Story #10 originally specified preserve-on-failure for inspection. This spec overrides:** events are the forensic record (per-file transfer state, per-call storage outcomes, per-phase failure reason all logged). Filesystem preservation creates ghost-hoarding. Cleanup runs unconditionally.

Sequence on phase failure:
1. Strategy publishes `Sync<Phase>FailedInfo{File, Err}`.
2. Strategy sets `rs.Err = fmt.Errorf("<phase> %s: %w", file, err)` and `rs.FailedPhase = "<phase>"`. `Phase` values are stable identifiers: `"scan"`, `"plan"`, `"stage-dir-init"`, `"stage"`, `"commit"`, `"orphan-cleanup"`, `"ghost-cleanup"`, `"staging-dir-cleanup"`.
3. Strategy returns Failed terminal.
4. Failed publishes `SyncFailedInfo{Phase, Err}`.
5. Failed runs `dst.Delete(rs.StagingPath)` (skipped if `StagingPath == ""`, i.e. failure before StageDirInit).
6. Failed publishes `SyncStagingDirCleanedInfo{Outcome: ..., Err: ...}`.
7. Failed returns `rs.Err` (cleanup error never overrides original).

### Context cancellation

Engine respects `ctx.Done()` between files. Cancellation = failure with `err = ctx.Err()`. Cleanup still runs.

### Crash recovery

If process is killed mid-flight, staging dir survives. Next run's StageDirInit uses a fresh UUIDv4 — no collision. Stale staging dirs become orphans. **Orphan-sweep at startup is out of scope of this spec** (story #4 follow-up).

## Testing

All existing tests must pass after refactor. New tests cover critical points.

### Per-strategy unit tests

`internal/core/sync/<phase>/strategy_test.go` for each phase. Pattern:

```go
func TestStaging_HappyPath(t *testing.T) {
    src := memstore.New("mem::src"); dst := memstore.New("mem::dst")
    bus := eventbustest.New()
    rs := &SyncRunState{src: src, dst: dst, bus: bus,
        SrcMap: map[string]FileEntry{"a.dat": {Hash: "h1", Size: 10}},
        Diff: domain.SyncDiff{Add: []string{"a.dat"}},
        StagingID: "test-uuid", StagingPath: ".staging/test-uuid",
        TransferBytes: 10}
    src.MustPut("a.dat", []byte("0123456789"))

    next, err := staging.New(committingStub, failedStub).Run(ctx, rs)
    require.NoError(t, err)
    require.Same(t, committingStub, next)
    require.Equal(t, []byte("0123456789"), dst.MustGet(".staging/test-uuid/a.dat"))
    eventbustest.AssertSequence(t, bus, []reflect.Type{
        reflect.TypeOf(SyncStageStartedInfo{}),
        reflect.TypeOf(StorageGetInfo{}),
        reflect.TypeOf(StoragePutInfo{}),
        reflect.TypeOf(SyncStageProgressInfo{}),
        reflect.TypeOf(SyncStageFinishedInfo{}),
    })
}
```

Failure path test per strategy: inject failing storage → assert `Failed` returned + correct `*FailedInfo` emitted.

### Engine integration test

`internal/core/sync/engine_test.go`. Two `localfs.Storage` (the `sync(local, local)` capability) with the `observed` decorator wrapping each. Seed src, run engine, assert dst matches src + expected event sequence + staging dir gone.

### Ordering invariant test

Drain bus during real run, filter `Sync*` events, assert sequence matches expected `[]reflect.Type`. Catches accidental wire-up bugs.

### Failure-path integration test

Inject failing decorator at staging step:
- `SyncStageFailedInfo` published.
- `SyncFailedInfo{Phase: "stage"}` published.
- `SyncStagingDirCleanedInfo{Outcome: "success"}` published — cleanup ran.
- Staging dir gone from disk.
- No `SyncFinishedInfo` published.

### Storage decorator unit tests

`internal/adapters/observed/storage_test.go` — one happy + one error path per method. ~14 tests.

### Adapter `String()` tests

Each adapter test file asserts `String()` returns expected `"<type>::<path>"` format.

### r2 tree-delete test

`internal/adapters/r2_test.go`: `Delete` on a prefix removes all matching keys; `Delete` on missing prefix returns `"key not found"`.

### Critical points new tests must cover

1. Failure runs cleanup (staging never preserved).
2. Empty diff short-circuits to Done without StageDirInit.
3. `sync(local, local)` round-trip works; src/dst discriminated by path.
4. Storage decorator publishes on err path (failed Get still emits `StorageGetInfo` with `Err` populated).
5. Plan event totals match actual transferred bytes.
6. Per-file progress accumulator never exceeds total.
7. Sync engine asserts non-reentrant via panic-on-double-Run; concurrent guarding lives upstream (locking stage).

## Migration & Scope

- **Manifest schema change** (`map[string]string` → `map[string]FileEntry`) is uncontroversial (no released hashsync). Plain refactor.
- **`SyncService` deleted.** Callers (`SyncDownloadUpdater`, `SyncUploader`) construct and run new engine instead.
- **Old `StartInfo{"sync-..."}`/`UpdateInfo`/`FinishInfo` emissions removed** from sync code. Generic `*Info` types remain for non-sync ops.
- **Composition root rewires** every storage construction with `observed.NewStorage(inner, bus)`.

## Out of Scope / Follow-Ups

- Orphan-staging sweep at startup (crash recovery).
- `SettingsRepository` port to remove direct I/O from `internal/core/domain/settings.go`.
- Per-byte progress inside large `Put` calls (would require streaming `Put` and adapter changes).
- `Sync*Info` decorator subscribers (filtered logger, GUI subscription wiring) — separate spec when GUI sprint begins.

## Acceptance Criteria

Each criterion is verifiable by code, test output, or manual log inspection.

### Architectural

1. `internal/core/services/sync.go` deleted; `SyncService` removed.
2. `internal/core/sync/` package exists with engine, runstate, events, and one strategy package per phase listed in Components.
3. `internal/adapters/observed/storage.go` exists; `observed.NewStorage(inner, bus)` returns a `ports.StorageRepository`.
4. `ports.StorageRepository` embeds `fmt.Stringer` and declares `Rename(ctx, src, dst string) error`.
5. `internal/adapters/fs.go` and `internal/adapters/r2.go` both implement `String()` returning `"<type>::<initialpath>"` (e.g. `"fs::./worlds"`, `"r2::bucket/prefix"`).
6. `internal/adapters/r2.go` `Delete` performs tree-delete via internal `List + DeleteBatch`; returns `"key not found"` error on empty result.
7. `internal/core/services/updater_ritual.go` performs no direct `os.ReadFile`/`os.WriteFile`/`os.Remove`/`os.Rename`; all file CRUD goes through a `StorageRepository`.
8. Composition root wraps every constructed `StorageRepository` with `observed.NewStorage(...)`.
9. `internal/core/domain/manifest.go` `SyncState.XXHashMap` is `map[string]FileEntry`; `FileEntry{Hash string, Size int64}` exists.
10. No production sync code emits `StartInfo`/`UpdateInfo`/`FinishInfo` keyed by `"sync-*"`. (Generic types remain available for non-sync code.)

### Behavioral

11. Engine constructed by `sync.NewMachine(src, dst, bus)`; `src` and `dst` are `ports.StorageRepository` — direction is wiring, not type.
12. `sync.NewMachine(local, local, bus)` runs end-to-end in tests with two `localfs.Storage` instances.
13. Empty diff short-circuits: Planning routes directly to Done; no `SyncStagingDirCreatedInfo` emitted; `SyncFinishedInfo{Files: 0, Bytes: 0}` emitted.
14. Plan totals are correct: `SyncPlanInfo.AddBytes + UpdateBytes` equals sum of `BytesDone` across all `SyncStageProgressInfo` events for the run.
15. Per-file progress accumulator never exceeds total: every `SyncStageProgressInfo.BytesDone <= BytesTotal`. Same for commit.
16. Per-file ops (stage, commit, ghost-cleanup) emit one event per file. Batch ops (orphan-cleanup, staging-cleanup) emit one batch event with per-key outcome.
17. Every `Sync*Info` and `Storage*Info` event includes `Source` (and `Destination` for sync events) derived from `fmt.Sprint(store)`. Strings match adapter `String()` format.
18. `StagingPath` contains only the run's UUIDv4 — no lock identity, no PC name. (Lock identity remains in `LockAcquiredInfo` only.)

### Failure semantics

19. On any phase failure, `Sync<Phase>FailedInfo` is published before halt (file path + error).
20. After phase failure, Failed terminal publishes `SyncFailedInfo{Phase, Err}`.
21. After phase failure (excluding failure before StageDirInit), Failed terminal runs `dst.Delete(rs.StagingPath)`; staging dir does NOT exist on disk after the run.
22. After phase failure, `SyncStagingDirCleanedInfo{Outcome: "success"|"failed", Err: ...}` is published.
23. After phase failure, `SyncFinishedInfo` is NOT published.
24. Cleanup error during failure path is logged via event but does not override original `rs.Err`.
25. Successful run: `SyncStagingDirCleanedInfo{Outcome: "success"}` precedes `SyncFinishedInfo`; staging dir gone from storage.
26. Engine returns no `_ = storageCall(...)` patterns; all storage errors propagate or branch to Failed.

### Observability

27. Storage decorator publishes exactly one `Storage*Info` event per `StorageRepository` method invocation (success or failure).
28. Failed `StorageGetInfo`/`StoragePutInfo`/etc. events include the underlying error in `Err` and still report `Bytes`/`DurationMs`.
29. Bus contract preserved: `bus.Publish(...)` is non-blocking; slow subscribers drop.
30. Default subscriber printing via `evt.String()` produces a single greppable line per event including `src=...→dst=...` (sync) or `store=...` (storage).

### Testing

31. **All pre-existing tests in the repository pass after the refactor.** This includes `internal/core/services/sync_integration_test.go` rewritten against the new engine, `internal/app/ritual_integration_test.go` adapted for wrapped stores, all adapter tests, and all unrelated package tests.
32. New per-strategy unit tests exist in `internal/core/sync/<phase>/strategy_test.go` for every strategy listed in Components. Each covers happy + failure paths.
33. Engine integration test in `internal/core/sync/engine_test.go` runs an end-to-end `sync(local, local)` with the `observed` decorator and asserts: dst matches src, expected `[]reflect.Type` event sequence, staging dir cleaned.
34. Ordering invariant test asserts `Sync*` event sequence matches expected types in order.
35. Failure-path integration test injects failing storage at staging step; asserts criteria 19–23.
36. `internal/adapters/observed/storage_test.go` covers each method × {happy, error}.
37. `internal/adapters/fs_test.go`, `internal/adapters/r2_test.go` assert `String()` returns expected `"<type>::<path>"` format.
38. `internal/adapters/r2_test.go` covers tree-delete on prefix and `"key not found"` on empty prefix.
39. `internal/core/domain/manifest_test.go` covers JSON round-trip of new `FileEntry{Hash, Size}` shape.

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Refactor introduces ordering bug not present today | medium | Topology-enforced ordering + dedicated ordering invariant test |
| r2 `Delete` semantics change ripples to non-sync callers | low | Audit shows only sync.go calls r2.Delete on a prefix today; sync.go is being replaced |
| Storage decorator publishing rate is high under heavy sync | low | Bus is non-blocking; slow subscribers drop by design |
| Manifest schema commitment from day one | low | Pre-1.0; no released schema |
| Strategy proliferation invites unrelated future expansion | medium | Each strategy is bounded to one phase responsibility; convention codified in this spec |
