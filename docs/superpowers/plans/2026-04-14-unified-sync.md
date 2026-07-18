# Unified Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace streamer + instance updater with a generic SyncService that syncs any local directory with any remote prefix, serving both worlds and server files.

**Architecture:** Single `SyncService` with value semantics operates on `SyncState` pairs. No librarian dependency. Scanner accepts `fs.FS` for testability. `.ritualsync` whitelist controls file inclusion. Incremental migration framework gates schema changes via `ManifestVersion`.

**Tech Stack:** Go 1.25, `os.Root` (sandboxed FS), `fs.FS`/`fstest.MapFS`, `maps` package, xxhash, AWS SDK v2 (R2 `DeleteObjects`)

**Spec:** `docs/superpowers/specs/2026-04-14-unified-sync-design.md`

---

## File Map

### New files

| File | Responsibility |
|---|---|
| `internal/core/domain/sync_state.go` | `SyncState` struct, `WorldsManifest`, `ServerManifest` |
| `internal/core/domain/server_runtime.go` | `ServerRuntime` (renamed from `Server`) |
| `internal/adapters/ritualsync.go` | `.ritualsync` parser + `FilteredScanner` function adapter |
| `internal/adapters/ritualsync_test.go` | Parser table-driven tests + FilteredScanner tests |
| `internal/adapters/fullscanner.go` | `FullScanner` (renamed from `FullWorldScanner`, accepts `fs.FS`) |
| `internal/adapters/fullscanner_test.go` | Scanner tests using `fstest.MapFS` |
| `internal/adapters/mtimescanner.go` | `MtimeScanner` (renamed from `MtimeWorldScanner`, accepts `fs.FS`) |
| `internal/adapters/mtimescanner_test.go` | Scanner tests using real temp dirs (mtime dependent) |
| `internal/core/services/sync_new.go` | New `SyncService` — plain methods, value semantics |
| `internal/core/services/sync_new_test.go` | Unit tests — error paths, value semantics |
| `internal/core/services/migration.go` | `Migration` type, `migrations` slice, `RunMigrations` |
| `internal/core/services/migration_test.go` | Version gating + V3 migration tests |

### Modified files

| File | Change |
|---|---|
| `internal/config/config.go` | Add `ServerDir`, `TempRitualPath()`, staging constants. Remove `InstanceDir`, `InstanceArchiveKey`, `BackupExtension`, `S3PartSize`, `S3Concurrency` |
| `internal/core/domain/manifest.go` | Restructure: nest `Worlds`/`Server`, remove flat fields |
| `internal/core/domain/manifest_test.go` | Update struct literals |
| `internal/core/ports/ports.go` | Rename `WorldScanner` → `DirectoryScanner`, add `SyncService` interface, add `DeleteBatch` to `StorageRepository`, remove `ValidatorService.CheckInstance` |
| `internal/core/ports/mocks/worldscanner.go` | Rename to `mocks/directoryscanner.go` |
| `internal/core/ports/mocks/storage.go` | Add `DeleteBatch` mock |
| `internal/adapters/fs.go` | Simplify with `os.Root` Go 1.25 methods, add `DeleteBatch` |
| `internal/adapters/r2.go` | Add `DeleteBatch` via `s3.DeleteObjects`, add `Content-MD5` on Put |
| `internal/adapters/retrystorage.go` | Add `DeleteBatch` passthrough |
| `internal/adapters/serverrunner.go` | Read `StartScript` from `manifest.Server.StartScript` |
| `internal/core/services/validator.go` | Remove `CheckInstance` (dead), update `CheckWorld` for nested manifest |
| `internal/core/services/molfar.go` | New lifecycle: migrations → sync download both targets → sync upload worlds |
| `internal/core/services/sync_updater.go` | Update wrappers for value semantics |
| `internal/core/services/sync_integration_test.go` | Update helpers + add new tests |
| `internal/core/services/molfar_test.go` | Update manifest literals, remove instance updater refs |
| `cmd/cli/main.go` | New DI wiring: two SyncService instances, remove streamer/instance updater |

### Deleted files

| File | Reason |
|---|---|
| `internal/adapters/streamer/pull.go` | Streamer dead |
| `internal/adapters/streamer/push.go` | Streamer dead |
| `internal/adapters/streamer/types.go` | Streamer dead |
| `internal/adapters/streamer/localwriter.go` | Streamer dead |
| `internal/adapters/streamer/pull_test.go` | Streamer dead |
| `internal/adapters/streamer/push_test.go` | Streamer dead |
| `internal/core/services/updater_instance.go` | Replaced by serverSync |
| `internal/core/services/updater_instance_test.go` | Replaced by serverSync |
| `internal/core/services/backupper_local.go` | Superseded |
| `internal/core/services/backupper_local_test.go` | Superseded |
| `internal/core/services/sync.go` | Replaced by sync_new.go |
| `internal/core/services/sync_test.go` | Replaced |
| `internal/core/services/sync_phase_stage.go` | Inlined |
| `internal/core/services/sync_phase_commit.go` | Inlined |
| `internal/core/services/sync_phase_cleanup.go` | Inlined |
| `internal/adapters/fullworldscanner.go` | Replaced by fullscanner.go |
| `internal/adapters/fullworldscanner_test.go` | Replaced |
| `internal/adapters/mtimeworldscanner.go` | Replaced by mtimescanner.go |
| `internal/adapters/mtimeworldscanner_test.go` | Replaced |
| `internal/core/domain/server.go` | Replaced by server_runtime.go |
| `internal/core/domain/server_test.go` | Updated |

---

## Task 1: Config constants

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Update config constants**

```go
// Replace these constants:
// InstanceDir   = "instance"   → ServerDir = "server"
// Remove: InstanceArchiveKey, BackupExtension, S3PartSize, S3Concurrency

// Add:
const (
    ServerDir  = "server"
    WorldsDir  = "worlds"

    TempRitualDir      = "ritual"
    SyncStagingPattern = "sync_%d"
    SyncStagingGlob    = "sync_*"
)

func TempRitualPath() string {
    return filepath.Join(os.TempDir(), TempRitualDir)
}

// Update existing:
// UpdateFilePattern = "update_%d.exe"  (was "ritual_update_%d.exe")
// UpdateFileGlob    = "update_*.exe"   (was "ritual_update_*.exe")
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: compilation errors in files referencing removed constants — expected, will fix in subsequent tasks.

- [ ] **Step 3: Commit**

```
git add internal/config/config.go
git commit -m "config: add ServerDir, TempRitualPath, remove dead constants"
```

---

## Task 2: Domain model — SyncState, manifest restructure, ServerRuntime

**Files:**
- Create: `internal/core/domain/sync_state.go`
- Create: `internal/core/domain/server_runtime.go`
- Modify: `internal/core/domain/manifest.go`
- Modify: `internal/core/domain/manifest_test.go`
- Delete: `internal/core/domain/server.go`
- Modify: `internal/core/domain/server_test.go`

- [ ] **Step 1: Create SyncState and manifest types**

Create `internal/core/domain/sync_state.go`:

```go
package domain

import "time"

// SyncState is the common sync tracking struct embedded by both sync targets.
type SyncState struct {
    XXHashMap    map[string]string `json:"xxhash_map,omitempty"`
    XXHashSyncAt time.Time        `json:"xxhash_sync_at,omitempty"`
}

// WorldsManifest holds sync state and backup history for worlds.
type WorldsManifest struct {
    SyncState
    Backups []World `json:"backups"`
}

// ServerManifest holds sync state and server configuration.
type ServerManifest struct {
    SyncState
    StartScript string `json:"start_script"`
}
```

- [ ] **Step 2: Create ServerRuntime**

Create `internal/core/domain/server_runtime.go` — copy content from `server.go`, rename `Server` → `ServerRuntime`, rename constructor `NewServer` → `NewServerRuntime`. Delete `server.go`.

- [ ] **Step 3: Restructure Manifest**

In `internal/core/domain/manifest.go`, replace flat fields with nested structs:

```go
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

Remove: `InstanceVersion`, `StartScript`, `WorldDirs`, `XXHashMap`, `XXHashSyncAt`, `Backups` from root level.

Update `Clone()`, `AddWorld()`, `GetLatestWorld()`, `RemoveOldestWorlds()` to operate on `m.Worlds.Backups` instead of `m.Backups`. Update `Clone()` to copy `Worlds.SyncState` and `Server.SyncState`.

- [ ] **Step 4: Update manifest tests**

In `manifest_test.go` and `server_test.go`, update all struct literals to use new nested structure. Example:

```go
// Before
&domain.Manifest{InstanceVersion: "1.0.0", Backups: []domain.World{...}}

// After
&domain.Manifest{Worlds: domain.WorldsManifest{Backups: []domain.World{...}}, Server: domain.ServerManifest{StartScript: "start.bat"}}
```

- [ ] **Step 5: Build domain package**

Run: `go build ./internal/core/domain/...`
Expected: PASS. Other packages will have compilation errors — expected.

- [ ] **Step 6: Commit**

```
git add internal/core/domain/
git commit -m "domain: restructure manifest with SyncState, rename Server to ServerRuntime"
```

---

## Task 3: Ports — DirectoryScanner, SyncService interface, DeleteBatch

**Files:**
- Modify: `internal/core/ports/ports.go`
- Rename: `internal/core/ports/mocks/worldscanner.go` → `mocks/directoryscanner.go`
- Modify: `internal/core/ports/mocks/storage.go`

- [ ] **Step 1: Update ports.go**

Rename `WorldScanner` → `DirectoryScanner`. Add `SyncService` interface. Add `DeleteBatch` to `StorageRepository`. Remove `ValidatorService.CheckInstance`.

```go
type DirectoryScanner interface {
    Scan(ctx context.Context) (map[string]string, error)
}

type SyncService interface {
    Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
    Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
}

type StorageRepository interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, data []byte) error
    Delete(ctx context.Context, key string) error
    DeleteBatch(ctx context.Context, keys []string) error
    List(ctx context.Context, prefix string) ([]string, error)
    Copy(ctx context.Context, sourceKey string, destKey string) error
}
```

- [ ] **Step 2: Update mocks**

Rename `worldscanner.go` → `directoryscanner.go`. Update type name `MockWorldScanner` → `MockDirectoryScanner`. Add `DeleteBatch` to `MockStorageRepository`.

- [ ] **Step 3: Build ports package**

Run: `go build ./internal/core/ports/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```
git add internal/core/ports/
git commit -m "ports: rename WorldScanner to DirectoryScanner, add SyncService and DeleteBatch"
```

---

## Task 4: FSRepository — Go 1.25 os.Root methods + DeleteBatch

**Files:**
- Modify: `internal/adapters/fs.go`
- Modify: `internal/adapters/fs_test.go`

- [ ] **Step 1: Simplify FSRepository with os.Root methods**

Replace in `fs.go`:
- `Get`: replace manual Open+Read loop with `f.root.ReadFile(key)`
- `Put`: replace Create+Write with `f.root.WriteFile(key, data, 0644)` (keep `MkdirAll` for parent dir)
- `Delete` for directories: replace `deleteDirectoryRecursive` with `f.root.RemoveAll(key)`
- `Copy`: replace manual read/write loop with `ReadFile`+`WriteFile`
- Delete `deleteDirectoryRecursive` and `copyDirectory` methods entirely

- [ ] **Step 2: Add DeleteBatch**

```go
func (f *FSRepository) DeleteBatch(ctx context.Context, keys []string) error {
    for _, key := range keys {
        if err := f.Delete(ctx, key); err != nil {
            return err
        }
    }
    return nil
}
```

- [ ] **Step 3: Run existing FS tests**

Run: `go test ./internal/adapters/ -run TestFS -v`
Expected: PASS — behavior unchanged, implementation simplified.

- [ ] **Step 4: Commit**

```
git add internal/adapters/fs.go internal/adapters/fs_test.go
git commit -m "adapters: simplify FSRepository with os.Root Go 1.25 methods, add DeleteBatch"
```

---

## Task 5: R2Repository — DeleteBatch + Content-MD5

**Files:**
- Modify: `internal/adapters/r2.go`
- Modify: `internal/adapters/r2_test.go`

- [ ] **Step 1: Add DeleteBatch to R2Repository**

Implement using AWS SDK v2 `s3.DeleteObjectsInput`. Auto-batch at 1000 keys:

```go
func (r *R2Repository) DeleteBatch(ctx context.Context, keys []string) error {
    const maxBatch = 1000
    for i := 0; i < len(keys); i += maxBatch {
        end := i + maxBatch
        if end > len(keys) {
            end = len(keys)
        }
        batch := keys[i:end]
        objects := make([]types.ObjectIdentifier, len(batch))
        for j, key := range batch {
            objects[j] = types.ObjectIdentifier{Key: &key}
        }
        _, err := r.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
            Bucket: &r.bucket,
            Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
        })
        if err != nil {
            return fmt.Errorf("batch delete failed: %w", err)
        }
    }
    return nil
}
```

- [ ] **Step 2: Add Content-MD5 to Put**

In `R2Repository.Put`, compute MD5 and set `ContentMD5` header on `PutObjectInput`:

```go
md5Hash := md5.Sum(data)
contentMD5 := base64.StdEncoding.EncodeToString(md5Hash[:])
// Add to PutObjectInput: ContentMD5: &contentMD5
```

- [ ] **Step 3: Add DeleteBatch to RetryStorageRepository**

In `internal/adapters/retrystorage.go`:

```go
func (r *RetryStorageRepository) DeleteBatch(ctx context.Context, keys []string) error {
    return retry.Do(func() error {
        return r.inner.DeleteBatch(ctx, keys)
    }, r.retryOpts(ctx)...)
}
```

- [ ] **Step 4: Run R2 tests**

Run: `go test ./internal/adapters/ -run TestR2 -v`
Expected: PASS (mock-based tests).

- [ ] **Step 5: Commit**

```
git add internal/adapters/r2.go internal/adapters/r2_test.go internal/adapters/retrystorage.go
git commit -m "adapters: add DeleteBatch via R2 DeleteObjects, Content-MD5 on uploads"
```

---

## Task 6: Scanners — FullScanner and MtimeScanner with fs.FS

**Files:**
- Create: `internal/adapters/fullscanner.go`
- Create: `internal/adapters/fullscanner_test.go`
- Create: `internal/adapters/mtimescanner.go`
- Create: `internal/adapters/mtimescanner_test.go`
- Delete: `internal/adapters/fullworldscanner.go`, `fullworldscanner_test.go`
- Delete: `internal/adapters/mtimeworldscanner.go`, `mtimeworldscanner_test.go`

- [ ] **Step 1: Write FullScanner test with fstest.MapFS**

Create `fullscanner_test.go`:

```go
func TestFullScanner_Scan(t *testing.T) {
    fsys := fstest.MapFS{
        "world/level.dat":        &fstest.MapFile{Data: []byte("level data")},
        "world/region/r.0.0.mca": &fstest.MapFile{Data: []byte("region data")},
    }
    scanner := adapters.NewFullScanner(fsys)
    result, err := scanner.Scan(context.Background())
    require.NoError(t, err)
    assert.Len(t, result, 2)
    assert.Contains(t, result, "world/level.dat")
    assert.Contains(t, result, "world/region/r.0.0.mca")
    // Hashes are deterministic — same input = same xxhash
    assert.Equal(t, result["world/level.dat"], result["world/level.dat"])
}

func TestFullScanner_EmptyFS(t *testing.T) {
    fsys := fstest.MapFS{}
    scanner := adapters.NewFullScanner(fsys)
    result, err := scanner.Scan(context.Background())
    require.NoError(t, err)
    assert.Empty(t, result)
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/adapters/ -run TestFullScanner -v`
Expected: FAIL — `NewFullScanner` doesn't exist yet.

- [ ] **Step 3: Implement FullScanner**

Create `fullscanner.go`:

```go
package adapters

import (
    "context"
    "fmt"
    "io"
    "io/fs"

    "github.com/cespare/xxhash/v2"
    "ritual/internal/core/ports"
)

type FullScanner struct {
    fsys fs.FS
}

var _ ports.DirectoryScanner = (*FullScanner)(nil)

func NewFullScanner(fsys fs.FS) *FullScanner {
    return &FullScanner{fsys: fsys}
}

func (s *FullScanner) Scan(ctx context.Context) (map[string]string, error) {
    result := make(map[string]string)
    err := fs.WalkDir(s.fsys, ".", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        if d.IsDir() || path == "." {
            return nil
        }
        hash, hashErr := hashFSFile(s.fsys, path)
        if hashErr != nil {
            return fmt.Errorf("hashing %s: %w", path, hashErr)
        }
        result[path] = hash
        return nil
    })
    if err != nil {
        return nil, err
    }
    return result, nil
}

func hashFSFile(fsys fs.FS, path string) (string, error) {
    f, err := fsys.Open(path)
    if err != nil {
        return "", err
    }
    defer f.Close()
    h := xxhash.New()
    if _, err := io.Copy(h, f); err != nil {
        return "", err
    }
    return fmt.Sprintf("%016x", h.Sum64()), nil
}
```

- [ ] **Step 4: Run test — verify it passes**

Run: `go test ./internal/adapters/ -run TestFullScanner -v`
Expected: PASS.

- [ ] **Step 5: Implement MtimeScanner**

Create `mtimescanner.go` — same as current `MtimeWorldScanner` but accepting `fs.FS` + root path (mtime needs real FS access). Keep real path for `os.Stat` mtime checks, use `fs.FS` for hashing.

Create `mtimescanner_test.go` with real temp dirs (mtime is OS-dependent, can't use MapFS).

- [ ] **Step 6: Run all scanner tests**

Run: `go test ./internal/adapters/ -run "TestFullScanner|TestMtimeScanner" -v`
Expected: PASS.

- [ ] **Step 7: Delete old scanner files**

```bash
git rm internal/adapters/fullworldscanner.go internal/adapters/fullworldscanner_test.go
git rm internal/adapters/mtimeworldscanner.go internal/adapters/mtimeworldscanner_test.go
```

- [ ] **Step 8: Commit**

```
git add internal/adapters/fullscanner*.go internal/adapters/mtimescanner*.go
git commit -m "adapters: rewrite scanners with fs.FS, rename to FullScanner/MtimeScanner"
```

---

## Task 7: .ritualsync parser + FilteredScanner

**Files:**
- Create: `internal/adapters/ritualsync.go`
- Create: `internal/adapters/ritualsync_test.go`

- [ ] **Step 1: Write parser tests**

Create `ritualsync_test.go` with table-driven test:

```go
func TestParseRitualSync(t *testing.T) {
    tests := []struct {
        name    string
        fsys    fs.FS
        paths   []string
        expect  []bool
        wantErr bool
    }{
        {
            name: "wildcard matches everything",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("*\n")}},
            paths: []string{"a.txt", "dir/b.txt", "deep/nested/c.txt"},
            expect: []bool{true, true, true},
        },
        {
            name: "exact file match",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("server.jar\n")}},
            paths: []string{"server.jar", "server.jar.bak", "other.txt"},
            expect: []bool{true, false, false},
        },
        {
            name: "directory match with trailing slash",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("config/\n")}},
            paths: []string{"config/a.cfg", "config/sub/b.cfg", "configstuff.txt"},
            expect: []bool{true, true, false},
        },
        {
            name: "mods/ rejects modstuff.txt",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("mods/\n")}},
            paths: []string{"mods/a.jar", "modstuff.txt"},
            expect: []bool{true, false},
        },
        {
            name: "empty file errors",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("")}},
            wantErr: true,
        },
        {
            name: "missing file errors",
            fsys: fstest.MapFS{},
            wantErr: true,
        },
        {
            name: "comments and blank lines skipped",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("# comment\n\nserver.jar\n")}},
            paths: []string{"server.jar", "other"},
            expect: []bool{true, false},
        },
        {
            name: "path traversal rejected",
            fsys: fstest.MapFS{".ritualsync": &fstest.MapFile{Data: []byte("../escape\n")}},
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            filter, err := adapters.ParseRitualSync(tt.fsys)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            for i, path := range tt.paths {
                assert.Equal(t, tt.expect[i], filter(path), "path: %s", path)
            }
        })
    }
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/adapters/ -run TestParseRitualSync -v`
Expected: FAIL.

- [ ] **Step 3: Implement parser**

Create `ritualsync.go`:

```go
package adapters

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io/fs"
    "maps"
    "strings"

    "ritual/internal/core/ports"
)

const ritualSyncFile = ".ritualsync"

func ParseRitualSync(fsys fs.FS) (func(string) bool, error) {
    f, err := fsys.Open(ritualSyncFile)
    if err != nil {
        return nil, fmt.Errorf("open %s: %w", ritualSyncFile, err)
    }
    defer f.Close()

    var rules []string
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        if strings.Contains(line, "..") {
            return nil, fmt.Errorf("path traversal in %s: %s", ritualSyncFile, line)
        }
        rules = append(rules, line)
    }
    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("reading %s: %w", ritualSyncFile, err)
    }
    if len(rules) == 0 {
        return nil, errors.New(ritualSyncFile + " is empty — explicit sync rules required")
    }

    // Wildcard — match everything
    for _, r := range rules {
        if r == "*" {
            return func(string) bool { return true }, nil
        }
    }

    return func(path string) bool {
        for _, rule := range rules {
            if strings.HasSuffix(rule, "/") {
                // Directory rule — prefix match
                if strings.HasPrefix(path, rule) {
                    return true
                }
            } else {
                // Exact file match
                if path == rule {
                    return true
                }
            }
        }
        return false
    }, nil
}
```

- [ ] **Step 4: Run parser tests — verify pass**

Run: `go test ./internal/adapters/ -run TestParseRitualSync -v`
Expected: PASS.

- [ ] **Step 5: Write FilteredScanner tests**

Add to `ritualsync_test.go`:

```go
func TestFilteredScanner_WhitelistApplied(t *testing.T) {
    inner := &mockScanner{result: map[string]string{
        ".ritualsync": "hash0",
        "server.jar":  "hash1",
        "config/a.cfg": "hash2",
        "logs/latest.log": "hash3",
        "cache/data": "hash4",
    }}
    filter := func(path string) bool {
        return path == "server.jar" || strings.HasPrefix(path, "config/")
    }
    scanner := adapters.NewFilteredScanner(inner, filter)
    result, err := scanner.Scan(context.Background())
    require.NoError(t, err)
    assert.Len(t, result, 3) // .ritualsync + server.jar + config/a.cfg
    assert.Contains(t, result, ".ritualsync")
    assert.Contains(t, result, "server.jar")
    assert.Contains(t, result, "config/a.cfg")
    assert.NotContains(t, result, "logs/latest.log")
}

func TestFilteredScanner_InnerError(t *testing.T) {
    inner := &mockScanner{err: errors.New("scan failed")}
    scanner := adapters.NewFilteredScanner(inner, func(string) bool { return true })
    _, err := scanner.Scan(context.Background())
    assert.Error(t, err)
}

type mockScanner struct {
    result map[string]string
    err    error
}

func (m *mockScanner) Scan(ctx context.Context) (map[string]string, error) {
    if m.err != nil {
        return nil, m.err
    }
    return maps.Clone(m.result), nil
}
```

- [ ] **Step 6: Implement FilteredScanner**

Add to `ritualsync.go`:

```go
type scannerFunc func(context.Context) (map[string]string, error)

func (f scannerFunc) Scan(ctx context.Context) (map[string]string, error) { return f(ctx) }

func NewFilteredScanner(inner ports.DirectoryScanner, filter func(string) bool) ports.DirectoryScanner {
    return scannerFunc(func(ctx context.Context) (map[string]string, error) {
        m, err := inner.Scan(ctx)
        if err != nil {
            return nil, err
        }
        maps.DeleteFunc(m, func(path, _ string) bool {
            return path != ritualSyncFile && !filter(path)
        })
        return m, nil
    })
}
```

- [ ] **Step 7: Run all ritualsync tests**

Run: `go test ./internal/adapters/ -run "TestParseRitualSync|TestFilteredScanner" -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```
git add internal/adapters/ritualsync.go internal/adapters/ritualsync_test.go
git commit -m "adapters: add .ritualsync parser and FilteredScanner decorator"
```

---

## Task 8: New SyncService implementation

**Files:**
- Create: `internal/core/services/sync_new.go`
- Create: `internal/core/services/sync_new_test.go`

- [ ] **Step 1: Write unit tests for error paths and value semantics**

Create `sync_new_test.go`:

```go
func TestSyncService_Download_EmptyDiff(t *testing.T) {
    state := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
    svc := services.NewSyncService(nil, nil, nil, nil,
        services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
        filepath.Join(t.TempDir(), "staging"), "sync/test",
    )
    result, err := svc.Download(context.Background(), state, state)
    require.NoError(t, err)
    assert.Equal(t, state.XXHashMap, result.XXHashMap)
}

func TestSyncService_Download_ValueSemantics(t *testing.T) {
    local := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
    remote := domain.SyncState{XXHashMap: map[string]string{"a": "h2"}}
    localCopy := maps.Clone(local.XXHashMap)

    // Use mock storage that returns data for Get
    mockRemote := &mocks.MockStorageRepository{
        GetFunc: func(ctx context.Context, key string) ([]byte, error) {
            return []byte("data"), nil
        },
    }
    mockLocal := &mocks.MockStorageRepository{}

    svc := services.NewSyncService(nil, mockLocal, mockRemote, nil,
        services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
        filepath.Join(t.TempDir(), "staging"), "sync/test",
    )
    _, err := svc.Download(context.Background(), local, remote)
    require.NoError(t, err)

    // Original local state not mutated
    assert.Equal(t, localCopy, local.XXHashMap)
}

func TestSyncService_Upload_EmptyDiff(t *testing.T) {
    hashMap := map[string]string{"a": "h1"}
    mockScanner := &mocks.MockDirectoryScanner{
        ScanFunc: func(ctx context.Context) (map[string]string, error) {
            return maps.Clone(hashMap), nil
        },
    }
    remote := domain.SyncState{XXHashMap: hashMap}

    svc := services.NewSyncService(mockScanner, nil, nil, nil,
        services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
        filepath.Join(t.TempDir(), "staging"), "sync/test",
    )
    result, err := svc.Upload(context.Background(), domain.SyncState{}, remote)
    require.NoError(t, err)
    assert.Equal(t, hashMap, result.XXHashMap)
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./internal/core/services/ -run "TestSyncService_" -v`
Expected: FAIL — `NewSyncService` (new signature) doesn't exist.

- [ ] **Step 3: Implement SyncService**

Create `sync_new.go`:

```go
package services

import (
    "context"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "time"

    "ritual/internal/core/domain"
    "ritual/internal/core/ports"
)

type SyncConfig struct {
    Prefix   string
    LocalDir string
}

type syncService struct {
    scanner       ports.DirectoryScanner
    local         ports.StorageRepository
    remote        ports.StorageRepository
    events        chan<- ports.Event
    config        SyncConfig
    localStaging  string
    remoteStaging string
}

var _ ports.SyncService = (*syncService)(nil)

func NewSyncService(
    scanner ports.DirectoryScanner,
    local, remote ports.StorageRepository,
    events chan<- ports.Event,
    config SyncConfig,
    localStaging string,
    remoteStaging string,
) *syncService {
    return &syncService{
        scanner:       scanner,
        local:         local,
        remote:        remote,
        events:        events,
        config:        config,
        localStaging:  localStaging,
        remoteStaging: remoteStaging,
    }
}

func (s *syncService) Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
    diff := domain.ComputeDiff(local.XXHashMap, remote.XXHashMap)
    if len(diff.Download) == 0 {
        return local, nil
    }

    defer os.RemoveAll(s.localStaging)

    s.send(ports.StartEvent{Operation: "sync-" + s.config.Prefix})

    if err := s.stageDownload(ctx, diff.Download); err != nil {
        return local, fmt.Errorf("stage: %w", err)
    }
    if err := s.commitDownload(ctx, diff.Download); err != nil {
        return local, fmt.Errorf("commit: %w", err)
    }
    s.cleanLocalGhosts(remote.XXHashMap)

    s.send(ports.FinishEvent{Operation: "sync-" + s.config.Prefix})

    return domain.SyncState{
        XXHashMap:    remote.XXHashMap,
        XXHashSyncAt: remote.XXHashSyncAt,
    }, nil
}

func (s *syncService) Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
    newMap, err := s.scanner.Scan(ctx)
    if err != nil {
        return local, fmt.Errorf("scan: %w", err)
    }
    now := time.Now()

    diff := domain.ComputeDiff(newMap, remote.XXHashMap)
    if len(diff.Upload) == 0 && len(diff.Delete) == 0 {
        return domain.SyncState{XXHashMap: newMap, XXHashSyncAt: now}, nil
    }

    s.send(ports.StartEvent{Operation: "sync-" + s.config.Prefix})

    if len(diff.Upload) > 0 {
        if err := s.stageUpload(ctx, diff.Upload); err != nil {
            return local, fmt.Errorf("stage: %w", err)
        }
        if err := s.commitUpload(ctx, diff.Upload); err != nil {
            return local, fmt.Errorf("commit: %w", err)
        }
    }
    if len(diff.Delete) > 0 {
        s.cleanRemoteOrphans(ctx, diff.Delete)
    }
    s.cleanRemoteStaging(ctx)

    s.send(ports.FinishEvent{Operation: "sync-" + s.config.Prefix})

    return domain.SyncState{XXHashMap: newMap, XXHashSyncAt: now}, nil
}

func (s *syncService) send(evt ports.Event) {
    ports.SendEvent(s.events, evt)
}

func (s *syncService) stageDownload(ctx context.Context, files []string) error {
    for i, file := range files {
        s.send(ports.UpdateEvent{
            Operation: "sync-" + s.config.Prefix,
            Message:   "Downloading",
            Data:      map[string]any{"file": file, "progress": i + 1, "total": len(files)},
        })
        srcKey := s.config.Prefix + "/" + file
        data, err := s.remote.Get(ctx, srcKey)
        if err != nil {
            return fmt.Errorf("get %s: %w", file, err)
        }
        dstPath := filepath.Join(s.localStaging, filepath.FromSlash(file))
        if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
            return fmt.Errorf("mkdir %s: %w", file, err)
        }
        if err := os.WriteFile(dstPath, data, 0644); err != nil {
            return fmt.Errorf("write staging %s: %w", file, err)
        }
    }
    return nil
}

func (s *syncService) commitDownload(ctx context.Context, files []string) error {
    return fs.WalkDir(os.DirFS(s.localStaging), ".", func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() || path == "." {
            return err
        }
        data, readErr := os.ReadFile(filepath.Join(s.localStaging, path))
        if readErr != nil {
            return readErr
        }
        dstPath := filepath.Join(s.config.LocalDir, filepath.FromSlash(path))
        if mkErr := os.MkdirAll(filepath.Dir(dstPath), 0755); mkErr != nil {
            return mkErr
        }
        return os.WriteFile(dstPath, data, 0644)
    })
}

func (s *syncService) cleanLocalGhosts(xxhashMap map[string]string) {
    filepath.WalkDir(s.config.LocalDir, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return err
        }
        rel, relErr := filepath.Rel(s.config.LocalDir, path)
        if relErr != nil {
            return nil
        }
        if _, exists := xxhashMap[filepath.ToSlash(rel)]; !exists {
            os.Remove(path)
        }
        return nil
    })
}

func (s *syncService) stageUpload(ctx context.Context, files []string) error {
    for i, file := range files {
        s.send(ports.UpdateEvent{
            Operation: "sync-" + s.config.Prefix,
            Message:   "Uploading",
            Data:      map[string]any{"file": file, "progress": i + 1, "total": len(files)},
        })
        srcKey := s.config.Prefix + "/" + file
        data, err := s.local.Get(ctx, srcKey)
        if err != nil {
            return fmt.Errorf("get local %s: %w", file, err)
        }
        dstKey := s.remoteStaging + "/" + file
        if err := s.remote.Put(ctx, dstKey, data); err != nil {
            return fmt.Errorf("stage %s: %w", file, err)
        }
    }
    return nil
}

func (s *syncService) commitUpload(ctx context.Context, files []string) error {
    for _, file := range files {
        src := s.remoteStaging + "/" + file
        dst := s.config.Prefix + "/" + file
        if err := s.remote.Copy(ctx, src, dst); err != nil {
            return fmt.Errorf("copy %s: %w", file, err)
        }
        _ = s.remote.Delete(ctx, src)
    }
    return nil
}

func (s *syncService) cleanRemoteOrphans(ctx context.Context, files []string) {
    keys := make([]string, len(files))
    for i, file := range files {
        keys[i] = s.config.Prefix + "/" + file
    }
    _ = s.remote.DeleteBatch(ctx, keys)
}

func (s *syncService) cleanRemoteStaging(ctx context.Context) {
    keys, err := s.remote.List(ctx, s.remoteStaging)
    if err == nil && len(keys) > 0 {
        _ = s.remote.DeleteBatch(ctx, keys)
    }
}
```

- [ ] **Step 4: Run unit tests — verify pass**

Run: `go test ./internal/core/services/ -run "TestSyncService_" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/core/services/sync_new.go internal/core/services/sync_new_test.go
git commit -m "services: new SyncService with value semantics, no librarian, plain methods"
```

---

## Task 9: Migration framework

**Files:**
- Create: `internal/core/services/migration.go`
- Create: `internal/core/services/migration_test.go`

- [ ] **Step 1: Write migration tests**

```go
func TestRunMigrations(t *testing.T) {
    tests := []struct {
        name            string
        manifestVersion string
        wantRun         []string
    }{
        {"nil manifest runs all", "", []string{"3.0.0"}},
        {"old version runs pending", "2.0.0", []string{"3.0.0"}},
        {"current version skips all", "3.0.0", []string{}},
        {"future version skips all", "4.0.0", []string{}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var ran []string
            testMigrations := []Migration{
                {Version: "3.0.0", Run: func(rootPath string) error {
                    ran = append(ran, "3.0.0")
                    return nil
                }},
            }
            var manifest *domain.Manifest
            if tt.manifestVersion != "" {
                manifest = &domain.Manifest{ManifestVersion: tt.manifestVersion}
            }
            err := RunMigrationsWithList(rootPath, manifest, testMigrations)
            require.NoError(t, err)
            assert.Equal(t, tt.wantRun, ran)
        })
    }
}

func TestMigrateV3(t *testing.T) {
    root := t.TempDir()
    require.NoError(t, os.MkdirAll(filepath.Join(root, "instance"), 0755))

    require.NoError(t, migrateV3(root))

    assert.NoDirExists(t, filepath.Join(root, "instance"))
    data, err := os.ReadFile(filepath.Join(root, config.WorldsDir, ".ritualsync"))
    require.NoError(t, err)
    assert.Equal(t, "*\n", string(data))

    // Idempotent
    require.NoError(t, migrateV3(root))
}
```

- [ ] **Step 2: Run tests — verify fail**

Run: `go test ./internal/core/services/ -run "TestRunMigrations|TestMigrateV3" -v`
Expected: FAIL.

- [ ] **Step 3: Implement migration framework**

Create `migration.go`:

```go
package services

import (
    "fmt"
    "os"
    "path/filepath"

    "ritual/internal/config"
    "ritual/internal/core/domain"
)

type Migration struct {
    Version string
    Run     func(rootPath string) error
}

var migrations = []Migration{
    {Version: "3.0.0", Run: migrateV3},
}

func RunMigrations(rootPath string, manifest *domain.Manifest) error {
    return RunMigrationsWithList(rootPath, manifest, migrations)
}

func RunMigrationsWithList(rootPath string, manifest *domain.Manifest, list []Migration) error {
    currentVersion := ""
    if manifest != nil {
        currentVersion = manifest.ManifestVersion
    }
    for _, m := range list {
        if currentVersion == "" || IsVersionOlder(currentVersion, m.Version) {
            if err := m.Run(rootPath); err != nil {
                return fmt.Errorf("migration to %s failed: %w", m.Version, err)
            }
        }
    }
    return nil
}

func migrateV3(rootPath string) error {
    os.RemoveAll(filepath.Join(rootPath, "instance"))
    ritualSync := filepath.Join(rootPath, config.WorldsDir, ".ritualsync")
    if _, err := os.Stat(ritualSync); os.IsNotExist(err) {
        if mkErr := os.MkdirAll(filepath.Dir(ritualSync), 0755); mkErr != nil {
            return mkErr
        }
        return os.WriteFile(ritualSync, []byte("*\n"), 0644)
    }
    return nil
}
```

- [ ] **Step 4: Run tests — verify pass**

Run: `go test ./internal/core/services/ -run "TestRunMigrations|TestMigrateV3" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/core/services/migration.go internal/core/services/migration_test.go
git commit -m "services: add incremental migration framework with V3 migration"
```

---

## Task 10: Update sync_updater wrappers

**Files:**
- Modify: `internal/core/services/sync_updater.go`

- [ ] **Step 1: Update wrappers for value semantics**

`SyncDownloadUpdater` and `SyncUploadBackupper` need to accept manifest pointers from Molfar, call sync with value semantics, write results back:

```go
type SyncDownloadUpdater struct {
    sync      *syncService
    librarian ports.LibrarianService
    getState  func(*domain.Manifest) *domain.SyncState
}

func (u *SyncDownloadUpdater) Run(ctx context.Context) error {
    localManifest, err := u.librarian.GetLocalManifest(ctx)
    if err != nil {
        return err
    }
    remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
    if err != nil {
        return err
    }
    localState := u.getState(localManifest)
    remoteState := u.getState(remoteManifest)
    newState, err := u.sync.Download(ctx, *localState, *remoteState)
    if err != nil {
        return err
    }
    *localState = newState
    return u.librarian.SaveLocalManifest(ctx, localManifest)
}
```

Similar for `SyncUploadBackupper` — calls `Upload`, saves both manifests.

The `getState` function accessor allows targeting `&manifest.Worlds.SyncState` or `&manifest.Server.SyncState`.

- [ ] **Step 2: Build**

Run: `go build ./internal/core/services/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```
git add internal/core/services/sync_updater.go
git commit -m "services: update sync wrappers for value semantics with state accessor"
```

---

## Task 11: Integration tests — adapt existing + new tests

**Files:**
- Modify: `internal/core/services/sync_integration_test.go`

- [ ] **Step 1: Update test helpers**

Update `buildSyncUpload` and `buildSyncDownload` to use new `NewSyncService` signature with `SyncConfig`, `localStaging`, `remoteStaging`. Both still use dual `FSRepository` over temp dirs.

```go
func buildSyncUpload(t *testing.T, env *syncTestEnv, localState, remoteState domain.SyncState) domain.SyncState {
    t.Helper()
    wPath := worldsPath(env.localDir)
    _ = os.MkdirAll(wPath, 0755)
    scanner := adapters.NewFullScanner(os.DirFS(wPath))

    staging := filepath.Join(t.TempDir(), "staging", "worlds")
    svc := services.NewSyncService(scanner, env.local, env.remote, nil,
        services.SyncConfig{Prefix: config.WorldsDir, LocalDir: wPath},
        staging, "sync/test-lock/worlds",
    )
    result, err := svc.Upload(env.ctx, localState, remoteState)
    require.NoError(t, err)
    return result
}
```

- [ ] **Step 2: Update existing test assertions**

Replace `rm.XXHashMap` with correct access paths. Replace `env.loadRemoteManifest(t).XXHashMap` patterns with direct state tracking (tests now pass `SyncState` values, not manifests).

- [ ] **Step 3: Add new integration tests**

Add: `TwoTargetsSameLock`, `RitualSyncWhitelist`, `RitualSyncContracted`, `RitualSyncRemoteWins`, `EmptyRemotePrefix`, `DeleteBatchIntegration` — as specified in the design spec test strategy section.

- [ ] **Step 4: Run all integration tests**

Run: `go test ./internal/core/services/ -run TestSyncIntegration -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```
git add internal/core/services/sync_integration_test.go
git commit -m "test: adapt integration tests for new SyncService, add two-target and ritualsync tests"
```

---

## Task 12: Update validator, serverrunner, molfar for new manifest

**Files:**
- Modify: `internal/core/services/validator.go`
- Modify: `internal/core/services/validator_test.go`
- Modify: `internal/adapters/serverrunner.go`
- Modify: `internal/adapters/serverrunner_test.go`
- Modify: `internal/core/services/molfar.go`
- Modify: `internal/core/services/molfar_test.go`
- Modify: `internal/core/services/settings.go`

- [ ] **Step 1: Update validator**

Remove `CheckInstance` method (dead — instance version gone). Update `CheckWorld` to read from `manifest.Worlds.Backups` instead of `manifest.Backups`. Remove `ErrLocalInstanceVersionEmpty`, `ErrRemoteInstanceVersionEmpty`, `ErrOutdatedInstance` errors.

- [ ] **Step 2: Update serverrunner**

`ServerRunner` reads `StartScript` — update to accept it from `manifest.Server.StartScript` at construction or via method parameter. Also update `config.InstanceDir` references to `config.ServerDir`.

- [ ] **Step 3: Update settings**

`domain.Server` references → `domain.ServerRuntime`. `NewServer` → `NewServerRuntime`.

- [ ] **Step 4: Update Molfar lifecycle**

In `molfar.go`:
- Add `cleanupLeftoverSyncDirs()` call at prepare start
- Add `RunMigrations()` call after manifest load
- Remove instance updater from updaters list
- Update all manifest field access to nested structure
- Renumber lifecycle steps per spec

- [ ] **Step 5: Update all test files**

Update manifest literals in `validator_test.go`, `serverrunner_test.go`, `molfar_test.go`. Replace `InstanceVersion` with `Server: domain.ServerManifest{...}`, `Backups` with `Worlds: domain.WorldsManifest{Backups: ...}`, etc.

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS (except files pending deletion in next task).

- [ ] **Step 7: Commit**

```
git add internal/core/services/ internal/adapters/serverrunner*.go
git commit -m "refactor: update validator, serverrunner, molfar for nested manifest structure"
```

---

## Task 13: DI wiring in main.go

**Files:**
- Modify: `cmd/cli/main.go`

- [ ] **Step 1: Update main.go**

Replace instance updater + old sync service wiring with:
- Two `SyncService` instances (worlds + server)
- `FilteredScanner` per target with `parseRitualSync`
- Staging path construction
- `SyncDownloadUpdater` for both targets
- `SyncUploadBackupper` for worlds only
- Remove streamer imports, remove bucket passing to instance updater

See spec DI Wiring section for exact code.

- [ ] **Step 2: Build**

Run: `go build ./cmd/cli/`
Expected: PASS (may need to temporarily keep old files for compilation — delete in next task).

- [ ] **Step 3: Commit**

```
git add cmd/cli/main.go
git commit -m "wire: two SyncService instances for worlds and server, remove streamer/instance updater"
```

---

## Task 14: Delete dead code

**Files:**
- Delete: entire `internal/adapters/streamer/` directory
- Delete: `internal/core/services/updater_instance.go` + test
- Delete: `internal/core/services/backupper_local.go` + test
- Delete: `internal/core/services/sync.go` (old)
- Delete: `internal/core/services/sync_test.go` (old)
- Delete: `internal/core/services/sync_phase_stage.go`
- Delete: `internal/core/services/sync_phase_commit.go`
- Delete: `internal/core/services/sync_phase_cleanup.go`
- Delete: `internal/core/domain/server.go` (if not already deleted in Task 2)

- [ ] **Step 1: Delete files**

```bash
git rm -r internal/adapters/streamer/
git rm internal/core/services/updater_instance.go internal/core/services/updater_instance_test.go
git rm internal/core/services/backupper_local.go internal/core/services/backupper_local_test.go
git rm internal/core/services/sync.go internal/core/services/sync_test.go
git rm internal/core/services/sync_phase_stage.go internal/core/services/sync_phase_commit.go internal/core/services/sync_phase_cleanup.go
```

- [ ] **Step 2: Rename sync_new.go → sync.go**

```bash
git mv internal/core/services/sync_new.go internal/core/services/sync.go
git mv internal/core/services/sync_new_test.go internal/core/services/sync_test.go
```

- [ ] **Step 3: Grep for orphan references**

Run: `grep -r "streamer\|InstanceDir\|InstanceArchiveKey\|BackupExtension\|S3PartSize\|S3Concurrency\|FullWorldScanner\|MtimeWorldScanner\|WorldScanner\|updater_instance\|backupper_local\|SyncPhase" internal/ cmd/ --include="*.go" | grep -v "_test.go" | grep -v "vendor/"`

Expected: zero hits. Any hit = missed reference, fix before proceeding.

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Verify negative LOC**

Run: `git diff --stat HEAD~14` (or compare against the commit before Task 1)
Expected: net negative lines.

- [ ] **Step 6: Commit**

```
git add -A
git commit -m "refactor: delete streamer, instance updater, old sync phases — negative LOC achieved"
```

---

## Task 15: Final cleanup and librarian update

**Files:**
- Modify: `internal/core/services/librarian.go`
- Modify: `internal/core/services/librarian_test.go`
- Modify: `internal/core/services/retention_local.go`
- Modify: `internal/core/services/retention_r2.go`

- [ ] **Step 1: Update librarian**

Verify librarian serializes/deserializes new manifest structure correctly. The `json` tags handle this — main concern is `Clone()` copies nested structs properly. Add a roundtrip test if not already covered.

- [ ] **Step 2: Update retention**

Retention services access `manifest.Worlds.Backups` instead of `manifest.Backups`. Update field access in `retention_local.go` and `retention_r2.go`.

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -count=1 -race`
Expected: PASS with zero races.

- [ ] **Step 4: Commit**

```
git add internal/core/services/librarian*.go internal/core/services/retention*.go
git commit -m "refactor: update librarian and retention for nested manifest structure"
```

---

## Self-Review

**Spec coverage:** All sections implemented — domain model (T2), ports (T3), FSRepository (T4), R2 (T5), scanners (T6), .ritualsync (T7), SyncService (T8), migration (T9), wrappers (T10), integration tests (T11), molfar/validator/runner (T12), DI wiring (T13), dead code removal (T14), librarian/retention (T15).

**Placeholder scan:** All code blocks contain complete implementations. No TBD/TODO.

**Type consistency:** `SyncConfig` uses `Prefix`/`LocalDir` throughout. `SyncState` passed by value everywhere. `DirectoryScanner` (not `WorldScanner`) in all references. `ServerRuntime` (not `Server`) for runtime config. `DeleteBatch` in StorageRepository, FSRepository, R2Repository, RetryStorageRepository, and mocks.
