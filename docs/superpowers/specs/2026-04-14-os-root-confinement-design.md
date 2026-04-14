# os.Root Confinement — Design Spec

**Date:** 2026-04-14
**Goal:** Eliminate all raw filesystem access. Every file operation goes through `*os.Root` — kernel-enforced boundary, no userspace string checks.

## Principle

`os.Root` uses `openat2` (or platform equivalent) to resolve paths relative to a pinned directory file descriptor. No symlink escape. No TOCTOU gap. No path traversal. The kernel enforces it — not string comparisons.

**Single type everywhere: `*os.Root`.** No custom filesystem interfaces. No `fs.FS` at API boundaries. Consumers call `root.FS()` internally when they need `fs.WalkDir`. This is stdlib — `*os.Root` provides both read (`Open`, `ReadFile`, `Stat`, `FS()`) and write (`Create`, `WriteFile`, `MkdirAll`, `Remove`, `Rename`) through one type.

**Why not `fs.FS`?** Read-only. No write interfaces in `io/fs` (proposal #45757 frozen). Splitting consumers into `fs.FS` vs `*os.Root` adds a seam that buys nothing — all consumers live in same trust boundary.

**Why not custom interfaces?** `*os.Root` is stdlib. Defining `ports.FileSystem` that mirrors `*os.Root` methods is redundant wrapper. Tests use `os.OpenRoot(t.TempDir())` — real kernel confinement, real integration.

### Walk pattern

```go
// All traversal through root.FS() — consumer decides internally
fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
    // path is relative, forward-slash — safe by construction
    f, _ := root.Open(path)  // *os.File, full io.Reader
    root.Remove(path)        // write ops through same root
    return nil
})
```

### References

- [go.dev/blog/osroot](https://go.dev/blog/osroot) — official blog: "Code which calls filepath.Join to combine a fixed directory and an externally-provided filename should use os.Root instead"
- [google/safeopen](https://pkg.go.dev/github.com/google/safeopen) — predecessor, now deprecated: "Go 1.24 introduced os.Root — use it instead"
- [pkg.go.dev/os#Root](https://pkg.go.dev/os#Root) — full API: 24 methods covering read, write, metadata, directory ops
- `root.FS()` satisfies: `fs.FS`, `fs.StatFS`, `fs.ReadFileFS`, `fs.ReadDirFS`, `fs.ReadLinkFS`
- No writable `fs.FS` interface exists in stdlib (proposal #45757 frozen, no successor)

## Scope

### Exempt (legitimate reasons)

| Location | Why exempt |
|----------|-----------|
| `cmd/cli/main.go:47` — `os.MkdirAll(config.RootPath)` | Bootstrap: creates the directory that *becomes* the root. Must exist before `os.OpenRoot`. |
| `services/updater_ritual.go` — all raw ops | Self-update process: reads/writes executable binary in `os.TempDir()`, replaces running exe. Operates outside work root by design. Separate security boundary (temp dir). |
| `internal/adapters/streamer/*` — entire package | Scheduled for removal as obsolete. Not migrating dead code. |
| `internal/core/services/backupper_local.go` | Deprecated (comment in file: "superseded by SyncService"). Depends on streamer. Dies with it. |
| `internal/core/services/updater_instance.go` | Depends on streamer.Pull. Dies with streamer removal. |

### Production files to migrate

#### 1. `cmd/cli/logger.go`
- **Current:** `os.MkdirAll(logsDir)`, `os.Create(logPath)` — already receives `*os.Root` but extracts `.Name()` string
- **Change:** Use `workRoot.MkdirAll(config.LogsDir, ...)` and `workRoot.Create(logPath)` directly
- **Impact:** Logger creation confined to work root

#### 2. `internal/adapters/fullworldscanner.go`
- **Current:** Stores `root string`, uses `filepath.WalkDir(s.root, ...)`, calls `hashFile(path)` which uses `os.Open`
- **Change:** Store `*os.Root` field. Walk via `fs.WalkDir(root.FS(), ".", ...)`. `hashFile` accepts `*os.Root` + relative path, opens via `root.Open(relPath)`
- **Impact:** World scanning confined. Symlink in worlds dir cannot escape.

#### 3. `internal/adapters/mtimeworldscanner.go`
- **Current:** Same as fullworldscanner — `root string`, `filepath.WalkDir`, calls `hashFile` with absolute path
- **Change:** Same pattern — `*os.Root` field, `fs.WalkDir(root.FS(), ...)`, `hashFile` through root
- **Impact:** Mtime-based scanning confined

#### 4. `internal/core/services/sync_phase_cleanup.go`
- **Current:** `worldsRoot string`, `filepath.WalkDir`, `os.Remove(path)`
- **Change:** `*os.Root` field. Walk via `fs.WalkDir(root.FS(), ".", ...)`. Remove via `root.Remove(relPath)`.
- **Impact:** Ghost file cleanup confined

#### 8. `internal/core/domain/settings.go`
- **Current:** `os.ReadFile(path)`, `os.WriteFile(path)` with absolute path from `config.RootPath`
- **Change:** `LoadSettings` and `Save` accept `*os.Root`. Use `root.ReadFile(SettingsFilename)` and `root.WriteFile(SettingsFilename, ...)`.
- **Impact:** Settings read/write confined

#### 9. `internal/core/services/sync.go`
- **Current:** `worldsRoot string` field, passed to cleanup phase
- **Change:** Accept `*os.Root` (worlds-scoped root via `workRoot.OpenRoot("worlds")`), pass to cleanup and scanner
- **Impact:** Sync service fully confined

### Caller changes in `cmd/cli/main.go`
- Create worlds root: `worldsRoot, _ := workRoot.OpenRoot("worlds")` (after instance updater ensures dir exists)
- Pass `worldsRoot` to scanner constructors and sync service
- Pass `workRoot` to streamer configs, settings, logger

### Test files to migrate

All test files using raw `os.*` for file operations within test fixture directories should use `os.OpenRoot(tempDir)` and operate through that root. This includes:

- `internal/core/services/molfar_test.go` — `filepath.Walk` + `os.Open` for verification
- `internal/core/services/updater_instance_test.go` — `filepath.Walk` + `os.Open` for verification
- `internal/core/services/sync_integration_test.go` — `os.Remove` for test manipulation
- `internal/core/services/session_test.go` — `os.Create` for log file setup
- `internal/testhelpers/checksum.go` — `filepath.Walk` + `os.Open` for hash computation
- `internal/testhelpers/paperworldsetup_test.go` — `filepath.Walk` for copy helper
- `internal/adapters/fs_test.go` — `os.Mkdir` for test setup
- `internal/testhelpers/checksum_test.go` — `os.Mkdir` for test setup
- `internal/adapters/commandexecutor_test.go` — `os.RemoveAll` for cleanup

**Test helper pattern:**
```go
func setupTestRoot(t *testing.T) *os.Root {
    t.Helper()
    dir := t.TempDir()
    root, err := os.OpenRoot(dir)
    require.NoError(t, err)
    t.Cleanup(func() { root.Close() })
    return root
}
```

## What dies

- All `workRoot.Name()` extractions for building absolute paths
- All `filepath.Join(rootPath, ...)` patterns that construct unconfined paths
- All `os.Open/Create/Remove/Stat/MkdirAll` calls (except exempt bootstrap + self-update)
- No custom filesystem interfaces needed — `*os.Root` is the interface

## API changes summary

```go
// Scanners: string → *os.Root
NewFullWorldScanner(root *os.Root) 
NewMtimeWorldScanner(root *os.Root, since time.Time, previous map[string]string)

// hashFile: path string → root-confined
hashFile(root *os.Root, relPath string) (string, error)

// Settings: static path → root-confined
LoadSettings(root *os.Root) (*Settings, error)
func (s *Settings) Save(root *os.Root) error

// SyncService: string → *os.Root
NewSyncService(..., worldsRoot *os.Root, ...)

// CleanupDownloadPhase: string → *os.Root
type CleanupDownloadPhase struct {
    worldsRoot *os.Root
    ...
}
```

## Known CVEs and gotchas

| Issue | Impact | Status |
|-------|--------|--------|
| CVE-2025-22873 (#73555) | `Root.Open("../")` escaped to parent | Fixed Go 1.24.3 |
| CVE-2025-0913 (GO-2025-3750) | `O_CREATE\|O_EXCL` followed dangling symlinks on Windows | Fixed Go 1.24.4 |
| CVE-2026-32282 (#78293) | `Root.Chmod` TOCTOU race on Linux | Fixed Go 1.27. Not relevant — project doesn't Chmod through root |
| #73077 | `openat2` with `RESOLVE_BENEATH` not yet used on Linux | Open. Current impl uses userspace `openat` loop — still safe, slightly less efficient |
| `Root.Symlink` | Does NOT validate target — can point outside root | Not relevant — project doesn't create symlinks |
| Windows reserved names | `NUL`, `COM1` etc. rejected by `os.Root` | Positive — extra safety on target platform |

## Risk

- `fs.WalkDir(root.FS(), ...)` returns `fs.DirEntry` not `os.FileInfo`. Callers needing `FileInfo` must call `d.Info()`.
- `root.FS()` paths are always forward-slash relative. `filepath.FromSlash` not needed inside walk callbacks.
- Windows: `os.Root` on Windows uses a directory handle. Confirmed supported in Go 1.25.
- `root.Open()` returns `*os.File` (not `fs.File`) — full `io.Reader` support for hash operations.
- Performance: paths with many `..` components are expensive in `os.Root`. `filepath.Clean` inputs before passing if sourced externally.
