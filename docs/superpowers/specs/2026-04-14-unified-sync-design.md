# Unified Sync — Technical Specification

## Motivation

Three problems drive this change:

1. **InstanceUpdater depends on streamer** — leaks adapter-level S3 streaming into service layer. Streamer is marked for removal by delta-sync-v2.
2. **Instance upload requires Cloudflare UI** — no programmatic path to push server files. Admin must manually upload to R2 bucket.
3. **Duplicate sync logic** — worlds use file-by-file delta sync via StorageRepository. Instance uses tar streaming via streamer. Same operation, two code paths.

Solution: one generic `SyncService` that syncs any local directory with any remote prefix. Both worlds and server files use the same code path. Streamer dies. Instance updater dies.

---

## Key Changes from Current State

| Current | Target |
|---|---|
| Streamer package (tar streaming) | Deleted — all I/O through StorageRepository |
| InstanceUpdater (tar download + version check) | Deleted — replaced by serverSync.Download |
| LocalBackupper (tar archive) | Deleted — superseded by delta sync |
| SyncService coupled to worlds (hardcoded prefix, librarian dependency, pointer mutation) | Rewritten — generic, value semantics, no librarian |
| `instance/` directory | Renamed to `server/` |
| `domain.Server` (IP, port, memory) | Renamed to `domain.ServerRuntime` |
| `InstanceVersion` string comparison | Deleted — hash diff detects changes |
| Flat manifest fields (`xxhash_map`, `world_dirs`, `backups` at root) | Nested under `Worlds` and `Server` manifest sections |
| Update files scattered in `os.TempDir()` | Dedicated `ritual/` folder under temp dir |

**Target: ~-1500 net LOC.**

---

## Directory Layout

```
ritual/
  worlds/              <- worldSync target
    .ritualsync        <- "* " (sync everything)
    world/
    world_nether/
  server/              <- serverSync target
    .ritualsync        <- whitelist (server.jar, config/, mods/, etc.)
    server.jar
    server.properties
    start.bat
    config/
    mods/
  manifest.json
  backups/
  logs/
```

Worlds path and server path hardcoded in config. No manifest field needed for directory discovery.

---

## Domain Model

### SyncState (common struct)

```go
type SyncState struct {
    XXHashMap    map[string]string `json:"xxhash_map,omitempty"`
    XXHashSyncAt time.Time        `json:"xxhash_sync_at,omitempty"`
}
```

Embedded by both sync targets. SyncService operates on this — doesn't know what it's syncing.

### Manifest

```go
type WorldsManifest struct {
    SyncState
    Backups []World `json:"backups"`
}

type ServerManifest struct {
    SyncState
    StartScript string `json:"start_script"`
}

type Manifest struct {
    ManifestVersion string    `json:"manifest_version"`
    RitualVersion   string    `json:"ritual_version"`
    LockedBy        string    `json:"locked_by"`
    UpdatedAt       time.Time `json:"updated_at"`

    MinRAMMB       int `json:"min_ram_mb"`
    MinDiskMB      int `json:"min_disk_mb"`
    MinJavaVersion int `json:"min_java_version"`

    Worlds WorldsManifest `json:"worlds"`
    Server ServerManifest `json:"server"`
}
```

Removed from root: `InstanceVersion`, `StartScript`, `WorldDirs`, `XXHashMap`, `XXHashSyncAt`, `Backups`.

`StartScript` moves into `ServerManifest`. Config fields (`Min*`) stay root-level.

### ServerRuntime (renamed from Server)

```go
type ServerRuntime struct {
    Address string
    IP      string
    Port    int
    Memory  int
}
```

Runtime config (IP, port, RAM). No collision with `ServerManifest`.

---

## Ports

### DirectoryScanner (renamed from WorldScanner)

```go
type DirectoryScanner interface {
    Scan(ctx context.Context) (map[string]string, error)
}
```

Same interface, new name. Two implementations: `FullScanner`, `MtimeScanner`. Neither is worlds-specific.

### SyncService interface

```go
type SyncService interface {
    Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
    Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
}
```

Value semantics. Input states not mutated. New state returned. Caller applies to manifest, caller saves.

### StorageRepository (updated)

```go
type StorageRepository interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, data []byte) error
    Delete(ctx context.Context, key string) error
    DeleteBatch(ctx context.Context, keys []string) error  // NEW — R2 batch delete
    List(ctx context.Context, prefix string) ([]string, error)
    Copy(ctx context.Context, sourceKey string, destKey string) error
}
```

`DeleteBatch` uses R2's `DeleteObjects` (up to 1000 keys per call, free). `FSRepository` implements as loop over `os.Remove`. `R2Repository` implements via `s3.DeleteObjects` with auto-batching at 1000.

### Unchanged ports

`LibrarianService`, `ValidatorService`, `BackupperService`, `UpdaterService`, `RetentionService`, `ConditionService`, `ServerRunner`, `CommandExecutor` — no changes.

### Deleted ports

`streamer.S3StreamDownloader`, `streamer.S3StreamUploader` — dead with streamer package.

---

## SyncService Implementation

### SyncConfig

```go
type SyncConfig struct {
    Prefix   string // "worlds" or "server" — used for event names, remote keys
    LocalDir string // absolute path to final destination
}
```

Pure data. No methods, no path derivation. Staging paths injected separately at construction.

### Constructor

```go
type syncService struct {
    scanner       ports.DirectoryScanner
    local         ports.StorageRepository
    remote        ports.StorageRepository
    events        chan<- ports.Event
    config        SyncConfig
    localStaging  string // local temp path, fully resolved
    remoteStaging string // remote staging prefix, fully resolved
}

func NewSyncService(
    scanner ports.DirectoryScanner,
    local, remote ports.StorageRepository,
    events chan<- ports.Event,
    config SyncConfig,
    localStaging string,  // e.g. "{tempRitualPath}/sync_{ts}/worlds"
    remoteStaging string, // e.g. "sync/{lockID}/worlds"
) *syncService
```

No librarian. No manifest. Staging paths resolved by caller at DI time. Service stores strings, uses them directly.

### Download flow

1. `ComputeDiff(local.XXHashMap, remote.XXHashMap)`
2. Empty diff → return `local` unchanged
3. **Stage:** `remote.Get(config.Prefix + "/" + file)` → write to `localStaging/file`
4. **Commit:** walk `localStaging/`, `os.WriteFile` each to `config.LocalDir/file` (overwrites)
5. **Cleanup:** `os.RemoveAll(localStaging)`, delete local ghosts via walk
6. Return new `SyncState` with `remote.XXHashMap` and `remote.XXHashSyncAt`

### Upload flow

1. `scanner.Scan(ctx)` → `newMap`
2. `ComputeDiff(newMap, remote.XXHashMap)`
3. Empty diff → return `SyncState{XXHashMap: newMap, XXHashSyncAt: now}`
4. **Stage:** `local.Get(config.Prefix + "/" + file)` → `remote.Put(remoteStaging + "/" + file)`
5. **Commit:** `remote.Copy(remoteStaging/file, config.Prefix/file)` + `remote.Delete(remoteStaging/file)`
6. **Cleanup:** `remote.DeleteBatch(orphan keys)`, `remote.DeleteBatch(remaining staging keys)`
7. Return new `SyncState{XXHashMap: newMap, XXHashSyncAt: now}`

### No phase structs

Phase interface eliminated. Stage, commit, cleanup are plain private methods on `syncService`. No `Execute`/`Verify`/`Name`. Events emitted inline. See Go stdlib Leverage section for Download example.

SyncState computation happens in SyncService before/after phases. Phases are I/O only.

---

## Go stdlib Leverage

Go 1.25 stdlib eliminates significant custom code.

### `os.Root` methods (Go 1.25) — FSRepository simplification

`FSRepository` already uses `os.Root` but predates Go 1.25 additions. Current manual code replaced by one-liners:

| Current | Replacement | Lines saved |
|---|---|---|
| `Get`: Open + manual read loop | `root.ReadFile(key)` | ~15 |
| `Put`: MkdirAll + Create + Write | `root.MkdirAll` + `root.WriteFile(key, data, perm)` | ~10 |
| `deleteDirectoryRecursive`: 40-line recursive walk | `root.RemoveAll(dir)` | ~40 |
| `copyDirectory`: 65-line recursive copy | Walk + `ReadFile` + `WriteFile` | ~50 |

Net: **~115 lines removed** from `fs.go` alone.

### `os.Root.FS()` for scanner

`root.FS()` returns sandboxed `fs.FS` implementing `fs.StatFS`, `fs.ReadFileFS`, `fs.ReadDirFS`, `fs.ReadLinkFS`. Prevents symlink escapes at kernel level — safer than `os.DirFS` which follows symlinks outside the directory.

Scanner accepts `fs.FS`:

```go
// Production — sandboxed via os.Root
adapters.NewFullScanner(workRoot.FS())

// Test — in-memory, no disk
adapters.NewFullScanner(fstest.MapFS{
    "world/level.dat": &fstest.MapFile{Data: []byte("level")},
})
```

### `fstest.MapFS` for tests

Scanner and FilteredScanner unit tests use in-memory FS. No temp dir creation/cleanup. Eliminates ~50 lines of boilerplate per test file.

Limitation: not concurrent-safe, slow with hundreds of entries. Use for unit tests only; integration tests keep real FS.

### `maps` package

`maps.DeleteFunc` for FilteredScanner filtering. `maps.Clone` for safe copy in value semantics. `maps.Equal` in test assertions.

### No phase structs — plain methods

Phase interface (`Execute`/`Verify`/`Name`) eliminated. Plain methods on `syncService`. `Verify` was no-op on most phases. Events emitted inline. ~200 lines saved.

```go
func (s *syncService) Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
    diff := domain.ComputeDiff(local.XXHashMap, remote.XXHashMap)
    if len(diff.Download) == 0 {
        return local, nil
    }

    defer os.RemoveAll(s.localStaging)

    if err := s.stageDownload(ctx, diff.Download); err != nil {
        return local, err
    }
    if err := s.commitDownload(); err != nil {
        return local, err
    }
    s.cleanLocalGhosts(remote.XXHashMap)

    return domain.SyncState{XXHashMap: remote.XXHashMap, XXHashSyncAt: remote.XXHashSyncAt}, nil
}
```

### FilteredScanner as function adapter

No struct. `maps.DeleteFunc` does the work:

```go
func NewFilteredScanner(inner ports.DirectoryScanner, filter func(string) bool) ports.DirectoryScanner {
    return scannerFunc(func(ctx context.Context) (map[string]string, error) {
        m, err := inner.Scan(ctx)
        if err != nil {
            return nil, err
        }
        maps.DeleteFunc(m, func(path, _ string) bool {
            return path != ".ritualsync" && !filter(path)
        })
        return m, nil
    })
}

type scannerFunc func(context.Context) (map[string]string, error)
func (f scannerFunc) Scan(ctx context.Context) (map[string]string, error) { return f(ctx) }
```

### NOT using `os.CopyFS`

`os.CopyFS` refuses to overwrite existing files (`fs.ErrExist`). Download commit needs overwrite for changed files. Pre-deleting targets creates a crash window where files are missing but replacements haven't arrived.

Use `fs.WalkDir` over staging dir + `os.WriteFile` per file instead. Overwrites naturally, no crash window, one pattern for all cases.

---

## R2 Optimizations

### `DeleteObjects` batch delete

R2 supports batch delete of up to 1000 keys per call via `DeleteObjects` (S3 `POST ?delete`). Deletes are **free** — no per-request charge.

`StorageRepository` interface gains `DeleteBatch`:

```go
type StorageRepository interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, data []byte) error
    Delete(ctx context.Context, key string) error
    DeleteBatch(ctx context.Context, keys []string) error  // NEW
    List(ctx context.Context, prefix string) ([]string, error)
    Copy(ctx context.Context, sourceKey string, destKey string) error
}
```

Used in:
- Cleanup phase: batch delete orphan files from remote (replaces N-loop single deletes)
- Cleanup phase: batch delete staging keys after commit
- Retention: batch delete old backup entries

`FSRepository` (local) implements `DeleteBatch` as a loop over `os.Remove` — no OS batch primitive exists.

`R2Repository` implements via AWS SDK `s3.DeleteObjects` with batching at 1000 keys.

### `Content-MD5` on PutObject

R2 accepts `Content-MD5` header on upload. Server validates integrity — rejects corrupted uploads. Free safety net.

Added to `R2Repository.Put` implementation. No interface change — adapter-internal optimization.

### R2 pricing awareness

| Operation type | Cost | Implication |
|---|---|---|
| Class A (Put, Copy) | $4.50/million | Minimize redundant writes — xxhash diff ensures only changed files transfer |
| Class B (Get, Head, List) | $0.36/million | Cheap — list freely for accurate remote state |
| Delete | Free | Aggressive orphan cleanup costs nothing |
| Egress | Free | Download bandwidth is free |

Manifest-driven diff eliminates need for per-file remote queries (HeadObject, conditional Get). All state lives in manifest — one Get per sync operation.

---

## .ritualsync

### Purpose

Whitelist file controlling which files in a directory participate in sync. Lives inside the synced directory. Gets synced itself.

### Format

```
# worlds/.ritualsync
*

# server/.ritualsync
server.jar
server.properties
start.bat
config/
mods/
```

Rules:
- `*` — sync everything
- No trailing slash — exact file match
- Trailing slash — directory prefix match (all contents)
- `#` lines — comments, ignored
- Blank lines — ignored
- Duplicate entries — deduplicated, no error
- Path with `..` — rejected, error
- Trailing whitespace — trimmed
- Empty file — error
- Missing file — error

`.ritualsync` itself is always included in scan output, exempt from its own filtering.

### Parser

```go
func parseRitualSync(fsys fs.FS) (func(string) bool, error)
```

Reads `.ritualsync` from provided `fs.FS`. Returns filter function. Error if missing or empty. Accepts `fs.FS` for testability with `fstest.MapFS`.

### FilteredScanner

Function adapter — no struct needed. Uses `maps.DeleteFunc` from stdlib:

```go
func NewFilteredScanner(inner ports.DirectoryScanner, filter func(string) bool) ports.DirectoryScanner
```

Both worlds and server use FilteredScanner. Worlds with `*` filter (pass-through). Server with explicit whitelist. See Go stdlib Leverage section for implementation.

### .ritualsync change between sessions

Synced like any other file. Expansion (add `plugins/` to whitelist) → new files appear in hash map → uploaded. Contraction (remove `mods/`) → files absent from hash map → orphan deletion. Remote `.ritualsync` wins on download — no merge.

---

## Temp Directory Structure

All ritual temp artifacts under one dedicated folder:

```
os.TempDir()/
  ritual/
    update_1713091892000000000.exe    <- binary update (existing, path updated)
    sync_1713091892000000000/         <- sync staging
      worlds/
      server/
```

### Config additions

```go
const (
    TempRitualDir      = "ritual"
    SyncStagingPattern = "sync_%d"
    SyncStagingGlob    = "sync_*"
)

func TempRitualPath() string {
    return filepath.Join(os.TempDir(), TempRitualDir)
}
```

Existing `UpdateFilePattern` and `UpdateFileGlob` updated to drop `ritual_` prefix (now redundant — parent folder provides namespace).

### Cleanup

Startup scans `TempRitualPath()` for `sync_*` dirs, removes them. Same pattern as existing `cleanupLeftoverUpdateFile()`.

```go
func cleanupLeftoverSyncDirs() {
    pattern := filepath.Join(config.TempRitualPath(), config.SyncStagingGlob)
    matches, _ := filepath.Glob(pattern)
    for _, match := range matches {
        os.RemoveAll(match)
    }
}
```

---

## Migration

### Version scheme

`ManifestVersion` tracks manifest schema version. Current: `"2.0.0"`. This release: `"3.0.0"`.

`RitualVersion` tracks binary version. Separate concern — binary can update without manifest schema change and vice versa.

### How migration runs

```
Startup
  1. cleanupLeftoverSyncDirs()
  2. Load local manifest (may be nil on first run)
  3. RunMigrations(rootPath, localManifest)
     ├── ManifestVersion < "3.0.0"? → run migrateV3
     ├── Future: ManifestVersion < "4.0.0"? → run migrateV4
     └── Set ManifestVersion = latest, save
  4. RitualUpdater (self-update, may exit)
  5. ... normal prepare flow
```

### `ritual_version` stays root-level

Critical bootstrap field. Old binary reads it from new format manifest — `json.Unmarshal` ignores unknown nested fields. Self-update triggers. Old binary never touches sync/instance code with new manifest.

### Admin deployment order

1. Upload server files to remote `server/` prefix (including `.ritualsync`)
2. Upload `worlds/.ritualsync` (with `*`) to remote `worlds/` prefix
3. Update remote manifest to new format (`ManifestVersion: "3.0.0"`, nested `Worlds`/`Server`)
4. Upload new binary to remote

### V2 → V3 user experience (step by step)

1. Old binary (v2) starts → reads remote manifest → `ritual_version` mismatch → downloads new binary → exits
2. New binary (v3) starts → `cleanupLeftoverSyncDirs()`
3. Loads local manifest → `ManifestVersion: "2.0.0"` (or nil)
4. `RunMigrations` → `migrateV3` executes:
   - `os.RemoveAll(instance/)` — server files stateless, 100MB re-download
   - Creates `worlds/.ritualsync` with `*` if missing
5. Sets `ManifestVersion = "3.0.0"`, saves local manifest
6. `RitualUpdater` → version matches, continues
7. Remote manifest has `Worlds.SyncState` empty → `worldSync.Download` does full download
8. Remote manifest has `Server.SyncState` empty → `serverSync.Download` does full download (populates `server/` dir fresh)
9. Both targets populated. Normal operation from here.

### Fresh install experience

1. New binary starts, no local manifest (nil)
2. `RunMigrations` → all migrations run (idempotent on empty state)
3. `instance/` doesn't exist → `RemoveAll` is no-op
4. `worlds/.ritualsync` created with `*`
5. Remote manifest fetched → both `SyncState` empty → full download of everything
6. Normal operation.

### Future migrations

Append to `migrations` slice. `RunMigrations` runs them in order. Each migration:
- Must be idempotent (safe to re-run)
- Can assume previous migrations completed
- Gets real temp dir in tests for verification

### Incremental migrations

`ManifestVersion` gates migrations. Each migration runs once — after success, `ManifestVersion` advances. Next startup skips completed migrations.

```go
type Migration struct {
    Version string                          // target manifest version after this migration
    Run     func(rootPath string) error     // idempotent migration function
}

var migrations = []Migration{
    {
        Version: "3.0.0",
        Run: func(rootPath string) error {
            // V2 → V3: delete instance/, create worlds/.ritualsync
            os.RemoveAll(filepath.Join(rootPath, "instance"))
            worldsRitualSync := filepath.Join(rootPath, config.WorldsDir, ".ritualsync")
            if _, err := os.Stat(worldsRitualSync); os.IsNotExist(err) {
                os.MkdirAll(filepath.Dir(worldsRitualSync), 0755)
                return os.WriteFile(worldsRitualSync, []byte("*\n"), 0644)
            }
            return nil
        },
    },
    // Future:
    // {Version: "4.0.0", Run: migrateV3toV4},
}

func RunMigrations(rootPath string, manifest *domain.Manifest) error {
    for _, m := range migrations {
        if manifest == nil || IsVersionOlder(manifest.ManifestVersion, m.Version) {
            if err := m.Run(rootPath); err != nil {
                return fmt.Errorf("migration to %s failed: %w", m.Version, err)
            }
        }
    }
    return nil
}
```

- `manifest == nil` (first run / no local manifest) → runs all migrations. Safe — each migration is idempotent.
- After success, caller sets `manifest.ManifestVersion` to latest and saves.
- Migrations are sequential, ordered by version. Each can assume previous migrations ran.
- Adding future migration = append to slice. No other code changes.
- `ManifestVersion` finally has a purpose — was unused until now.
- Version comparison uses existing `IsVersionOlder` (numeric semver, not string comparison).

### Migration tests

**Version gating** — table-driven, no I/O:

```go
func TestRunMigrations(t *testing.T) {
    tests := []struct {
        name            string
        manifestVersion string
        wantRun         []string // migration versions that should execute
    }{
        {"nil manifest runs all", "", []string{"3.0.0"}},
        {"old version runs pending", "2.0.0", []string{"3.0.0"}},
        {"current version skips all", "3.0.0", []string{}},
        {"future version skips all", "4.0.0", []string{}},
    }
}
```

**Each migration function** — real temp dir, real filesystem:

```go
func TestMigrateV3(t *testing.T) {
    root := t.TempDir()
    os.MkdirAll(filepath.Join(root, "instance"), 0755)

    require.NoError(t, migrations[0].Run(root))

    assert.NoDirExists(t, filepath.Join(root, "instance"))
    data, _ := os.ReadFile(filepath.Join(root, "worlds", ".ritualsync"))
    assert.Equal(t, "*\n", string(data))

    // Idempotent
    require.NoError(t, migrations[0].Run(root))
}
```

Two layers: version gating logic tested with table-driven, each migration's filesystem effects tested with real dirs + idempotency check.

---

## DI Wiring (main.go)

```go
// Staging bases — shared across targets, resolved once
localStagingBase := filepath.Join(config.TempRitualPath(), fmt.Sprintf(config.SyncStagingPattern, time.Now().UnixNano()))
remoteStagingBase := "sync/" + lockID

// Scanners — workRoot.FS() for sandboxed access, parseRitualSync takes fs.FS
worldsFS, _ := fs.Sub(workRoot.FS(), config.WorldsDir)
serverFS, _ := fs.Sub(workRoot.FS(), config.ServerDir)

worldScanner := adapters.NewFilteredScanner(
    adapters.NewFullScanner(worldsFS),
    parseRitualSync(worldsFS),
)
serverScanner := adapters.NewFilteredScanner(
    adapters.NewFullScanner(serverFS),
    parseRitualSync(serverFS),
)

// Sync services — same code, different config
worldSync := services.NewSyncService(
    worldScanner, local, retryRemote, events,
    services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
    filepath.Join(localStagingBase, config.WorldsDir),
    remoteStagingBase + "/" + config.WorldsDir,
)
serverSync := services.NewSyncService(
    serverScanner, local, retryRemote, events,
    services.SyncConfig{Prefix: config.ServerDir, LocalDir: serverPath},
    filepath.Join(localStagingBase, config.ServerDir),
    remoteStagingBase + "/" + config.ServerDir,
)

// Updaters — both download on prepare
updaters := []ports.UpdaterService{ritualUpdater, worldSyncDownloader, serverSyncDownloader}

// Backuppers — only worlds upload on exit
backuppers := []ports.BackupperService{worldSyncUploader}
```

Adding a third synced directory = new `SyncConfig` + scanner + staging paths. All path construction at DI level.

---

## Molfar Lifecycle

```
PREPARE
  1. cleanupLeftoverSyncDirs()
  2. Load local manifest
  3. RunMigrations()                   <- version-gated, save ManifestVersion
  4. RitualUpdater                     <- self-update, may exit
  5. Conditions (lock, RAM, disk, java)
  5. serverSync.Download()             <- full or delta
  6. worldSync.Download()              <- full or delta
  7. librarian.SaveLocalManifest()     <- one save, both states

RUN
  ServerRunner                         <- reads Server.StartScript

EXIT
  1. worldSync.Upload()                <- delta
  2. librarian.SaveLocalManifest()
  3. librarian.SaveRemoteManifest()    <- one save, both states
  4. Retentions
```

Server never uploads from Molfar. GUI/pipe handles server push separately.

Manifest saved once per phase covering both sync targets. Current code does separate saves per sync — new code batches.

---

## Dead Code Removal

### Files deleted entirely

| File | Reason |
|---|---|
| `internal/adapters/streamer/pull.go` | Streamer dead |
| `internal/adapters/streamer/push.go` | Streamer dead |
| `internal/adapters/streamer/types.go` | Streamer dead |
| `internal/adapters/streamer/localwriter.go` | Streamer dead |
| All `streamer/*_test.go` | Streamer dead |
| `internal/core/services/updater_instance.go` | Replaced by serverSync.Download |
| `internal/core/services/updater_instance_test.go` | Replaced by serverSync.Download |
| `internal/core/services/backupper_local.go` | Superseded by delta sync |
| `internal/core/services/backupper_local_test.go` | Superseded by delta sync |

### Files rewritten

| File | Change |
|---|---|
| `internal/core/services/sync.go` | New implementation — value semantics, SyncConfig, no librarian, plain methods instead of phase structs |
| `internal/core/services/sync_updater.go` | Updated wrappers for value semantics |

### Files deleted (were phase structs, now inlined)

| File | Reason |
|---|---|
| `internal/core/services/sync_phase_stage.go` | Inlined as private method |
| `internal/core/services/sync_phase_commit.go` | Inlined as private method |
| `internal/core/services/sync_phase_cleanup.go` | Inlined as private method |

### Config constants removed

| Constant | Reason |
|---|---|
| `InstanceDir` | Replaced by `ServerDir` |
| `InstanceArchiveKey` | No more tar |
| `BackupExtension` | No more tar |
| `S3PartSize` | Streamer dead |
| `S3Concurrency` | Streamer dead |

### Renamed

| From | To |
|---|---|
| `domain.Server` | `domain.ServerRuntime` |
| `config.InstanceDir` | `config.ServerDir` |
| `ports.WorldScanner` | `ports.DirectoryScanner` |

---

## LOC Projection

| | Lines |
|---|---|
| Deleted | ~2400 (includes phase structs) |
| Added | ~650 (stdlib replaces custom code) |
| **Net** | **~-1750** |

---

## Test Strategy

Test code is liability. Each test must justify its existence by covering a behavior that no other test covers. Prefer fewer integration tests that cover multiple concerns over many unit tests that each cover one.

### What is NOT tested here

Already covered elsewhere — do not duplicate:

- `ComputeDiff` — exhaustive table-driven tests in domain (existing, untouched)
- Retry behavior — `RetryStorageRepository` tests (existing, untouched)
- Manifest JSON serialization — librarian tests (existing, updated for new struct)
- `os.Root` methods — Go stdlib, not our code
- `maps.DeleteFunc` — Go stdlib
- `StorageRepository.Get/Put/Delete/Copy` — FSRepository + R2Repository tests (existing)

### Layer 1: .ritualsync parser (table-driven, pure function)

Single test function, table-driven. No I/O — parser takes `fs.FS`, testable with `fstest.MapFS`.

```go
func TestParseRitualSync(t *testing.T) {
    tests := []struct{
        name    string
        content string  // .ritualsync file content
        paths   []string // paths to test against filter
        expect  []bool   // match result per path
        wantErr bool
    }{...}
}
```

Cases that matter:

| Input | Why this case exists |
|---|---|
| `*` matches everything | Core behavior — worlds default |
| `server.jar` matches exact, rejects `server.jar.bak` | Exact match vs prefix confusion |
| `config/` matches `config/a.cfg` and `config/sub/b.cfg` | Directory recursion |
| `mods/` rejects `modstuff.txt` | Trailing slash = directory only, not prefix of other names |
| Empty file → error | Prevent accidental full-sync of unfiltered directory |
| Missing `.ritualsync` in FS → error | Explicit intent required |
| Comments `#` and blank lines skipped | Format compliance |
| `../escape` → error | Path traversal prevention |

NOT tested: duplicate entries, trailing whitespace — `strings.TrimSpace` and map dedup are stdlib behavior, not worth a test case.

### Layer 2: FilteredScanner (mock scanner, 2 cases)

Only two cases needed — filter function is already tested by parser tests above:

| Case | What it proves |
|---|---|
| Whitelist filter applied | Inner returns 5 entries, filter allows 2 + `.ritualsync` always passes = 3 in output |
| Inner scanner error propagates | Filter never runs on error |

### Layer 3: SyncService unit tests (mock storage, error paths only)

Integration tests cover happy paths better than mocks. Unit tests exist only for error behavior that's hard to trigger with real FS:

| Case | What it proves |
|---|---|
| Stage failure → error, no commit calls | Partial staging doesn't corrupt target |
| Remote Copy failure during upload commit → error | Staging persists for retry |
| Value semantics: input states unchanged after Download | Pass SyncState by value, verify original untouched |
| Value semantics: input states unchanged after Upload | Same |
| Empty diff → immediate return, no storage calls | No-op is truly no-op |

NOT tested via unit mocks: happy path download, happy path upload, events, cleanup — all covered by integration tests.

### Layer 4: Integration tests (dual FSRepository, real files)

Core of the test suite. Both local and remote are real `FSRepository` backed by `os.Root` over temp dirs. Same `StorageRepository` interface, real file I/O, real hashing, real diff — no network. R2 is not testable without network; `FSRepository` has identical interface behavior, so integration tests prove sync correctness through real I/O.

```go
// Test setup — two real FS repos, two temp dirs
localRoot, _ := os.OpenRoot(t.TempDir())
remoteRoot, _ := os.OpenRoot(t.TempDir())
local, _ := adapters.NewFSRepository(localRoot)
remote, _ := adapters.NewFSRepository(remoteRoot)
```

R2-specific behavior (`DeleteObjects` batch, `Content-MD5` header) tested separately in R2 adapter tests with mocked S3 client. Not duplicated here.

Adapted from existing `sync_integration_test.go` — assertions unchanged, helper signatures updated.

**Existing tests (adapted):**

| Test | What it proves |
|---|---|
| FullUploadThenDownload | End-to-end: files match across hosts after full cycle |
| DeltaUpload_OnlyChangedFiles | Modified file updated, unchanged file untouched on remote |
| FileDeletedLocally_RemovedFromRemote | Orphan cleaned from remote via DeleteBatch |
| FileAddedLocally_UploadedToRemote | New file appears on remote |
| DownloadDeletesLocalGhosts | File not in remote manifest deleted locally after download |
| EmptyDiff_NoTransfers | Matching maps = no I/O |
| SyncFolderCleaned | No staging remnants after success |
| HostA→HostB roundtrip | Multi-host: A uploads, B downloads, B modifies, B uploads, verify remote |

**New tests:**

| Test | What it proves |
|---|---|
| TwoTargetsSameLock | Worlds + server sync in same test with same lockID. Verify `sync/{lockID}/worlds/` and `sync/{lockID}/server/` don't collide. Verify both targets commit independently. |
| RitualSyncWhitelist | Server dir has `server.jar`, `config/a.cfg`, `logs/latest.log`. `.ritualsync` lists `server.jar` + `config/`. Upload → only `server.jar` + `config/a.cfg` on remote. `logs/` excluded. |
| RitualSyncContracted | `.ritualsync` initially has `mods/`. Upload mods. Remove `mods/` from `.ritualsync`. Upload again → mods deleted from remote. Proves whitelist contraction triggers orphan cleanup. |
| RitualSyncRemoteWins | Host A and B have different `.ritualsync`. A uploads first. B downloads → B gets A's `.ritualsync`. B's next upload uses A's rules. Remote is source of truth for sync rules. |
| EmptyRemotePrefix | Download when remote `server/` has zero files. Verify clean empty SyncState returned, no crash, no ghost cleanup errors. |
| MigrationV3 | `instance/` dir exists, no `worlds/.ritualsync`, `ManifestVersion` < "3.0.0". Run `RunMigrations`. Verify `instance/` deleted, `worlds/.ritualsync` created with `*`. Run again — no-op, no error. |
| MigrationSkipsCompleted | `ManifestVersion` = "3.0.0". Run `RunMigrations`. Verify no filesystem changes — migration already applied. |
| DeleteBatchIntegration | Upload 50 files. Delete 40 locally. Upload again → verify DeleteBatch called (not 40 individual deletes). Verify 10 files remain on remote. |

**Explicitly NOT adding:**

| Skipped test | Why |
|---|---|
| Large file count (1000+) | Performance benchmark, not correctness. Add only if perf regression observed. |
| Both syncs fail independently | Molfar error handling, not sync service concern. Tested at Molfar level if needed. |
| Temp staging cleanup on startup | 3-line function calling `filepath.Glob` + `os.RemoveAll`. Testing stdlib. |
| `.ritualsync` expanded (add plugins/) | Subset of DeltaUpload — new files appear in scan, uploaded. Already covered. |
| Missing `.ritualsync` error in integration | Already covered by parser unit test. Scanner wraps parser — if parser errors, scanner errors. |

---

## Implementation Steps

Each step compiles independently. Tests green at every step.

| Step | What |
|---|---|
| 1 | Config: `ServerDir`, `TempRitualPath()`, sync staging constants, update `UpdateFilePattern`/`UpdateFileGlob` |
| 2 | Domain: `SyncState`, `WorldsManifest`, `ServerManifest`, restructure `Manifest`, rename `Server` → `ServerRuntime` — update all test literals |
| 3 | Ports: rename `WorldScanner` → `DirectoryScanner`, add `SyncService` interface |
| 4 | `.ritualsync` parser + `FilteredScanner` decorator + tests |
| 5 | New `SyncService` implementation (plain methods, no phase structs) + unit tests |
| 6 | Integration tests — adapt existing suite + new two-target + .ritualsync edge cases |
| 7 | Migration framework (`RunMigrations` + `migrateV3`) + tests |
| 8 | DI wiring in main.go, Molfar lifecycle changes |
| 9 | Delete dead code (streamer, instance updater, old sync, local backupper) |
| 10 | Verify negative LOC |

---

## Acceptance Criteria

### Must have

- [ ] Single SyncService implementation serves both worlds and server targets
- [ ] Value semantics — input SyncState not mutated, new state returned
- [ ] SyncService has zero librarian/manifest dependency
- [ ] .ritualsync controls file inclusion per directory
- [ ] Missing .ritualsync = error
- [ ] .ritualsync always synced (exempt from own filter)
- [ ] Local staging uses OS temp dir under `ritual/` namespace
- [ ] Remote staging uses `sync/{lockID}/{prefix}/` for target isolation
- [ ] Staging dirs cleaned after success and on startup
- [ ] V2 → V3 migration: delete instance/, create worlds/.ritualsync, full re-download
- [ ] Streamer package deleted entirely
- [ ] InstanceUpdater deleted entirely
- [ ] LocalBackupper deleted entirely
- [ ] Phase structs eliminated — plain methods on syncService
- [ ] `StorageRepository.DeleteBatch` implemented with R2 `DeleteObjects`
- [ ] `Content-MD5` header on R2 uploads for integrity verification
- [ ] Scanner accepts `fs.FS` — testable with `fstest.MapFS`
- [ ] `FSRepository` simplified with Go 1.25 `os.Root` methods (`ReadFile`, `WriteFile`, `RemoveAll`)
- [ ] Net negative LOC
- [ ] All existing integration tests pass with updated signatures
- [ ] Compile-time interface checks on all implementations

### Must not

- [ ] Must not leave partial state in target directory on failure
- [ ] Must not transfer unchanged files (hash match = skip)
- [ ] Must not require manual user intervention for V1 → V2 migration
- [ ] Must not mutate input SyncState values
- [ ] Must not have SyncService depend on LibrarianService or Manifest
- [ ] Must not use phase structs/interface — plain methods only
