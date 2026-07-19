# Backup & Retention Redesign — Technical Specification

## Motivation

Two problems:

1. **Backups are dead weight.** `SyncUploadBackupper` returns empty archive name. `updateManifestsWithArchive` never fires. `Backups []World` never populates. Retention never runs. ~956 LOC doing nothing.
2. **Hosts are distributed.** Each host is independent. No shared R2 guarantee. Host A offline = Host B cannot rely on A's delta sync state. Backups must be self-contained per host — every backup independently restorable.

Solution: two simple backuppers (local + R2) that copy worlds uncompressed. One generic retention engine replaces two bespoke implementations. Dirty check via xxhash diff replaces PLAYER_JOINED heuristic.

### Design Principles

- **Negative LOC.** Maintenance is more expensive than writing. More code = more parts to break.
- **Radical simplicity.** Ideas as abstractions, not code as abstractions.
- **SOLID as thought expression engine.** Open/closed via strategy functions. DI for testability.
- **Tests as critique.** Pure functions tested without mocks. IO boundaries thin and obvious.
- **Stdlib over custom.** `time.Parse`, `time.Format`, `slices.SortFunc`, `time.ISOWeek` — no custom calendar, sorting, or parsing logic.

---

## Key Changes

| Current | Target |
|---|---|
| `retention_local.go` (102 LOC) | Deleted — generic engine |
| `retention_r2.go` (142 LOC) | Deleted — generic engine |
| `retention_util.go` + tests (118 LOC) | Deleted — timestamp parsing moves to strategy function |
| `retention_logs.go` (100 LOC) | Separate concern, untouched |
| `session.go` CheckPlayersJoined (32 LOC) | Deleted — xxhash diff is ground truth |
| `session_test.go` (159 LOC) | Deleted |
| `mocks/backupper.go`, `mocks/retention.go` (85 LOC) | Deleted |
| `molfar.go` archive plumbing (122 LOC) | Simplified — backuppers return nothing, retention decoupled from manifest |
| `BackupperService` returns `(string, error)` | Returns `error` only |
| `RetentionService.Apply(ctx, manifest)` | `RetentionService.Apply(ctx)` — no manifest dependency |
| Two retention implementations | One generic engine, injected twice with different storage |
| PLAYER_JOINED gate | xxhash dirty check |
| `Manifest.AddWorld`, `GetLatestWorld`, `RemoveOldestWorlds` | Deleted |
| `domain.World` | Deleted |
| `Manifest.Worlds.Backups []World` | Deleted |

**Target: ~-600 net LOC.**

---

## Backup Structure

Both local and R2 use identical layout:

```
{prefix}/{timestamp}/
  worlds/
    world/
    world_nether/
    ...
  manifest.json
```

- `{prefix}` — `world_backups` (local), `backups` (R2)
- `{timestamp}` — `20060102150405` format in **UTC** (existing `TimestampFormat`)
- `manifest.json` — snapshot of manifest at backup time (self-contained restore point, carries xxhash state)
- Worlds copied uncompressed
- **Backup dir is sacred.** Unknown files = corruption. Retention deletes them. "The dog bites, don't come close."

### Data Format

- **Worlds** — copied uncompressed as-is. Key structure preserves directory nesting: `{prefix}/{ts}/worlds/world/region/r.0.0.mca`
- **Local backup** — `localStorage.Copy(ctx, srcKey, dstKey)` — same-disk copy, no memory load
- **R2 backup** — `remoteStorage.Copy(ctx, srcKey, dstKey)` — server-side copy within R2, no bytes travel. Source is `worlds/` (live delta sync state), destination is `backups/{ts}/worlds/`
- **manifest.json** — `json.MarshalIndent(manifest, "", "  ")` → `dst.Put()`. Readable, consistent with all other config files in project.

---

## Dirty Check

Backup created only when worlds changed since last sync.

```
current xxhash map ≠ last synced xxhash map → dirty → backup
current xxhash map = last synced xxhash map → clean → skip
```

Already computed by delta sync. Zero extra cost. Replaces `CheckPlayersJoined` log parsing — xxhash diff is ground truth vs player-join being a proxy signal. Player joins but changes nothing → no backup. Server generates chunks without player → backup triggers.

---

## Retention Rules

### Config Model

```go
type RetentionRules struct {
    KeepLast    int `json:"keep_last"`
    KeepDaily   int `json:"keep_daily"`
    KeepWeekly  int `json:"keep_weekly"`
    KeepMonthly int `json:"keep_monthly"`
}
```

All fields 0-5. Zero = tier disabled. No floor enforced — user's choice, UI warns about consequences.

**Default: `KeepLast: 2`, rest zero.** Matches current `R2MaxBackups = 2` / `LocalMaxBackups = 2` behavior.

### Where Rules Live

| Scope | Location | Who controls |
|---|---|---|
| Local retention | Host config file (per-host) | Host owner |
| R2 retention | `manifest.json` | Admin |

Each host sets own local rules — their disk, their rules. R2 is shared resource — admin decides via manifest.

### Tier Algorithm (borg/restic model)

Union-based protection. Each tier "protects" backups independently. Any tier wants it → kept. No tier wants it → deleted. Tiers never conflict — more tiers = more protection, never less.

**How tiers spread over time:**

```
keep_last: 1, keep_weekly: 1, keep_monthly: 1

Early (all tiers overlap on one backup):
|Апр──────────────────────────────────|
                                    ●   ← protected by all 3 tiers

After weeks pass (tiers separate):
|Мар─────────|Апр──────────────────────|
      ●            ●              ●
      мес          нед        последний

Config examples:
  Параноик:   keep_last:5, keep_daily:3, keep_weekly:2, keep_monthly:2  (~12 backups)
  Экономный:  keep_last:1, keep_monthly:3                               (~4 backups)
  Минималист: keep_last:1                                               (1 backup)
  Архивариус: keep_last:1, keep_monthly:4                               (~5 backups, months of coverage)
```

**UI mapping:** 4 sliders (0-5 each) + computed "total protected" preview + timeline visualization. Labels self-document.

### Config Integration

**Local retention rules** — extend existing `Settings` struct in `domain/settings.go`:

```go
type Settings struct {
    IP              string         `json:"ip"`
    Port            int            `json:"port"`
    Memory          int            `json:"memory"`
    LocalRetention  RetentionRules `json:"local_retention"`
}
```

Lives in `settings.json` per host. `LoadSettings()` already handles defaults via `DefaultSettings()`. Add `DefaultRetentionRules()` returning `KeepLast: 2` (matches current `LocalMaxBackups`). Missing/zero fields in existing `settings.json` files get defaults — backward compatible.

**R2 retention rules** — extend `Manifest` struct:

```go
type Manifest struct {
    // ... existing fields ...
    RemoteRetention RetentionRules `json:"remote_retention"`
}
```

Admin sets via manifest. All hosts read same R2 rules. `ApplyDefaults()` already exists on Manifest — add retention defaults there. Default: `KeepLast: 2` (matches current `R2MaxBackups`).

**Loading in main.go:**

```go
settings, _ := domain.LoadSettings()
manifest, _ := librarian.GetRemoteManifest(ctx)

localRules  := settings.LocalRetention   // per-host
remoteRules := manifest.RemoteRetention   // admin-controlled
```

No new config file. No new loading mechanism. Extends existing structures.

### Timestamps

All backup timestamps stored in **UTC**. Hosts in different timezones produce consistent ordering. Tier grouping (daily/weekly/monthly) uses UTC calendar boundaries via stdlib:

```go
// Daily:   t.Format("2006-01-02")
// Weekly:  y, w := t.ISOWeek(); fmt.Sprintf("%d-W%02d", y, w)
// Monthly: t.Format("2006-01")
```

Zero custom calendar logic.

---

## Architecture

### Two-Phase Retention: Mark then Delete

Retention is two atomic stages:

1. **Mark** — pure function. Receives `[]string` (keys), returns `[]string` (keys to delete). No IO. No state. No storage dependency.
2. **Delete** — caller executes deletion. Retryable. Failed delete = re-mark, re-delete next run.

```
caller:  keys     = storage.List(prefix)
engine:  toDelete = Mark(keys, rules, parseStrategy)    // pure, no IO
caller:  storage.DeleteBatch(toDelete)
```

Retention engine = stateless function. Mark phase fully testable without mocks. Delete phase is separate atomic commit.

### Strategy Function for Entry Parsing

Engine delegates "is this a backup? what's its timestamp?" to an injected strategy:

```go
// ParseStrategy extracts a UTC timestamp from a storage key.
// Returns zero time if key is not a recognized backup entry.
type ParseStrategy func(key string) time.Time
```

New format = new strategy. Engine untouched. Open/closed.

**V1→V2 migration** handled by composing strategies:

```go
parse := ChainStrategies(
    ParseTimestampDir,    // 20260414160000/
    ParseTimestampTar,    // 20260414160000.tar
)
```

Mark stays pure. Migration = swap strategy at DI site. Engine never changes. Old `.tar` files age out naturally under same rules alongside new directory backups.

Strategy implementations use stdlib only: `time.Parse` + `filepath.Base` + `strings.TrimSuffix`.

### Mark Function

```go
func Mark(keys []string, rules RetentionRules, parse ParseStrategy) []string
```

1. Parse each key via strategy. Unparseable → mark for deletion (sacred dir).
2. Sort parsed entries newest → oldest via `slices.SortFunc` + `time.Compare` (UTC).
3. Walk each entry, track seen buckets via `map[string]int`:
   - Within `KeepLast` count? → protect
   - First seen for its UTC calendar day? → protect (up to `KeepDaily`)
   - First seen for its ISO week? → protect (up to `KeepWeekly`)
   - First seen for its UTC calendar month? → protect (up to `KeepMonthly`)
4. Unprotected → returned as delete list.

Union logic. ~40 lines of stdlib calls with a loop. Internal `entry` struct (key + timestamp) is unexported, local to `retention.go` — not a domain type.

### Backupper — Function, Not Struct

No struct. No interface. No DI ceremony. A one-shot copy operation is a function.

```go
func CreateBackup(
    ctx context.Context,
    storage StorageRepository,
    srcPrefix, dstPrefix string,
    manifest *Manifest,
) error {
    ts := time.Now().UTC().Format(config.TimestampFormat)
    base := path.Join(dstPrefix, ts)

    keys, err := storage.List(ctx, srcPrefix)
    if err != nil {
        return err
    }

    for _, key := range keys {
        if err := storage.Copy(ctx, key, path.Join(base, key)); err != nil {
            return err
        }
    }

    data, err := json.MarshalIndent(manifest, "", "  ")
    if err != nil {
        return err
    }

    return storage.Put(ctx, path.Join(base, "manifest.json"), data)
}
```

~15 lines. Pure stdlib: `time`, `path`, `json`, `strings`. No custom helpers. No `fmt.Sprintf` for paths. `path` (not `filepath`) because storage keys are slash-separated regardless of host OS.

**Scope:**
- Knows nothing about worlds, sync state, or dirty checks
- Takes a storage, two prefixes, a manifest
- Copies files within same storage (same-disk locally, server-side on R2)
- Writes one manifest snapshot
- Caller decides when to call it

**Dirty check moves to caller** (Molfar/state machine exit):

```go
if dirty(localState, remoteState) {
    CreateBackup(ctx, localStorage, "worlds", "backups", manifest)
    CreateBackup(ctx, remoteStorage, "worlds", "backups", manifest)
}
```

Two lines for both backups. No coupling between backupper and librarian/sync state.

### Interface Changes

```go
// Before
type BackupperService interface {
    Run(ctx context.Context) (string, error)
}

type RetentionService interface {
    Apply(ctx context.Context, manifest *domain.Manifest) error
}

// After
// BackupperService — DELETED. Backup is a function, not a service.
type RetentionService interface {
    Apply(ctx context.Context) error
}
```

`BackupperService` interface dies entirely. Backup is `CreateBackup(ctx, storage, src, dst, manifest)` — a function. No interface ceremony. Caller composes as needed.

`RetentionService` simplified. No return values. No manifest dependency. Retention owns its storage, rules, and parse strategy at construction time.

### Retention Service (wraps Mark + IO)

```go
type retention struct {
    storage StorageRepository
    rules   RetentionRules
    prefix  string
    parse   ParseStrategy
}

func (r *retention) Apply(ctx context.Context) error {
    keys, err := r.storage.List(ctx, r.prefix)
    // ...
    toDelete := Mark(keys, r.rules, r.parse)
    return r.storage.DeleteBatch(ctx, toDelete)
}
```

Thin wrapper. All logic in `Mark`. Struct exists only to satisfy `RetentionService` interface and hold injected dependencies.

### Molfar Exit Flow

```
Exit()
  ├─ backuppers[i].Run(ctx)     — delta sync, local snapshot, R2 snapshot
  ├─ retentions[i].Apply(ctx)   — prune local, prune R2, prune logs
  └─ unlockManifests(ctx)
```

`updateManifestsWithArchive` deleted. No archive name plumbing. No manifest Backups list dependency.

### DI in main.go

```go
// Parse strategy — shared, handles both v1 (.tar) and v2 (dir) formats
parseBackupTimestamp := ChainStrategies(ParseTimestampDir, ParseTimestampTar)

// Retentions — default KeepLast:2 matches current R2MaxBackups/LocalMaxBackups
localRetention := NewRetention(localStorage, localRules, config.BackupsDir, parseBackupTimestamp)
r2Retention    := NewRetention(remoteStorage, remoteRules, config.BackupsDir, parseBackupTimestamp)
logRetention   := NewLogRetention(localStorage, events)  // untouched

retentions := []ports.RetentionService{localRetention, r2Retention, logRetention}
```

No backupper construction — backup is a function call in exit flow. Same retention engine, different storage injected. Same parse strategy shared.

### Exit Flow

> **Note:** Examples below show integration with current `MolfarService.Exit()`. MolfarService is planned for replacement by a state machine orchestrator (see `docs/state-machine-proposal.md`). All functions and the `RetentionService` interface are orchestrator-agnostic — they plug into state machine transitions with zero changes. Spec describes **behaviors**, not Molfar method structure.


```go
// In Molfar/state machine Exit:
localState, remoteState := getSyncStates(manifest)

// Delta sync upload — existing behavior
if err := worldSync.Upload(ctx, localState, remoteState); err != nil { ... }

// Backups — only if worlds changed
if !maps.Equal(localState.XXHashMap, remoteState.XXHashMap) {
    CreateBackup(ctx, localStorage, config.WorldsDir, config.BackupsDir, manifest)
    CreateBackup(ctx, remoteStorage, config.WorldsDir, config.BackupsDir, manifest)
}

// Retention — always runs, prunes old backups regardless of session dirtiness
for _, r := range retentions {
    r.Apply(ctx)
}

unlock(ctx)
```

`maps.Equal` — stdlib (`maps` package, Go 1.21+). Dirty check inline, one line.

---

## Testing Strategy

### Mark Function — Pure Function Tests

```go
func Mark(keys []string, rules RetentionRules, parse ParseStrategy) []string
```

Table-driven. No mocks. No IO. No storage.

| Case | Input | Expect |
|---|---|---|
| Empty list | `[]`, any rules | delete=[] |
| All protected | 2 keys, `KeepLast:5` | delete=[] |
| All tiers zero | 5 keys, all zeros | delete=5 |
| Single tier | 10 keys spanning 3 months, `KeepMonthly:2` | delete=8 |
| Overlap | 1 key, all tiers nonzero | delete=0 |
| Same-second timestamps | 2 keys same timestamp | deterministic by key |
| Unparseable key | unknown file in sacred dir | delete=1 (killed) |
| Daily boundary | 23:59 UTC vs 00:01 UTC | different days |
| Weekly boundary | Sunday vs Monday | depends on ISO week |
| Month boundary | Jan 31 vs Feb 1 | different months |
| Mixed formats | `.tar` + dirs | both parsed by strategy |
| Gaps | 3 months gap | monthly tier covers correctly |
| Timezone safety | timestamps always UTC | grouping consistent regardless of host TZ |
| Default config | `KeepLast:2`, rest zero | keep 2 newest, delete rest |

### Parse Strategy — Unit Tests

Test each strategy function independently. Stdlib-only implementations:
- Known format → correct `time.Time` (UTC)
- Unknown format → zero time
- Edge cases: extensions, nested paths, empty strings
- `ChainStrategies`: first match wins, fallthrough on zero time

### Retention Service — Mock Storage Tests

Mock `StorageRepository`. Verify:
- `List` called with correct prefix
- `DeleteBatch` called with exact output of `Mark`
- Empty storage = no deletes
- List error propagates
- DeleteBatch error propagates

### Backupper — Mock Storage Tests

Mock src + dst `StorageRepository`. Verify:
- Dirty (hash diff) → copies all keys + manifest
- Clean (no diff) → no copies
- Keys written to correct `{prefix}/{ts}/worlds/{key}` paths
- `manifest.json` serialized and written
- Timestamp in UTC

### Integration — Real Filesystem

Temp dir with mix of `.tar` files and timestamped directories. Run full Apply cycle. Verify survivors on disk.

### Integration — Dirty/Clean + Backupper

Verifies backupper correctly reads sync state and decides to copy or skip.

| Case | Setup | Expect |
|---|---|---|
| Clean after sync | Sync runs, updates xxhash map, then backupper runs | Backupper skips — no copy |
| Dirty after world change | World files modified between syncs | Backupper copies worlds + manifest |
| First run (no previous hash map) | Empty/nil xxhash map in sync state | Dirty — backup created |
| Sync failed mid-run | Stale hash map from previous session | Backupper compares against stale map — still correct |
| Hash maps equal but different key order | Same entries, different iteration order | Clean — maps.Equal handles this |

### Integration — Molfar Exit Flow

Full pipeline tests. Proves wiring between backuppers, retention, and lock lifecycle.

| Case | Setup | Expect |
|---|---|---|
| Dirty exit | Worlds changed during session | Sync uploads + backup created + retention prunes + unlock |
| Clean exit | No world changes | Sync uploads (no diff) + backup skipped + retention still runs + unlock |
| No lock owned | `currentLockID == ""` | Everything skipped — no backup, no retention, no unlock |
| Backupper fails | First backupper errors | Exit aborts. No retention runs. Lock remains (manual recovery). |
| Retention fails | Backupper succeeds, retention errors | Exit aborts after backup. Backup survives on storage. |
| Multiple backuppers | Sync + local + R2 in sequence | Each independently dirty-checks. One clean + two dirty = only two copy. |
| Retention after backup skip | Clean worlds, backup skipped | Retention still runs — prunes old backups regardless of current session dirtiness. |

---

## Edge Cases

| Case | Behavior |
|---|---|
| Mixed `.tar` + directory backups | Both parsed by strategy chain. Coexist under same rules. Old tars age out naturally. |
| Same-second timestamps | Deterministic tiebreaker by key (alphabetical). |
| Deletion fails mid-run | Next run idempotent — re-marks, re-deletes. Mark and delete are separate atomic stages. |
| All tiers zero | Everything deleted. UI warns, engine obeys. |
| Unknown files in backup dir | Deleted. Sacred dir — unknown = corruption. |
| Config change between runs | New rules apply immediately. Previously safe backups may be pruned. Expected. |
| Timezone differences | UTC timestamps. All hosts, all tiers use UTC calendar boundaries. |
| Empty storage | No-op. No errors. |
| `KeepLast:0` with only monthly | Backups within same month replaced each session. UI warns: "without 'keep last', backups are deleted after next session." |

---

## Acceptance Criteria

### Core Behavior

- [ ] Dirty worlds (xxhash diff) → local backup created at `world_backups/{ts}/[worlds/, manifest.json]`
- [ ] Dirty worlds (xxhash diff) → R2 backup created at `backups/{ts}/[worlds/, manifest.json]`
- [ ] Clean worlds (no xxhash diff) → no backup created, no files copied
- [ ] Backup `manifest.json` is a valid snapshot — deserializable, contains xxhash state
- [ ] All backup timestamps in UTC regardless of host timezone
- [ ] `CheckPlayersJoined` removed — dirty check is sole gate for backup creation

### Retention Engine

- [ ] `Mark()` is a pure function — no IO, no storage calls, no side effects
- [ ] `Mark()` accepts `ParseStrategy` — engine never parses keys directly
- [ ] Tiered protection: `keep_last`, `keep_daily`, `keep_weekly`, `keep_monthly` (0-5 each)
- [ ] Union logic: any tier protects → entry survives
- [ ] All tiers zero → all entries marked for deletion
- [ ] Unknown/unparseable entries in backup dir → marked for deletion
- [ ] Same-second timestamps → deterministic tiebreaker (alphabetical by key)
- [ ] Mark and delete are separate stages — `Mark()` returns list, caller deletes

### V1 Compatibility

- [ ] `ChainStrategies` parses both `.tar` (v1) and directory (v2) formats
- [ ] Old `.tar` backups coexist with new directory backups under same retention rules
- [ ] Old `.tar` backups age out naturally — no forced migration, no special cleanup

### Config Integration

- [ ] Local retention rules loaded from `settings.json` per host (`LocalRetention` field)
- [ ] R2 retention rules loaded from `manifest.json` (`RemoteRetention` field)
- [ ] Missing retention config → defaults to `KeepLast: 2` (backward compatible)
- [ ] Existing `settings.json` without retention fields loads without error

### Interface Changes

- [ ] `BackupperService.Run()` returns `error` only (no archive name string)
- [ ] `RetentionService.Apply()` takes only `ctx` (no manifest parameter)
- [ ] Molfar `Exit()` works without `updateManifestsWithArchive`
- [ ] Molfar `Exit()` runs retention even when backup was skipped (clean worlds)

### Dead Code Removed

- [ ] `retention_local.go` deleted
- [ ] `retention_r2.go` + `retention_r2_test.go` deleted
- [ ] `retention_util.go` + `retention_util_test.go` deleted
- [ ] `session.go` (`CheckPlayersJoined`) + `session_test.go` deleted
- [ ] `mocks/backupper.go` + `mocks/backupper_test.go` + `mocks/retention.go` deleted
- [ ] `molfar.go` `updateManifestsWithArchive` deleted
- [ ] `domain.World` + `world_test.go` deleted
- [ ] `Manifest.Worlds.Backups`, `AddWorld`, `GetLatestWorld`, `RemoveOldestWorlds` deleted

### Post-Implementation Dead Code Audit

- [ ] After implementation, full codebase sweep for orphaned code: imports referencing deleted types, functions that only served `World`/`Backups`/`CheckPlayersJoined`/archive plumbing, config constants (`R2MaxBackups`, `LocalMaxBackups`, `ManualWorldFilename`) that lost all callers, mock types that implemented removed interfaces, test helpers that set up deleted flows. Grep for every deleted symbol — any remaining reference is dead or broken.

### Tests Pass

- [ ] `Mark()` pure function tests — all table cases from spec
- [ ] `ParseStrategy` unit tests — both formats + chain
- [ ] Retention service mock storage tests — List/DeleteBatch wiring
- [ ] Backupper mock storage tests — dirty/clean/copy paths
- [ ] Dirty/clean integration — sync state + backupper interaction
- [ ] Molfar exit flow integration — full pipeline: backup → retention → unlock
- [ ] Net LOC delta is negative

---

## What Dies

- `retention_local.go` — replaced by generic engine
- `retention_r2.go` — replaced by generic engine
- `retention_util.go` + `retention_util_test.go` — absorbed into strategy function
- `session.go` (`CheckPlayersJoined`) + `session_test.go` — replaced by xxhash dirty check
- `mocks/backupper.go` + `mocks/backupper_test.go` — interface simplified
- `mocks/retention.go` — interface simplified
- `molfar.go` `updateManifestsWithArchive` — deleted
- `molfar.go` Exit archive plumbing — simplified
- `Manifest.AddWorld`, `GetLatestWorld`, `RemoveOldestWorlds` — deleted
- `Manifest.Worlds.Backups []World` — deleted
- `domain.World` + `world_test.go` — deleted

## What Lives

- `retention_logs.go` — separate concern, untouched
- `SyncUploadBackupper` — still does delta sync upload (return type simplified to `error`)

## LOC Budget

```
Deleted:   ~956 LOC
Added:     ~300 LOC (Mark is ~40 lines, retention wrapper ~15, backupper ~50, strategies ~20, tests ~175)
Net:       ~-650 LOC
```
