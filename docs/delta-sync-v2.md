# Delta Sync v2.0 — Technical Specification

## Motivation

Minecraft multiplayer requires rotating hosts. Each session transition currently transfers
the entire world as a compressed archive (1GB+). This frequently fails on unstable
connections, leaving the world corrupted with no recovery path for non-technical users.

The current approach has fundamental flaws:
- Full archive upload/download — no scoping, no partial retry
- Compression adds CPU overhead with no benefit for delta workflows
- Failed transfers are not resumable — must restart from zero
- No visibility into what changed between sessions

Delta sync solves this by transferring only changed files, making retries safe and scoped,
and storing raw files that can be individually addressed.

---

## Key Changes from v1

| v1 (current) | v2 (target) |
|---|---|
| Compressed tar archives | Raw files on remote |
| Full world upload/download | File-level delta sync via xxhash diff |
| No retry granularity | Per-file retry with exponential backoff |
| Backup = compressed archive | Backup = raw copy of worlds/ + manifest snapshot |
| Extension-based retention | Timestamp-based retention |
| Stream upload/download (tar pipe) | Individual file put/get via StorageRepository |

**Target: negative LOC.** Compression, streaming, and archive code removed entirely.

---

## Storage Layout

```
./worlds/                                  # active world files (unchanged)
./sync/{lockID}/**                         # temporal staging area
./backups/{timestamp}/manifest.json        # remote manifest at time of backup
./backups/{timestamp}/worlds/**            # raw copy of world files
```

### Sync folder

- Named by lock ID (`hostname__nanosecond_timestamp`) — ties staging to session for debugging
- Created at start of sync operation
- Cleaned after full commit confirmed + R2 retention applied
- If process crashes — stale sync folder persists, cleaned on next successful session

### Backups folder

- Timestamp in folder name — sole discriminator for retention
- Contains remote manifest snapshot at time of creation — enables full state restoration
- Contains raw copy of `./worlds/` — no decompression needed for restore

---

## xxhash Map

### What it is

The manifest stores a map of relative file paths to their xxhash values:

```json
{
  "xxhash_map": {
    "world/region/r.0.0.mca": "a1b2c3d4e5f6",
    "world/region/r.0.1.mca": "f6e5d4c3b2a1",
    "world/level.dat": "1a2b3c4d5e6f"
  },
  "xxhash_sync_at": "2026-04-14T10:30:00Z"
}
```

### Why xxhash

- Fast — optimized for throughput, not cryptographic security
- Deterministic — same file always produces same hash
- Sufficient collision resistance for file comparison

### When it is computed

After server finishes work, before any uploads or backups. Walk over `./worlds/` directory
produces `map[string]string` of `relative/path` → `xxhash`.

### Manifest fields

- `xxhash_map` — the hash map itself
- `xxhash_sync_at` — timestamp of when the map was last computed

`xxhash_sync_at` is distinct from `updated_at`. `updated_at` changes on every manifest
write (lock, unlock, any metadata). `xxhash_sync_at` changes only when worlds were
scanned. The mtime-filtered scanner uses `xxhash_sync_at` to skip unchanged files.

---

## Source of Truth Model

Two rules. No exceptions.

1. **After server stops** → disk is truth → local manifest xxhash map overwritten from walk
2. **After download completes** → remote manifest is truth → local manifest overwritten from remote

No drift detection needed. No integrity checks. Walk happens at exactly the right moments.

**Implication:** if a user manually modifies `./worlds/` between sessions (nostalgia rollback,
external tools), the next server stop produces a walk that captures whatever is on disk.
System does not judge — it syncs truth.

---

## Upload Flow (after server stops)

### Prerequisites

- Server process has exited
- Lock is held by current session

### Setup

1. Walk `./worlds/`, compute xxhash for every file → `new_map`
2. Overwrite local manifest xxhash map with `new_map` (disk is truth)
3. Set local manifest `xxhash_sync_at` to current time
4. Diff `new_map` against remote manifest xxhash map:
   - `upload_set` = keys where hash differs + keys present in `new_map` but absent in remote
   - `delete_set` = keys present in remote but absent in `new_map`

### Phase 1: Upload

Upload each file in `upload_set` to remote `./sync/{lockID}/` staging area.

- Per-file operation via `StorageRepository.Put()`
- Each file retried up to 5 times with exponential backoff (1s base, 15s cap)
- Use retry library — no hand-rolled retry loops
- Emit batch event at start: total file count + total bytes
- Emit per-file event: file path + progress

**If P1 fails after all retries exhausted:** stop. Sync folder persists on remote. Next
session retries. No partial state exposed to other hosts.

### Phase 2: Move

Move each file from remote `./sync/{lockID}/` → remote `./worlds/` with replacement.

- Per-key `Copy(sync_key, worlds_key)` + `Delete(sync_key)` via StorageRepository
- Each operation retried (same 5-attempt, expo backoff strategy)
- After all files moved: update remote manifest xxhash map = `new_map`
- After all files moved: update remote manifest `xxhash_sync_at`

**Ordering matters:** files move first, then manifest updates. If crash after files moved
but before manifest update — next session's diff sees hash mismatch, re-uploads affected
files. Self-correcting.

**If P2 fails:** sync folder may be partially consumed. Next session re-runs full upload
from walk. Files already in `./worlds/` with correct content are skipped by hash match.

### Phase 3: Delete

Delete each key in `delete_set` from remote `./worlds/`.

- Per-key `Delete()` via StorageRepository
- Each operation retried (same strategy)
- Only runs after P2 fully committed

**If P3 fails:** stale files on remote. Not harmful — they are not in the manifest xxhash
map, so no host will download them. Cleaned on next successful P3.

### Post-commit

1. Create backup: copy `./worlds/` + remote manifest → `./backups/{timestamp}/`
2. Clean sync folder: delete `./sync/{lockID}/` from remote
3. Apply retention policies

---

## Download Flow (on startup)

### Prerequisites

- Lock is held by current session
- Remote manifest fetched

### Setup

Diff remote manifest xxhash map against local manifest xxhash map.

- **String comparison only. Zero IO. Zero hashing.**
- `download_set` = keys where hash differs + keys present in remote but absent locally
- `skip_set` = keys where hash matches

If `download_set` is empty — skip download entirely.

### Phase 1: Download

Download each file in `download_set` from remote `./worlds/` → local `./sync/{lockID}/`.

- Per-file operation via `StorageRepository.Get()`
- Each file retried up to 5 times with exponential backoff (1s base, 15s cap)
- Emit batch event at start: total file count
- Emit per-file event: file path + progress

**If P1 fails:** sync folder partial. Next session retries from manifest diff (unchanged
files already on disk, diff produces same or smaller download set).

### Phase 2: Move

Move each file from local `./sync/{lockID}/` → local `./worlds/` with replacement.

- Filesystem move/copy operations
- After all files moved: overwrite local manifest = remote manifest

**Only after P1 fully confirmed.** Prevents premature overrides of world files with
incomplete data.

### Phase 3: Delete local ghosts

Walk local `./worlds/` once. Every file NOT present in local manifest (which now equals
remote manifest) → delete.

- Read-only scan — no hashing, filename existence check only
- Handles files deleted by other hosts since last session

**If P3 fails:** ghost files linger locally. Cleaned next session. Low impact — backups exist.

---

## Deletion Logic

### Principle

Deletions are always the LAST phase (P3). Never delete before upload/download is fully
committed. This is non-negotiable.

### Upload-side deletion

Files deleted from worlds during server runtime are absent from `new_map` after walk.
Remote manifest updated in P2 without those keys. P3 removes actual files from remote
storage.

### Download-side deletion

After local manifest updated to match remote (end of P2), walk catches local files not
mentioned in manifest. Deleted in P3.

### Ghost files

**Remote ghosts** (P3 crash on upload): persist until next successful upload session.
Self-correcting — next session's `new_map` won't include them, P3 retries deletion.
Not harmful — absent from manifest means no host downloads them.

**Local ghosts** (P3 crash on download): persist until next download session. Low priority —
backups exist, next session cleans up. User may accumulate some stale files between
sessions. Acceptable.

### Minecraft deletion behavior

Minecraft server may delete files from worlds during runtime (region file cleanup, session
lock rotation). These deletions are captured naturally by the post-server walk — deleted
files are absent from `new_map`, triggering remote P3 deletion.

---

## Backup v2

### Format

```
./backups/{timestamp}/
  manifest.json    # remote manifest at time of backup
  worlds/          # raw copy of ./worlds/
```

### When created

After upload P2 commits successfully. Before sync folder cleanup.

### How created

`StorageRepository.Copy()` loop over `./worlds/` contents + write manifest.json.
No compression. Raw files only.

### Why include manifest

Enables full state restoration. Pick a timestamp backup → restore remote manifest + remote
worlds from that backup. No guessing which manifest version matches which world files.

### Restoration

Not in v2 scope (issue #13 mentions self-service rollback as future goal). But the backup
format is designed to make it trivial: overwrite `./worlds/` + overwrite remote manifest
from backup's `manifest.json`.

---

## Retry Strategy

### Parameters

- Max attempts: 5 per file
- Backoff: exponential, 1s base
- Cap: 15s maximum delay
- Implementation: retry library (not hand-rolled)

### Where retries apply

- P1 upload: each `Put()` call
- P1 download: each `Get()` call
- P2 move: each `Copy()` + `Delete()` pair
- P3 delete: each `Delete()` call

### Failure escalation

If a single file exhausts all 5 retries → entire phase fails. Sync folder persists.
Next session retries from where the sync can resume (manifest diff produces same or smaller
work set).

---

## Event System

### Batch events

Emitted at start of each phase. Contain total work to be done.

```go
ports.UpdateInfo{
    Operation: "sync-upload",
    Message:   "Starting upload phase",
    Data: map[string]any{
        "total_files": 47,
        "total_bytes": 52428800,
    },
}
```

### Per-file events

Emitted for each file operation. Enable progress tracking.

```go
ports.UpdateInfo{
    Operation: "sync-upload",
    Message:   "Uploading file",
    Data: map[string]any{
        "file":     "world/region/r.0.0.mca",
        "progress": 12,  // file N of total
        "total":    47,
    },
}
```

### Phase transition events

```go
ports.StartInfo{Operation: "sync-upload-p1"}
ports.FinishInfo{Operation: "sync-upload-p1"}
ports.StartInfo{Operation: "sync-upload-p2"}
// ...
```

GUI subscribes to these. Current single-consumer channel pattern sufficient for now.
Future EventBus with multiple subscribers planned for GUI milestone.

---

## Architecture (SOLID)

### Component Responsibilities

| Component | Layer | Single Responsibility |
|---|---|---|
| `WorldScanner` | Port (interface) | Produce xxhash map from worlds directory |
| `FullWorldScanner` | Adapter | Walk + hash every file |
| `MtimeWorldScanner` | Adapter | Walk + hash files modified after threshold, carry forward unchanged |
| `DiffEngine` | Domain (pure function) | Two maps in → three sets out (upload, download, delete) |
| `SyncService` | Service | Orchestrate P1→P2→P3 for both upload and download |
| `LocalBackupper` (rewritten) | Service | Copy worlds + manifest → `./backups/{ts}/` |
| `LibrarianService` (existing) | Service | Read/write manifests — gains xxhash map + sync_at fields |
| `StorageRepository` (existing) | Port | Get/Put/Delete/List/Copy on any storage |

### Port: WorldScanner

```go
type WorldScanner interface {
    Scan(ctx context.Context) (map[string]string, error)
}
```

Clean interface. No parameters for strategy — strategy is baked into the implementation.

Two implementations:

**FullWorldScanner** — walks entire directory, hashes every file. Used when manifest xxhash
map is empty (first run, migration, corruption recovery).

**MtimeWorldScanner** — walks directory, hashes only files with mtime after `xxhash_sync_at`.
Files with older mtime carry forward hash from previous map. Used on subsequent runs when
manifest has existing xxhash map.

Constructor injection determines which scanner is created:

```go
var scanner ports.WorldScanner
if len(manifest.XXHashMap) == 0 {
    scanner = adapters.NewFullWorldScanner(root)
} else {
    scanner = adapters.NewMtimeWorldScanner(root, manifest.XXHashSyncAt, manifest.XXHashMap)
}
```

Factory decision lives in DI wiring. Not in scanner. Not in SyncService.

### Hasher

xxhash baked directly into scanner implementations. No port. No DI.

**Motivation:** algorithm swap is not a realistic scenario. Extra interface adds indirection
without testability or extensibility benefit. Scanner tests use real temp directories —
hashing is fast enough to not require mocking.

### Domain: DiffEngine

Pure function. No interfaces. No IO. No state.

```go
func ComputeDiff(local, remote map[string]string) DiffResult

type DiffResult struct {
    Upload []string   // files to upload (changed + new locally)
    Download []string // files to download (changed + new on remote)
    Delete []string   // files to delete from remote (absent locally)
}
```

**This is the brain of delta sync.** Gets the heaviest testing.

### Service: SyncService

```go
func NewSyncService(
    scanner   ports.WorldScanner,
    local     ports.StorageRepository,
    remote    ports.StorageRepository,
    librarian ports.LibrarianService,
    events    chan<- ports.Event,
) *SyncService
```

Methods:
- `Download(ctx context.Context) error` — startup flow (P1→P2→P3)
- `Upload(ctx context.Context) error` — post-server flow (P1→P2→P3)

SyncService does not know which scanner implementation it has. Does not know whether
storage is R2 or filesystem. Does not know manifest format. Orchestration only.

### Compile-time interface checks

All implementations must include:

```go
var _ ports.WorldScanner = (*FullWorldScanner)(nil)
var _ ports.WorldScanner = (*MtimeWorldScanner)(nil)
var _ ports.SyncService = (*SyncService)(nil)  // if interface exists
```

Follows established codebase pattern (every adapter and service already does this).

### Integration into Molfar lifecycle

- `WorldsUpdater` removed from updaters list
- `SyncService.Download()` takes its place in prepare phase
- `SyncService.Upload()` added to exit phase, runs before backup
- `R2Backupper` replaced by rewritten `LocalBackupper` (raw copy)
- Streamer package removed entirely

---

## Retention Changes

### Current behavior (v1)

Filters backup entries by `.tar` extension. Sorts by filename. Deletes oldest beyond limit.

### Target behavior (v2)

**Timestamp is the sole discriminator.** No extension filtering.

- List all entries in backups directory
- Parse timestamp from entry name
- Entries without valid timestamp are ignored (not counted, not deleted)
- Sort by timestamp, newest first
- Delete entries beyond retention limit

### Backwards compatibility

Old `.tar` backups have valid timestamps in their filenames. New directory backups have
valid timestamps in their folder names. Both are counted and sorted together.

During transition period: mixed formats coexist. Retention ages out old `.tar` files
naturally as new raw backups accumulate.

### Remote retention

Enforced. New format only. No backwards compatibility concern — self-update mechanism
ensures all clients run v2 before sync operations execute.

---

## Backwards Compatibility

### Manifest schema

Additive change only. Two new fields: `xxhash_map` and `xxhash_sync_at`.

- v2 reads v1 manifest → xxhash map nil → triggers full scan + full upload. Safe.
- v1 reads v2 manifest → `json.Unmarshal` ignores unknown fields. Safe.

### Mixed version prevention

Ritual self-update (`RitualUpdater`) already exists. Manifest already supports minimum
version thresholds. v2 manifest sets minimum ritual version → all clients auto-update
before touching worlds. No mixed v1/v2 scenario possible.

### Storage layout

- `./worlds/` — unchanged structure
- `./sync/` — new prefix, v1 ignores it
- `./backups/` — mixed `.tar` (v1) and directories (v2) during transition

---

## Migration (existing users)

Users from pre-v2:

1. Local manifest has no xxhash map → `len(manifest.XXHashMap) == 0`
2. DI wiring selects `FullWorldScanner`
3. Walk `./worlds/`, compute full xxhash map
4. Write as local manifest xxhash map
5. Proceed with normal upload flow
6. Diff against remote (also empty map) → full upload of all files

**No special migration code.** Empty map is the trigger. Standard flow handles it.

---

## Dead Code Removal

**Target: negative LOC.** Code is liability.

### Files to remove

| File | Reason |
|---|---|
| `internal/adapters/streamer/pull.go` | S3 stream download + tar extraction — replaced by per-file download |
| `internal/adapters/streamer/pull_test.go` | Tests for removed code |
| `internal/adapters/streamer/push.go` | Tar archive + S3 stream upload — replaced by per-file upload |
| `internal/adapters/streamer/push_test.go` | Tests for removed code |
| `internal/adapters/streamer/types.go` | `S3StreamDownloader`, `S3StreamUploader` interfaces — no more streaming |
| `internal/adapters/streamer/localwriter.go` | Local tar writer — no more archives |
| `internal/core/services/updater_worlds.go` | Compressed world download — replaced by SyncService.Download |
| `internal/core/services/updater_worlds_test.go` | Tests for removed code |
| `internal/core/services/backupper_r2.go` | Compressed archive upload to R2 — replaced by SyncService.Upload |
| `internal/core/services/backupper_r2_test.go` | Tests for removed code |
| `internal/core/services/backupper_local.go` | Compressed local backup — rewritten as raw copy |
| `internal/core/services/backupper_local_test.go` | Tests rewritten |

### Code to modify

| File | Change |
|---|---|
| `internal/config/config.go` | Remove `BackupExtension = ".tar"` |
| `internal/core/domain/manifest.go` | Add `XXHashMap` + `XXHashSyncAt` fields |
| `internal/core/services/retention_local.go` | Remove extension filter, use timestamp parsing |
| `internal/core/services/retention_r2.go` | Remove extension filter, use timestamp parsing |
| `internal/core/ports/ports.go` | Add `WorldScanner` interface, remove streamer-related ports |
| `internal/adapters/r2.go` | Remove `S3StreamDownloader` implementation, keep `StorageRepository` |
| `cmd/cli/main.go` | Wire SyncService instead of WorldsUpdater + R2Backupper |

### Files that stay

| File | Reason |
|---|---|
| `internal/core/services/updater_instance.go` | Server jar download — not worlds-related |
| `internal/core/services/updater_ritual.go` | Self-update binary — not worlds-related |
| `internal/adapters/r2.go` | `StorageRepository` impl still needed for per-file ops |
| `internal/adapters/fs.go` | Local filesystem storage — still needed |

### Audit requirement

Before implementation: full grep for all imports of streamer package, `BackupExtension`,
tar-related stdlib imports (`archive/tar`, `compress/gzip`). Every reference must be
accounted for — either removed or confirmed unrelated to worlds sync.

---

## Sync Folder Lifecycle

### Naming

Sync folder named by lock ID: `./sync/{hostname}__{nanosecond_timestamp}/`

Lock ID already carries unique hostname + nanosecond precision. Ties staging area to
specific session for debugging. No separate UUID generation needed.

### R2 considerations

R2/S3 is flat object storage. No real directories. `./sync/{lockID}/path/to/file` is
just a key prefix. "Move" = `CopyObject(src, dest)` + `DeleteObject(src)` per key.
No atomic batch move available.

Each copy+delete pair retried independently. If crash between copy and delete — file
exists in both locations. Next session's upload produces clean sync folder, old one
cleaned by retention.

### Cleanup

1. After upload P2 fully committed
2. After backup created
3. After retention applied
4. Then: delete all keys under `./sync/{lockID}/` prefix

If cleanup fails — stale sync folder on remote. Not harmful. Cleaned on next session
or by R2 lifecycle rules if configured.

---

## Test Strategy

### Philosophy

Tests are documentation. Each test name describes a behavior, not an implementation detail.
Table-driven where multiple inputs produce predictable outputs. Mocks for all port
boundaries — follows established codebase pattern.

### DiffEngine (heaviest testing)

Table-driven unit tests. Pure function — no mocks, no IO.

| Test case | Local map | Remote map | Expected |
|---|---|---|---|
| Both empty | `{}` | `{}` | No uploads, downloads, or deletes |
| Empty local, populated remote | `{}` | `{a: h1}` | Download: `[a]` |
| Populated local, empty remote | `{a: h1}` | `{}` | Upload: `[a]` |
| Matching maps | `{a: h1}` | `{a: h1}` | Nothing |
| One file changed | `{a: h1}` | `{a: h2}` | Upload: `[a]` (local wins on upload diff) |
| File deleted locally | `{}` | `{a: h1}` | Delete from remote: `[a]` (upload diff context) |
| File added locally | `{a: h1, b: h2}` | `{a: h1}` | Upload: `[b]` |
| Multiple changes | `{a: h1, b: h3, c: h4}` | `{a: h2, b: h3, d: h5}` | Upload: `[a, c]`, Delete: `[d]` |
| Large map (1000+ entries) | Generated | Generated with 10% diff | Correct sets, reasonable performance |

### SyncService (mock-based unit tests)

Mock `WorldScanner`, `StorageRepository`, `LibrarianService`.

**Upload tests:**
- Happy path: scan → diff → P1 upload → P2 move → P3 delete → backup → cleanup
- P1 failure: verify sync folder persists, no manifest update
- P2 failure: verify sync folder intact, no deletions triggered
- P3 failure: verify manifest already updated, files already moved
- Empty diff: verify no operations executed
- Retry exhaustion: verify error propagation

**Download tests:**
- Happy path: diff → P1 download → P2 move → P3 ghost cleanup
- P1 failure: verify local worlds untouched
- P2 failure: verify sync folder intact
- Empty diff: verify skip (no IO)
- Ghost file detection: files on disk not in manifest → deleted in P3

### WorldScanner (integration tests)

Real temporary directories. Real xxhash computation. No mocks.

**FullWorldScanner:**
- Empty directory → empty map
- Single file → correct path and hash
- Nested directories → correct relative paths
- Large file → correct hash

**MtimeWorldScanner:**
- File modified after threshold → new hash computed
- File unmodified before threshold → hash carried from previous map
- File not in previous map (new) → hash computed regardless of mtime
- File in previous map but missing from disk → omitted from result (deletion)
- Empty previous map → falls back to hashing everything

### Retention (modified)

**Timestamp parsing:**
- Valid timestamp entries → counted and sorted
- Invalid names → ignored
- Mixed formats (`.tar` files + directories) → both counted by timestamp

**Backwards compatibility (one dedicated test):**
- Setup: mix of old `.tar` backups with timestamp names + new directory backups
- Verify: all counted together, sorted by timestamp, oldest deleted first regardless of format

### Compile-time checks

Every implementation file includes `var _ ports.Interface = (*Concrete)(nil)`.
Tests for mocks also include this check (established pattern).

---

## Edge Cases

| Case | Behavior | Phase |
|---|---|---|
| First run, no local manifest | Full download from remote | Download P1 |
| First run, no remote manifest | Full upload after server stop | Upload P1 |
| Network drop during P1 | Retry per-file (5x expo backoff). Fail → sync folder persists | P1 |
| Crash during P2 | Sync folder intact. Next session retries | P2 |
| Crash during P3 | Ghost files self-correct next session | P3 |
| File added during server runtime | Captured by post-server walk → uploaded | Upload setup |
| File deleted during server runtime | Absent from walk → remote P3 deletion | Upload P3 |
| User manually edits worlds between sessions | Next server stop walk captures changes | Upload setup |
| User places old world files manually (rollback) | Walk captures disk state → uploads as truth | Upload setup |
| Empty world (new server, no regions yet) | Empty xxhash map → diff produces full upload on first content | Upload setup |
| Concurrent hosts (should not happen) | Lock prevents. If lock fails → session aborts at precondition | Pre-sync |
| R2 rate limiting | Retry handles transient 429s within 5-attempt budget | All phases |
| Extremely large world (10GB+) | Same flow, more files. Batch event shows total count for progress | All phases |

---

## Acceptance Criteria

### Must have (blocks release)

- [ ] Delta sync reduces transfer size proportional to actual changes (not full world)
- [ ] Failed upload at any phase does not corrupt remote world state
- [ ] Failed download at any phase does not corrupt local world state
- [ ] Per-file retry with exponential backoff (5 attempts, 15s cap)
- [ ] Post-server walk produces correct xxhash map matching disk state
- [ ] MtimeWorldScanner correctly skips unchanged files and carries forward hashes
- [ ] Manifest xxhash map + xxhash_sync_at fields added without breaking v1 manifest reads
- [ ] All clients auto-update to v2 before sync operations execute
- [ ] Existing users migrate seamlessly (empty xxhash map triggers full scan)
- [ ] Backups contain raw worlds/ + manifest snapshot in `./backups/{timestamp}/`
- [ ] Retention sorts by timestamp, not extension — handles mixed v1/v2 backups
- [ ] Streamer package and compressed archive code removed — negative LOC achieved
- [ ] Compile-time interface checks on all new implementations
- [ ] DiffEngine has full table-driven test coverage for all documented cases
- [ ] SyncService has mock-based tests for happy path + each phase failure
- [ ] WorldScanner implementations have integration tests with real temp directories
- [ ] Retention has one backwards compatibility test with mixed backup formats

### Should have (polish, not blocking)

- [ ] Batch + per-file events emitted for GUI observability
- [ ] Sync folder cleanup after successful commit
- [ ] Phase transition events (start/finish per phase)

### Must not

- [ ] Must not leave partial state in `./worlds/` on failure (sync folder absorbs risk)
- [ ] Must not transfer unchanged files (hash match = skip)
- [ ] Must not require manual intervention for migration
- [ ] Must not add net lines of code (dead code removal must offset new code)
- [ ] Must not filter retention by file extension

---

## Implementation Steps

Ordered by dependency. Each step is independently testable and commitable.

### Phase A: Foundation

1. **Add xxhash map fields to manifest domain**
   - Add `XXHashMap map[string]string` and `XXHashSyncAt time.Time` to `domain.Manifest`
   - Verify `json.Unmarshal` of v1 manifest (missing fields) produces nil map + zero time
   - Test: marshal/unmarshal roundtrip with new fields

2. **Implement DiffEngine as pure domain function**
   - `func ComputeDiff(local, remote map[string]string) DiffResult`
   - Full table-driven test suite (all cases from test strategy section)
   - No imports beyond stdlib

3. **Define WorldScanner port**
   - Add interface to `internal/core/ports/ports.go`
   - Add mock to `internal/core/ports/mocks/`

4. **Implement FullWorldScanner adapter**
   - `internal/adapters/fullworldscanner.go`
   - Walk directory + xxhash each file → return map
   - Integration test with real temp directory
   - Compile-time interface check

5. **Implement MtimeWorldScanner adapter**
   - `internal/adapters/mtimeworldscanner.go`
   - Walk + filter by mtime + carry forward from previous map
   - Integration tests: modified files, unmodified files, missing files, new files
   - Compile-time interface check

### Phase B: Sync Service

6. **Implement SyncService.Upload**
   - P1: put files to sync staging
   - P2: copy from sync to worlds on remote + update manifest
   - P3: delete orphaned files from remote
   - Per-file retry with exponential backoff via retry library
   - Mock-based tests for happy path + each phase failure
   - Event emission: batch + per-file

7. **Implement SyncService.Download**
   - P1: get files from remote to local sync staging
   - P2: move from sync to local worlds + update local manifest
   - P3: walk + delete local ghost files
   - Same retry and event strategy as upload
   - Mock-based tests

### Phase C: Backup + Retention

8. **Rewrite LocalBackupper**
   - Copy `./worlds/` + write manifest.json → `./backups/{timestamp}/`
   - Raw files, no compression
   - Test: verify directory structure + manifest content

9. **Modify retention to use timestamp parsing**
   - Remove extension filter from `retention_local.go` and `retention_r2.go`
   - Parse timestamp from entry name
   - Ignore entries without valid timestamp
   - Test: mixed `.tar` + directory formats sorted correctly
   - Backwards compatibility test: old + new formats coexist

### Phase D: Integration + Dead Code Removal

10. **Wire SyncService into Molfar lifecycle**
    - Replace `WorldsUpdater` with `SyncService.Download()` in prepare phase
    - Add `SyncService.Upload()` to exit phase before backup
    - Replace `R2Backupper` with rewritten `LocalBackupper`
    - Scanner selection logic in DI wiring (main.go)

11. **Remove dead code**
    - Delete streamer package entirely (`pull.go`, `push.go`, `types.go`, `localwriter.go` + all tests)
    - Delete `updater_worlds.go` + test
    - Delete `backupper_r2.go` + test
    - Remove `BackupExtension` from config
    - Remove `S3StreamDownloader`/`S3StreamUploader` from R2 adapter
    - Audit: grep for all references to removed code — every import accounted for

12. **Verify negative LOC**
    - `git diff --stat` must show net negative lines
    - If positive: identify what can be further simplified

### Phase E: Migration + Final Validation

13. **Test migration path**
    - Start with v1 state (no xxhash map, compressed backups)
    - Run v2 → verify full scan triggered
    - Verify upload produces correct remote state
    - Verify old `.tar` backups survive retention alongside new format

14. **Integration test: full upload/download cycle**
    - Host A: start server, modify world, stop → upload
    - Host B: start → download → verify world matches Host A's state
    - Host A: delete files, stop → upload with deletions
    - Host B: start → download → verify deletions propagated

15. **Integration test: crash recovery**
    - Simulate crash at each phase boundary
    - Verify retry produces correct final state
    - Verify no corruption in worlds or manifest
