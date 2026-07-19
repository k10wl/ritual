# Live Sync During Server Running

## Problem

World sync only happens after server stops (Publishing stage). If server crashes or machine dies mid-session, all world progress since Fetching is lost.

## Solution

Periodic live sync during Running stage. Every tick interval: force server save via stdin, upload changed files to remote storage, refresh heartbeat — all in one tick cycle. Sync interval reuses existing lease heartbeat interval from manifest.

## CmdBuilder Changes

`Build()` accepts IO interfaces:

```go
type CmdBuilder interface {
    Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error)
}
```

Builder assigns `cmd.Stdin = stdin`, `cmd.Stdout = stdout`, `cmd.Stderr = stdout` (merged). Caller creates pipes, passes interfaces, keeps the other ends.

## New Port: ReadinessCheck

```go
type ReadinessCheck interface {
    Wait(ctx context.Context) error
}
```

Injected into Running stage. Implementation decides how to verify server is accepting connections. Composition root wires the concrete implementation with address from manifest.

## Running Stage

Single source of truth for server lifecycle. Only Running stage publishes server events. Only Running stage writes to stdin.

### Process Lifecycle

```go
stdinR, stdinW := io.Pipe()
outR, outW := io.Pipe()

cmd, err := s.cmd.Build(ctx, stdinR, outW)
cmd.Start()

go scanOutput(outR, rs.Bus)              // bufio.Scanner → ServerOutputInfo per line
go s.readiness.Wait(ctx) → ServerReadyInfo
go listenCommands(ctx, stdinW, rs.Bus)   // handles SaveRequested, shutdown

Bus.Publish(ServerStartingInfo{})

err = cmd.Wait()
outW.Close()

if err != nil {
    Bus.Publish(ServerCrashedInfo{Err: err})
    return s.onCrash, nil   // → Unlocking
}
Bus.Publish(ServerStoppedInfo{})
return s.onNext, nil        // → Archiving → Publishing → Unlocking → Retaining
```

### Stdin Command Handler

Running stage owns all stdin writes. Bus-driven:

```go
// listenCommands goroutine
case SaveRequested:
    stdinW.Write([]byte("save-all flush\n"))
    if waitForLine(outCh, "Saved the game", 30*time.Second) {
        bus.Publish(SaveCompleted{})
    }

case <-ctx.Done():
    stdinW.Write([]byte("stop\n"))
```

`save-all flush` freezes the server tick until all chunks are written to disk. While frozen, only save-related output appears. `Saved the game` is a clean, unambiguous signal that flush completed and files are safe to read.

`waitForLine` listens on a channel fed by the stdout scanner. Running stage owns both stdin and stdout. 30 second timeout — if exceeded, `SaveCompleted` is withheld, supervisor skips sync for this tick.

`save-all flush` is the only command needed — synchronous, complete, sufficient.

### Stdout Scanner

```go
go func() {
    sc := bufio.NewScanner(outR)
    for sc.Scan() {
        rs.Bus.Publish(ServerOutputInfo{Line: sc.Text()})
    }
}()
```

Captures all process output (stdout+stderr merged via builder). Java version errors, server logs, crash output — all observable via bus.

### Graceful Shutdown

```
ctx cancelled (user stop / OS signal)
  → listenCommands writes "stop\n" to stdin
  → Java graceful shutdown
  → stdout: "Stopping the server" (consistent 1.12→1.21, vanilla+Forge)
  → Java saves worlds, exits
  → cmd.Wait() returns
  → pipeline continues
```

30 second timeout after `stop` — force kill if Java hangs.

### Crash Path

`cmd.Wait()` returns with error → `ServerCrashedInfo` → Running routes to Unlocking directly. Local files may be mid-write, so Archiving and Publishing are bypassed.

New route in `buildChain()`: Running gets `onCrash` pointing to Unlocking.

## Supervisor Changes

Same supervisor, wider scope. Publishes `SaveRequested` when server is running, handles sync after `SaveCompleted`.

### Event Handling

```
LockAcquiredInfo  → start tick loop
ServerReadyInfo   → s.syncCtx, s.syncCancel = context.WithCancel(bg)
ServerOutputInfo "Stopping the server" → s.syncCancel()
ServerStoppedInfo → s.syncCancel() (safety net for crashes)
LockReleasedInfo  → stop everything
```

Server lifecycle = context lifecycle.

### Tick

```go
func (s *Supervisor) tick(ctx, runID) {
    // lock check — always
    remote, _ := s.remoteStore.Get(ctx)
    if remote.LockedBy != runID { publish LockLost; return }

    // heartbeat — always, synchronous
    remote.HeartbeatAt = time.Now()
    s.remoteStore.Save(ctx, remote)

    // sync — when server is running and previous sync finished
    if s.syncCtx.Err() != nil { return }

    go func() {
        s.bus.Publish(SaveRequested{})
        // wait for SaveCompleted

        local, _ := s.localStore.Get(s.syncCtx)
        remote, _ := s.remoteStore.Get(s.syncCtx)
        newState, _ := s.syncer.Upload(s.syncCtx, local.Worlds.SyncState, remote.Worlds.SyncState)

        local.Worlds.SyncState = newState
        remote.Worlds.SyncState = newState
        remote.HeartbeatAt = time.Now()

        s.localStore.Save(s.syncCtx, local)
        s.remoteStore.Save(s.syncCtx, remote)
    }()
}
```

Key properties:
- Heartbeat runs every tick, synchronous
- Sync context derived from server lifecycle — cancelled when server stops
- Sync goroutine uses `syncCtx` — aborts if server dies mid-sync
- Sync runs in background goroutine
- Previous sync still running → tick syncs heartbeat only
- Supervisor publishes `SaveRequested` — Running stage handles stdin
- Fixed sync cost per tick: 2 reads, 2 writes, 1 upload diff
- Local manifest saved first (survives internet failure)

## Crash Recovery: Local Newer Than Remote

Scenario: playing, internet drops, sync ticks fail, server stops. Next run: Fetching would overwrite newer local with stale remote.

Guard in `SyncDownloadUpdater.Run()`:

```go
if local.XXHashSyncAt.After(remote.XXHashSyncAt) {
    return nil  // local is newer, keep local
}
```

Works because supervisor saves local manifest first (always succeeds — local disk), then remote (may fail — network).

## Backups and Retention

Post-exit only:

```
Running → Archiving → Publishing → Unlocking → Retaining
```

Live sync = durability (minimize loss window). Backups = recovery (point-in-time snapshots). Separate concerns.

## Error Handling

**Network errors:** remote storage adapter retries. All retries exhausted → tick saves heartbeat only → next tick retries full diff. Server keeps running.

**Staged commits:** `SyncService.Upload()` uses stage-then-commit. Network dies mid-stage → staging has partial files, final prefix retains last good state. Next tick cleans staging, redoes diff.

**Internet down entire session:** All ticks save heartbeat only. Publishing catches up post-exit. If Publishing also fails, local state preserved. Next run recovers via `XXHashSyncAt` guard.

## New Event Types (ports/events.go)

- `ServerStartingInfo{}` — process launched, readiness check pending
- `ServerReadyInfo{}` — readiness check passed
- `ServerOutputInfo{Line string}` — single stdout/stderr line
- `ServerStoppedInfo{}` — clean exit
- `ServerCrashedInfo{Err error}` — exit with error
- `SaveRequested{}` — supervisor asks Running to flush
- `SaveCompleted{}` — Running confirms flush done

## What Changes

| Component | Change |
|-----------|--------|
| `CmdBuilder` interface | `Build(ctx)` → `Build(ctx, stdin io.Reader, stdout io.Writer)` |
| `ServerCmdBuilder` adapter | Accept IO interfaces, assign to cmd |
| Running stage | Start/pipe/Wait, goroutines for scanner/readiness/commands, crash route |
| Heartbeat supervisor | Add sync, dual manifest stores, goroutine tick, syncCtx lifecycle |
| Event types | 7 new event types |
| `buildChain()` | Add `onCrash` route Running → Unlocking |
| `SyncDownloadUpdater` | `XXHashSyncAt` guard |
| `fakerun` | Handle plain-text stdin: `save-all flush`, `stop`. Echo server output. |
| Existing integration tests | Adapt to stdin-based shutdown and fakerun changes |

## New Ports

| Port | Purpose |
|------|---------|
| `ReadinessCheck` | Server readiness verification, injected into Running stage |

## Preserved Interfaces

- `Strategy[RunState]`
- `UpdaterService`
- `EventBus`
- `ManifestStore`
- `DirectoryScanner`
- `SyncService` — used as-is
- `SyncUploader` — still used by Publishing post-exit
- Archiving, Publishing, Unlocking, Retaining stages

## Estimated Size

~180 lines production, ~420 lines tests. ~600 total.

| Component | Production | Tests |
|-----------|-----------|-------|
| Running stage rework | ~55 | ~70 |
| Supervisor changes | ~60 | ~100 |
| Event types | ~25 | — |
| ReadinessCheck port + adapter | ~15 | ~30 |
| Download guard | ~5 | ~30 |
| fakerun extension | ~25 | — |
| Integration stories (6) | — | ~150 |
| Existing test adaptation | ~5 | ~40 |

## Integration Tests

Follow existing conventions in `ritual_integration_test.go`.

```
TestIntegration_PlayingWithLiveSync_WorldsSyncEveryTick
TestIntegration_InternetDiesMidSession_ServerKeepsRunning
TestIntegration_CrashRecovery_LocalNewerKept
TestIntegration_ServerCrashDuringPlay_RemoteHasLastSyncedState
TestIntegration_NothingChangedDuringPlay_NothingUploaded
TestIntegration_PreviousSyncStillRunning_NextTickSkipped
```

## Unit Tests

Supervisor stories:
```
"player is playing, worlds sync every tick"
"player is playing, previous sync still uploading, next sync waits"
"player is playing, internet drops, server keeps running"
"player stops server, sync stops"
"another host stole the lock, sync stops"
```

Running stories:
```
"server starts and becomes reachable"
"server crashes, pipeline routes to unlock"
"server shuts down gracefully"
"user stops the app, server receives stop command"
```

Recovery stories:
```
"played offline, local worlds kept on next launch"
"played online, remote has latest worlds"
```

## Acceptance Criteria

1. **Reuses existing config** — sync interval from lease heartbeat interval in manifest
2. **Save-before-sync** — `save-all flush` via stdin before every upload, confirmed via stdout
3. **Internet resilience** — tick fails, next tick retries, server unaffected
4. **Crash recovery** — local newer than remote detected via `XXHashSyncAt`, local preserved
5. **Post-exit pipeline preserved** — backup, publish, unlock, retain after server stops
6. **Unified tick** — heartbeat and sync in same manifest cycle
7. **Observable** — all actions published to bus, logged for debugging
8. **Server crash safety** — exit with error routes to Unlocking
9. **Server is sacred** — sync in background goroutine, previous sync still running → heartbeat only
