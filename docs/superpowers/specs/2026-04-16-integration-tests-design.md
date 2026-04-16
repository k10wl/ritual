# Integration Tests Design

## Goal

Prove the full Ritual pipeline works end-to-end with real filesystem IO.
Unit tests prove stage wiring and sync logic in isolation — integration tests prove
they compose correctly: bytes flow through real files across the full stage chain
and land in the right place.

## Test design principles

These tests are the most-executed in the project and the primary signal for AI agents.
Every design decision optimizes for: **read once, understand immediately, write fast.**

The test file opens with a package godoc that states the rules:

```go
// Package app_test contains integration tests for the full Ritual pipeline.
//
// Rules for writing tests in this file:
//
//   - No comments in test bodies. Code must be self-documenting through
//     function names, variable names, and helper names. If a line needs
//     a comment, the name is wrong.
//
//   - Variable names carry meaning. Use "ritual" not "env", "server" not
//     "srv", "retryServer" not "srv2". Names must be unambiguous when read
//     in isolation — in a stack trace, in a diff, in a grep result.
//
//   - Assertion messages are the sole documentation. They appear in test
//     output on failure — three layers of signal:
//     1. Test function name — what scenario failed.
//     2. Assertion message — what was expected and why.
//     3. Expected vs actual values — what went wrong.
//
//   - Every assertion message must be verbose and meaningful. It should
//     explain the scenario context, not just restate the check. An AI agent
//     reading only the failure output must understand what broke and why.
//
//   - Test function names read as user stories:
//     TestIntegration_PlayedAndExitClean_ChangesUploadedAndBackedUp
//     not TestIntegration_Case8
//
//   - Flat structure. Arrange, act, assert visible in one scroll.
//     Helpers hide plumbing, never intent.
//
//   - No table-driven tests. Each scenario is its own function.
//
//   - Every assertion has a message string. Bare assertions are not allowed.
package app_test
```

### Helpers are vocabulary, not abstraction

Small, named, composable. Each does one thing.

```go
// Seed helpers — describe what exists before the test
seedRemoteWorld(t, ritual, files...)
seedRemoteManifest(t, ritual, manifest)
seedLocalManifest(t, ritual, manifest)

// Fakerun helpers — describe what server does during run
server.write(path, content)
server.delete(path)
server.exit(code)

// Assert helpers — describe what should be true after
ritual.assertRemoteHasFile(t, path, msg)
ritual.assertRemoteFileContent(t, path, expected, msg)
ritual.assertRemoteFileMissing(t, path, msg)
ritual.assertManifestUnlocked(t, msg)
ritual.assertManifestXXHashCount(t, n, msg)
ritual.assertBackupExists(t, msg)
ritual.assertNoBackup(t, msg)
```

## What gets mocked

| Dependency | Mock strategy |
|-----------|---------------|
| R2 remote storage | Second `FSRepository` backed by `t.TempDir()` (proven pattern from `sync_integration_test.go`) |
| CmdBuilder / server process | `cmd/fakerun` — compiled Go binary, reads JSON instructions from stdin, mutates files, exits with specified code |
| Conditions (disk/RAM/Java) | Pass-through `noopCondition` by default; failing condition injected per-test |
| RitualUpdater (self-update) | Excluded from updater list — not relevant to file sync tests |
| Retention | Real retention service backed by FS repos |

Everything else is real: `FSRepository`, `ManifestStore`, `EventBus`, `SyncService`,
`SyncDownloadUpdater`, `SyncUploader`, `FullScanner`, all 9 stage strategies.

## cmd/fakerun

Cross-platform Go binary. Simulates server runtime.

### Interface

```
fakerun --root <dir>
```

Reads newline-delimited JSON from stdin. Each line is one instruction:

```jsonl
{"op":"write","path":"world/playerdata/new.dat","data":"base64-encoded-content"}
{"op":"delete","path":"world/old_chunk.dat"}
{"op":"exit","code":0}
```

### Behavior

- Starts, opens `--root` directory
- Reads stdin line by line, executes each instruction:
  - `write`: creates/overwrites file at `root/path` with decoded content, creates parent dirs
  - `delete`: removes file at `root/path`
  - `exit`: exits with specified code
- Blocks between lines (like a real server sitting idle)
- On stdin EOF: exits 0
- On signal (context cancel / kill): exits immediately

### Testing fakerun itself

`cmd/fakerun/main_test.go` — unit tests for the binary before integration tests depend on it.
Each test runs fakerun against a real `t.TempDir()`:

| Test | Stdin | Assert |
|------|-------|--------|
| Write creates file | `{"op":"write","path":"a/b.txt","data":"aGVsbG8="}` + exit | File exists, content == "hello", parent dirs created |
| Write overwrites existing | Write same path twice with different data + exit | File has second content |
| Delete removes file | Seed file, send delete + exit | File gone |
| Delete missing file — no crash | Send delete for nonexistent path + exit | Exits 0, no error |
| Exit with code 0 | `{"op":"exit","code":0}` | Process exits 0 |
| Exit with code 1 | `{"op":"exit","code":1}` | Process exits 1 |
| Stdin EOF — clean exit | Close stdin without exit op | Process exits 0 |
| Multiple ops in sequence | Write 3 files, delete 1, exit | 2 files remain with correct content |
| Invalid JSON line — error | `not json` | Process exits non-zero or reports error |
| Unknown op — error | `{"op":"unknown"}` | Process exits non-zero or reports error |

Tests use `exec.Command` to run the compiled binary (same `TestMain` build pattern).
These must pass before any integration test is trusted.

### Build

Compiled once per test package via `TestMain`:

```go
func TestMain(m *testing.M) {
    bin, err := buildFakeRun()
    // ...
    fakeRunBin = bin
    os.Exit(m.Run())
}
```

### Test helper

```go
// FakeRunCmdBuilder implements ports.CmdBuilder.
// Exposes Stdin writer so tests can send instructions.
type FakeRunCmdBuilder struct {
    binary string
    root   string
    Stdin  io.WriteCloser
}

func (f *FakeRunCmdBuilder) Build(ctx context.Context) (*exec.Cmd, error) {
    cmd := exec.CommandContext(ctx, f.binary, "--root", f.root)
    r, w := io.Pipe()
    f.Stdin = w
    cmd.Stdin = r
    return cmd, nil
}
```

Test sends mutations via `Stdin`, then writes `{"op":"exit","code":0}` to let server exit.
For cancellation tests: cancel context without sending exit — process killed.

Ties into existing `testhelpers.PaperMinecraftWorldSetup` for seeding realistic file structures.

## Test environment

Reuses the `syncTestEnv` pattern from `sync_integration_test.go`:

```go
type testRitual struct {
    localDir        string
    remoteDir       string
    local           *adapters.FSRepository
    remote          *adapters.FSRepository
    localManifests  ports.ManifestStore
    remoteManifests ports.ManifestStore
    bus             *adapters.EventBus
    ctx             context.Context
    cancel          context.CancelFunc
}
```

Plus helpers:
- `newRitual(t)` — temp dirs, FS repos, manifest stores, event bus
- `seedRemoteWorld(t, ritual)` — uses testhelpers to populate remote with MC world + manifest
- `seedLocalWorld(t, ritual)` — same for local
- `ritual.startRitual(t)` — composes full Ritual, starts Listen goroutine
- `ritual.waitDone(t)` / `ritual.waitFailed(t)` — wait for terminal status

## Test cases

All tests live in `internal/app/ritual_integration_test.go`.
Each test shown as actual Go code — this is the canonical form.

### 1. First launch — no local files

```go
func TestIntegration_FirstLaunch_NoLocalFiles_DownloadsEverything(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level data"),
		file("worlds/world/region/r.0.0.mca", "region data"),
	)
	seedRemoteServer(t, ritual,
		file("server/server.jar", "server binary"),
		file("server/.ritualsync", "*\n"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertLocalHasFile(t, "worlds/world/level.dat",
		"first launch — world files should be downloaded from remote")
	ritual.assertLocalHasFile(t, "server/server.jar",
		"first launch — server files should be downloaded from remote")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after successful first run")
}
```

### 2. Has server, no worlds

```go
func TestIntegration_HasServer_NoWorlds_OnlyWorldsDownloaded(t *testing.T) {
	ritual := newRitual(t)

	seedLocalServer(t, ritual,
		file("server/server.jar", "local jar"),
		file("server/.ritualsync", "*\n"),
	)
	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "remote level"),
	)
	seedRemoteServer(t, ritual,
		file("server/server.jar", "local jar"),
		file("server/.ritualsync", "*\n"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertLocalHasFile(t, "worlds/world/level.dat",
		"worlds should be downloaded — user had none locally")
	ritual.assertLocalFileContent(t, "server/server.jar", []byte("local jar"),
		"server.jar should be untouched — already matched remote")
}
```

### 3. Has worlds, no server

```go
func TestIntegration_HasWorlds_NoServer_OnlyServerDownloaded(t *testing.T) {
	ritual := newRitual(t)

	seedLocalWorld(t, ritual,
		file("worlds/world/level.dat", "local level"),
	)
	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "local level"),
	)
	seedRemoteServer(t, ritual,
		file("server/server.jar", "remote jar"),
		file("server/.ritualsync", "*\n"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertLocalHasFile(t, "server/server.jar",
		"server files should be downloaded — user had none locally")
	ritual.assertLocalFileContent(t, "worlds/world/level.dat", []byte("local level"),
		"worlds should be untouched — already matched remote")
}
```

### 4. Outdated manifest (no xxhash maps)

```go
func TestIntegration_OutdatedManifest_NoXXHash_FullSyncPopulatesMaps(t *testing.T) {
	ritual := newRitual(t)

	seedLocalWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)
	seedLocalManifest(t, ritual, &domain.Manifest{ManifestVersion: "1.0.0"})

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)
	seedRemoteManifest(t, ritual, &domain.Manifest{ManifestVersion: "1.0.0"})

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertManifestXXHashNotEmpty(t,
		"outdated manifest with no xxhash — pipeline should populate maps from actual files")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after migration sync")
}
```

### 5. Conditions fail

```go
func TestIntegration_ConditionFails_NothingTouched(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)

	ritual.startRitualWithConditions(t, failCondition("insufficient disk space"))
	ritual.waitFailed(t)

	ritual.assertLocalFileMissing(t, "worlds/world/level.dat",
		"condition failed at checking — no files should be downloaded")
	ritual.assertManifestUnlocked(t,
		"condition failed before lock — both manifests should remain unlocked")
}
```

### 6. Manifest locked — reject start

```go
func TestIntegration_ManifestLocked_RejectStart(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteManifest(t, ritual, &domain.Manifest{LockedBy: "other-host"})
	ritual.startRitualWithConditions(t, manifestLockCondition(t, ritual))
	ritual.waitFailed(t)

	ritual.assertManifestLockedBy(t, "other-host",
		"remote manifest should still be locked by original host — we should not touch it")
}
```

### 7. Lease expired — allow start

```go
func TestIntegration_LeaseExpired_TakesOverAndCompletes(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)
	seedRemoteManifest(t, ritual, &domain.Manifest{
		LockedBy:    "crashed-host",
		HeartbeatAt: time.Now().Add(-time.Hour),
	})

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertManifestUnlocked(t,
		"stale lock should be taken over and released — crashed host is gone")
}
```

### 8. Played, server exits clean, files changed

```go
func TestIntegration_PlayedAndExitClean_ChangesUploadedAndBackedUp(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "original level"),
		file("worlds/world/region/r.0.0.mca", "region data"),
		file("worlds/world/playerdata/old.dat", "old player"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)

	server.write("worlds/world/level.dat", []byte("modified level"))
	server.write("worlds/world/playerdata/new.dat", []byte("new player"))
	server.exit(0)

	ritual.waitDone(t)

	ritual.assertRemoteFileContent(t, "worlds/world/level.dat", []byte("modified level"),
		"server modified level.dat — remote should reflect the change after publish")
	ritual.assertRemoteHasFile(t, "worlds/world/playerdata/new.dat",
		"server created new player file — should exist on remote after publish")
	ritual.assertRemoteHasFile(t, "worlds/world/region/r.0.0.mca",
		"untouched region file should still exist on remote")

	ritual.assertManifestXXHashCount(t, 4,
		"3 original + 1 new file = 4 entries in xxhash map")
	ritual.assertBackupExists(t,
		"files changed during run — backup should be created with pre-run state")
	ritual.assertBackupFileContent(t, "worlds/world/level.dat", []byte("original level"),
		"backup should preserve pre-mutation content")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared after successful pipeline completion")
}
```

### 9. Server crash (exit non-zero)

```go
func TestIntegration_ServerCrash_NoUploadLockReleased(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "original"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(1)
	ritual.waitFailed(t)

	ritual.assertRemoteFileContent(t, "worlds/world/level.dat", []byte("original"),
		"server crashed — remote should have pre-run content, no upload should happen")
	ritual.assertManifestUnlocked(t,
		"lock must be released even after server crash")
}
```

### 10. Stop mid-game (graceful shutdown)

```go
func TestIntegration_StopMidGame_UploadsCurrentStateLockReleased(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "original"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)

	server.write("worlds/world/level.dat", []byte("mid-game state"))

	ritual.sendStop(t)
	ritual.waitFailed(t)

	ritual.assertRemoteFileContent(t, "worlds/world/level.dat", []byte("mid-game state"),
		"graceful stop — publishing runs with WithoutCancel, should upload current state")
	ritual.assertManifestUnlocked(t,
		"lock must be released after graceful stop")
	ritual.assertNoStagingFiles(t,
		"staging files should be cleaned up after publish")
}
```

### 11. Already synced, no changes

```go
func TestIntegration_AlreadySynced_NoTransfersNoBackup(t *testing.T) {
	ritual := newRitual(t)

	seedSyncedWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertNoBackup(t,
		"no file changes during run — backup should not be created")
	ritual.assertManifestUnlocked(t,
		"lock should be cleared even when no work was done")
}
```

### 12. Fetch fails, retry succeeds

```go
func TestIntegration_FetchFails_RetrySucceeds(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)

	flaky := &failOnceUpdater{}
	server := ritual.fakerun()
	ritual.startRitualWithUpdaters(t, []ports.UpdaterService{flaky}, server)

	ritual.waitFailed(t)

	retryServer := ritual.fakerun()
	ritual.sendRetry(t, retryServer)
	retryServer.exit(0)
	ritual.waitDone(t)

	ritual.assertLocalHasFile(t, "worlds/world/level.dat",
		"retry should re-enter at fetching — file should be downloaded on second attempt")
}
```

### 13. Multi-host handoff

```go
func TestIntegration_MultiHost_AUploads_BDownloadsPlaysUploads(t *testing.T) {
	ritualA := newRitual(t)
	ritualB := newRitualSharingRemote(t, ritualA)

	seedLocalWorld(t, ritualA,
		file("worlds/world/level.dat", "host A level"),
	)

	serverA := ritualA.fakerun()
	ritualA.startRitual(t)
	serverA.write("worlds/world/level.dat", []byte("host A modified"))
	serverA.exit(0)
	ritualA.waitDone(t)

	serverB := ritualB.fakerun()
	ritualB.startRitual(t)
	serverB.write("worlds/world/level.dat", []byte("host B modified"))
	serverB.exit(0)
	ritualB.waitDone(t)

	ritualA.assertRemoteFileContent(t, "worlds/world/level.dat", []byte("host B modified"),
		"remote should have Host B's changes — B was the last to upload")
}
```

### 14. Server mutates files (add + delete + modify)

```go
func TestIntegration_ServerMutatesFiles_AllChangesReflectedOnRemote(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/a.dat", "original a"),
		file("worlds/world/b.dat", "original b"),
		file("worlds/world/c.dat", "original c"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)

	server.write("worlds/world/a.dat", []byte("modified a"))
	server.write("worlds/world/d.dat", []byte("brand new"))
	server.delete("worlds/world/c.dat")
	server.exit(0)

	ritual.waitDone(t)

	ritual.assertRemoteFileContent(t, "worlds/world/a.dat", []byte("modified a"),
		"modified file should have new content on remote")
	ritual.assertRemoteFileContent(t, "worlds/world/b.dat", []byte("original b"),
		"untouched file should remain unchanged")
	ritual.assertRemoteHasFile(t, "worlds/world/d.dat",
		"new file created by server should appear on remote")
	ritual.assertRemoteFileMissing(t, "worlds/world/c.dat",
		"deleted file should be removed from remote")
	ritual.assertManifestXXHashCount(t, 3,
		"3 original - 1 deleted + 1 added = 3 entries in manifest")
}
```

### 15. Backup created after successful run with changes

```go
func TestIntegration_BackupCreated_ContainsPreRunState(t *testing.T) {
	ritual := newRitual(t)

	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "before run"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertBackupExists(t,
		"files changed during run — backup should be created")
	ritual.assertBackupFileContent(t, "worlds/world/level.dat", []byte("before run"),
		"backup should contain pre-run snapshot, not post-mutation content")
	ritual.assertBackupHasManifest(t,
		"backup should contain manifest.json snapshot")
}
```

### 16. Retention prunes old backups

```go
func TestIntegration_RetentionPrunesOldBackups(t *testing.T) {
	ritual := newRitual(t)

	seedBackups(t, ritual, 5)
	seedRemoteWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.write("worlds/world/level.dat", []byte("changed"))
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertBackupCount(t, ritual.retentionLimit(),
		"retention should prune oldest backups, keeping only N most recent")
}
```

### 17. No backup when nothing changed

```go
func TestIntegration_NothingChanged_NoBackupCreated(t *testing.T) {
	ritual := newRitual(t)

	seedSyncedWorld(t, ritual,
		file("worlds/world/level.dat", "level"),
	)

	server := ritual.fakerun()
	ritual.startRitual(t)
	server.exit(0)
	ritual.waitDone(t)

	ritual.assertNoBackup(t,
		"server ran but changed nothing — no backup should be created")
}
```
