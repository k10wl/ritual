# Server Launcher — `*exec.Cmd` via closure

## Motivation

`ServerRunner` hardcodes how the server is launched: PowerShell + `cmd /C start /wait` + shell tee to `logs/server.log`, with `updateServerProperties` baked in. This conflates three concerns:

1. **Config resolution** — read user settings, write `server.properties` with ip/port.
2. **Process construction** — pick the executable, build argv, set cwd.
3. **I/O routing** — where do stdout, stderr, and stdin go?

The third concern is the one GUI and tests disagree with CLI about. CLI wants `os.Stdout` + log file. GUI wants Wails events. Tests want `bytes.Buffer`. Baking a single routing into the adapter forces every consumer through the same shell pipeline.

The replacement: state machine receives a **launcher closure** that returns a ready-to-start `*exec.Cmd`. The composition root (`cmd/cli/main.go`, `cmd/gui/main.go`, integration harness) builds that closure with I/O wired wherever it wants.

## Port

`internal/core/ports/server.go`:

```go
package ports

import (
    "context"
    "os/exec"
)

// ServerLauncher constructs a ready-to-start *exec.Cmd for the Minecraft server.
// The returned Cmd must have Dir, Stdin, Stdout, Stderr, and (if graceful stop
// is desired) Cancel + WaitDelay populated. The state machine only calls
// Start + Wait — it does not mutate the returned Cmd.
type ServerLauncher func(ctx context.Context) (*exec.Cmd, error)
```

One function type. No struct. Replaces `ports.ServerRunner` (and the associated `*domain.ServerRuntime` threading) for server-launch purposes.

## Wiring timeline

```
                       ┌─────────────────────────┐
                       │ ritual binary startup   │
                       │  composition root runs  │
                       │  builds ServerLauncher  │
                       └────────────┬────────────┘
                                    │
                                    ▼
                       ┌─────────────────────────┐
                       │ Preparing state         │
                       │  conditions + updaters  │
                       └────────────┬────────────┘
                                    │
                  ┌─────────────────┴──────────────────┐
                  │                                    │
        ritual self-update?                 non-binary updaters
                  │                                    │
                  ▼                                    ▼
       os.Exit(0) after                    config / mod /
       replacement process                 manifest rewrites
       launch — current                    run in-process,
       binary dies here.                   then control returns
       New binary restarts                 to composition root.
       from startup.                                   │
                                                       ▼
                                        ┌─────────────────────────┐
                                        │ Locking → Running state │
                                        │  launcher(ctx)          │
                                        │  cmd.Start / cmd.Wait   │
                                        └─────────────────────────┘
```

Two update paths, two ordering rules:

**Binary self-update** — `RitualUpdater` in `internal/core/services/updater_ritual.go`. Ends with `os.Exit(0)` after launching the replacement binary via `--replace-old`. The current process never reaches `Running`. The replacement binary starts fresh, runs its own composition root, builds its own launcher with the new binary's code and new settings. No staleness possible — the closure is discarded with the process.

**In-process updaters** — config migration, mod sync, manifest rewrites. These run during `Preparing`, do not exit the process, and may rewrite user settings or files on disk. The launcher closure, built earlier in composition root, must **read settings lazily** (at call time) rather than bake snapshots. Otherwise post-update settings changes are invisible to the `Running` state.

### Lazy-read requirement

Non-negotiable for any field that updaters may mutate:

- `rt.IP`, `rt.Port`, `rt.Memory` — read from settings each call, not captured once.
- `startScript` path — same; migration might rename it.
- `.ritualsync` contents — read by whatever consumes it, not pre-parsed.

Fields that never change during the lifetime of a single binary (root path, binary version, OS facts) can be captured at closure creation.

### Config-file-backed launcher (the pattern)

Launcher closure reads the settings file at every invocation. Nothing about the server runtime is captured at closure creation. Settings mutation + state-machine re-entry = new `*exec.Cmd` built from fresh values. No in-memory cache, no re-wiring, no restart dance.

```go
// composition root — built once at startup
launcher := func(ctx context.Context) (*exec.Cmd, error) {
    settings, err := settingsLoader.Load()       // reads settings.json each call
    if err != nil {
        return nil, fmt.Errorf("load settings: %w", err)
    }
    rt, err := settings.ToServerRuntime()
    if err != nil {
        return nil, fmt.Errorf("runtime from settings: %w", err)
    }

    return buildCmd(ctx, rt, serverDir, ioRouter)
}
deps.ServerLauncher = launcher
```

Consequences:

- **Soft restart.** User edits settings via GUI → GUI writes `settings.json` → triggers the state machine to re-run → `Running` enters → launcher reloads → new process with new IP/port/memory/script. No binary restart.
- **Update cycle compatible.** In-process updaters rewrite settings during `Preparing`. The launcher, called next in `Running`, sees the new values. Zero coupling between updater and launcher.
- **GUI save-and-run button is two lines:** write settings, kick state machine. Frontend never touches launcher or cmd.
- **Test ergonomics.** Tests point `settingsLoader` at a writable file under `t.TempDir()`, mutate it between runs, observe different launch behavior in the same test binary.

### Full restart vs. soft restart

| Trigger | Path | Who handles |
|---------|------|-------------|
| User clicks "save settings" in GUI | write `settings.json` → re-enter state machine | composition root |
| Ritual binary updated | `os.Exit(0)`, new binary launches | `RitualUpdater` |
| In-process migration rewrites settings | next launcher call reads new settings | launcher closure |
| Server crashes mid-run | `cmd.Wait` returns error → `Exiting` → next cycle starts → launcher reloads | state machine |

Every mutable-settings change flows through the same seam: "write file, run launcher again." No separate "reload settings" API.

## Composition root — CLI

`cmd/cli/main.go`:

```go
launcher := func(ctx context.Context) (*exec.Cmd, error) {
    settings, err := settingsLoader.Load()
    if err != nil {
        return nil, err
    }
    rt, err := settings.ToServerRuntime()
    if err != nil {
        return nil, err
    }

    // 1. Config resolution — write ip/port into server.properties.
    if err := serverprops.Update(
        filepath.Join(serverDir, "server.properties"),
        rt.IP, rt.Port,
    ); err != nil {
        return nil, err
    }

    // 2. Process construction.
    scriptPath := filepath.Join(serverDir, startScript)
    cmd := exec.CommandContext(ctx, scriptPath, fmt.Sprintf("-Xmx%dM", rt.Memory))
    cmd.Dir = serverDir

    // 3. I/O routing — CLI wants terminal + log file, no stdin.
    logFile, err := os.Create(filepath.Join(rootPath, config.LogsDir, config.ServerLogFilename))
    if err != nil {
        return nil, err
    }
    cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
    cmd.Stderr = cmd.Stdout
    cmd.Stdin = nil

    // 4. Graceful stop on ctx cancel.
    pr, pw := io.Pipe()
    cmd.Stdin = pr
    cmd.Cancel = func() error {
        _, _ = io.WriteString(pw, "stop\n")
        return nil
    }
    cmd.WaitDelay = 10 * time.Second

    return cmd, nil
}

deps.ServerLauncher = launcher
```

## Composition root — GUI (Wails v3)

`cmd/gui/main.go` + `internal/ui/wails/`:

```go
// ServerService is bound to the Wails frontend.
type ServerService struct {
    mu    sync.Mutex
    stdin io.Writer
    app   *application.App
}

func (s *ServerService) SetStdin(w io.Writer) {
    s.mu.Lock(); defer s.mu.Unlock()
    s.stdin = w
}

// Send is called from the frontend console.
func (s *ServerService) Send(line string) error {
    s.mu.Lock(); w := s.stdin; s.mu.Unlock()
    if w == nil { return errServerNotRunning }
    _, err := io.WriteString(w, line+"\n")
    return err
}

// eventWriter converts io.Writer calls into Wails events, one per line.
type eventWriter struct {
    app   *application.App
    event string
    buf   []byte
}

func (w *eventWriter) Write(p []byte) (int, error) {
    w.buf = append(w.buf, p...)
    for {
        i := bytes.IndexByte(w.buf, '\n')
        if i < 0 { break }
        w.app.Event.Emit(w.event, string(w.buf[:i]))
        w.buf = w.buf[i+1:]
    }
    return len(p), nil
}
```

```go
// composition root
serverSvc := wailsui.NewServerService(app)

launcher := func(ctx context.Context) (*exec.Cmd, error) {
    settings, _ := settingsLoader.Load()
    rt, _ := settings.ToServerRuntime()

    if err := serverprops.Update(propsPath, rt.IP, rt.Port); err != nil {
        return nil, err
    }

    cmd := exec.CommandContext(ctx, scriptPath, fmt.Sprintf("-Xmx%dM", rt.Memory))
    cmd.Dir = serverDir

    pr, pw := io.Pipe()
    cmd.Stdin = pr
    serverSvc.SetStdin(pw)

    cmd.Stdout = io.MultiWriter(
        &eventWriter{app: app, event: "server.stdout"},
        logFile,
    )
    cmd.Stderr = io.MultiWriter(
        &eventWriter{app: app, event: "server.stderr"},
        logFile,
    )

    cmd.Cancel = func() error { _, _ = io.WriteString(pw, "stop\n"); return nil }
    cmd.WaitDelay = 10 * time.Second
    return cmd, nil
}
```

Frontend:

```ts
import { ServerService } from "./bindings";
import { Events } from "@wailsio/runtime";

Events.On("server.stdout", e => logPanel.append(e.data));
Events.On("server.stderr", e => logPanel.appendErr(e.data));

sendBtn.onclick = () => ServerService.Send(inputBox.value);
```

## Composition root — integration test

`internal/testing/ritualtest/harness.go`:

```go
type Harness struct {
    Root        string
    ServerOut   *bytes.Buffer
    ServerErr   *bytes.Buffer
    ServerStdin *io.PipeWriter
    // …
}

func (h *Harness) Launcher() ports.ServerLauncher {
    return func(ctx context.Context) (*exec.Cmd, error) {
        cmd := exec.CommandContext(ctx, filepath.Join(h.Root, "server", "start.bat"),
            "-Xmx512M")
        cmd.Dir = filepath.Join(h.Root, "server")

        pr, pw := io.Pipe()
        cmd.Stdin = pr
        h.ServerStdin = pw

        cmd.Stdout = h.ServerOut
        cmd.Stderr = h.ServerErr

        cmd.Cancel = func() error { _, _ = io.WriteString(pw, "stop\n"); return nil }
        cmd.WaitDelay = 5 * time.Second
        return cmd, nil
    }
}
```

Tests can assert on `h.ServerOut.String()` after `cmd.Wait()` returns, or inject commands via `h.ServerStdin` mid-run.

## State-machine integration

`internal/core/statemachine/running.go` loses its `server *domain.ServerRuntime` and `runner ports.ServerRunner` fields; gains `launcher ports.ServerLauncher`:

```go
type RunningState struct {
    launcher        ports.ServerLauncher
    localManifests  ports.ManifestStore
    remoteManifests ports.ManifestStore
    bus             ports.EventBus
    factory         StateFactory
}

func (s *RunningState) Handle(ctx context.Context) (Handler, error) {
    publish(s.bus, ports.StartInfo{Operation: "server"})
    if next := ctxFailed(ctx, s.factory, Running); next != nil {
        return next, nil
    }

    localBefore, _ := s.localManifests.Get(ctx)
    remoteBefore, _ := s.remoteManifests.Get(ctx)
    lockID := ""
    if localBefore != nil {
        lockID = localBefore.LockedBy
    }

    cmd, err := s.launcher(ctx)
    if err != nil {
        publish(s.bus, ports.ErrorInfo{Operation: "server", Err: err})
        return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
    }
    if err := cmd.Start(); err != nil {
        publish(s.bus, ports.ErrorInfo{Operation: "server", Err: err})
        return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
    }
    if err := cmd.Wait(); err != nil {
        publish(s.bus, ports.ErrorInfo{Operation: "server", Err: err})
    } else {
        publish(s.bus, ports.FinishInfo{Operation: "server"})
    }
    return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
}
```

`factory.Deps` loses `Server *domain.ServerRuntime`; gains `ServerLauncher ports.ServerLauncher`.

## What to delete

- `internal/adapters/serverrunner.go` + test — replaced by per-root launcher code.
- `internal/adapters/commandexecutor.go` — if it's only used by `ServerRunner`, delete with it. Confirm via grep before removing.
- `factory.Deps.Server *domain.ServerRuntime` field.
- `ports.ServerRunner` interface.

## What to extract

- `internal/serverprops/` — small package housing `Update(propsPath, ip string, port int) error` (the property-rewrite loop currently inside `ServerRunner.updateServerProperties`). Every launcher needs it; the state machine does not. Pure stdlib.

## Contract summary

| Field | Who sets | Rule |
|-------|----------|------|
| `cmd.Path` / `cmd.Args` | launcher | via `exec.CommandContext(ctx, name, args…)` |
| `cmd.Dir` | launcher | typically `serverDir` |
| `cmd.Stdin` | launcher | reader end of whatever stream carries user commands |
| `cmd.Stdout` / `cmd.Stderr` | launcher | writer(s) for logs, events, tests |
| `cmd.Cancel` | launcher | if graceful stop desired — send `stop\n` to stdin |
| `cmd.WaitDelay` | launcher | seconds before forced kill after Cancel |
| `cmd.Start` / `cmd.Wait` | state machine | never by launcher |

Launcher constructs. State machine drives. Composition root decides topology.

## Update ordering rule

- **Binary self-update** is process-terminal (`os.Exit(0)` in `RitualUpdater`). The replacement binary rebuilds its own launcher from scratch. No action required from the current process.
- **In-process updaters** (config migration, mod sync, .ritualsync rewrites, etc.) run during `Preparing` and may mutate settings before `Running` starts. The launcher must **read settings lazily** — invoke `settingsLoader.Load()` inside the closure, not outside. See "Lazy-read requirement" above.

Do **not** snapshot mutable settings into the closure at program startup. Any in-process updater could invalidate them silently. The state machine would then launch the server with pre-update config.

## Testing strategy

### Two seams cover the whole state machine

With `ServerLauncher` in place, the state machine has exactly two external-world edges that tests swap:

1. **`ServerLauncher`** — test returns any `*exec.Cmd` (`exit 0`, `exit 1`, `powershell Start-Sleep …`, echo) for the scenario.
2. **`StorageRepository` (remote)** — test wires a local-FS adapter at `t.TempDir()` posing as remote. No R2, no MinIO, no network.

Everything else runs real: state machine, factory, manifest store, sync service, backup, retention, ritualsync parser, local FS adapter, `os.Root` confinement.

### Integration harness — `internal/testing/ritualtest/`

```go
type Harness struct {
    Root        string           // os.Root-confined ritual root
    Local       string
    Remote      string           // local dir acting as remote
    Deps        *statemachine.Deps
    ServerOut   *bytes.Buffer
    ServerErr   *bytes.Buffer
    ServerStdin *io.PipeWriter   // writer end of cmd.Stdin pipe
}

func New(t testing.TB) *Harness
func (h *Harness) SetCmd(factory func(ctx context.Context) (*exec.Cmd, error))
func (h *Harness) SetRemote(repo ports.StorageRepository)
func (h *Harness) Run(ctx context.Context) error   // drives full state machine
```

`SetCmd` wraps the supplied factory to attach `ServerOut` / `ServerErr` / `ServerStdin` I/O. Test never touches `*exec.Cmd` directly.

### Scenario table pattern

```go
type Scenario struct {
    Name              string
    Cmd               func(ctx context.Context) (*exec.Cmd, error)
    RemoteWrapper     func(ports.StorageRepository) ports.StorageRepository  // e.g. flakyRepo
    PreSeed           func(t testing.TB, h *Harness)
    CancelAfter       time.Duration
    ExpectTerminal    statemachine.StateName
    ExpectLockCleared bool
    ExpectBackup      bool
    ExpectRemoteFiles []string
    ExpectStdinSent   string
}

func TestIntegration_Matrix(t *testing.T) {
    scenarios := []Scenario{
        {Name: "happy_path",             Cmd: OkCmd("Done"),          ExpectLockCleared: true},
        {Name: "server_crash",           Cmd: FailCmd(1),             ExpectBackup: true},
        {Name: "ctx_cancel_graceful",    Cmd: SleepCmd(30*time.Second), CancelAfter: 500*time.Millisecond, ExpectStdinSent: "stop\n"},
        {Name: "stale_lock_recovery",    Cmd: OkCmd(""), PreSeed: SeedStale},
        {Name: "remote_flake_recovers",  Cmd: OkCmd(""), RemoteWrapper: Flaky(2)},
    }
    for _, s := range scenarios {
        t.Run(s.Name, func(t *testing.T) { RunScenario(t, s) })
    }
}
```

`OkCmd`, `FailCmd`, `SleepCmd` are ~5-line helpers returning `exec.Command("cmd","/C", ...)` invocations. Windows-only build tag on this file.

### Required `internal/testhelpers/` additions

Existing helpers (`PaperInstanceSetup`, `PaperMinecraftWorldSetup`, `HashDir`, `CheckDirs`) provide server + world tree generation. Missing pieces to fill out scenario coverage:

| Helper | Purpose | Size |
|--------|---------|------|
| `SeedRitualsync(root, prefix, rules)` | Write real-syntax `.ritualsync` per prefix. Preset helpers: `RulesWildcard()`, `RulesServerDefaults()`. | ~10 LOC |
| `SeedManifest(root, preset, opts)` | Construct real `*domain.Manifest`, persist via real `ManifestStore`. Presets: `ManifestEmpty`, `ManifestV2Clean`, `ManifestV2Locked`, `ManifestV1Legacy`. | ~40 LOC |
| `SeedBackup(root, entry)` | Write backup under `backups/<prefix>/` with controlled `Age` + deterministic content. | ~20 LOC |
| `SeedSettings(root, *domain.Settings)` + `DefaultSettings()` | Write `settings.json` at root. | ~15 LOC |
| `SeedStaleLock(root, worldDir)` | Create `session.lock` with MC snowman marker. | ~10 LOC |

Zero hand-rolled JSON. All manifest construction goes through `domain.Manifest` so schema evolution flows through tests automatically.

### Helpers refactor — Paper → flavor-aware

`PaperInstanceSetup` / `PaperMinecraftWorldSetup` are Paper-shaped. Project runs Fabric. Rename and generalize:

| Old | New | Notes |
|-----|-----|-------|
| `PaperInstanceSetup(root, version)` | `SeedServerInstance(root, ServerOpts)` | `ServerOpts` carries `Flavor`, `Version`, `StartScript`, admin-list toggle, mods list. Default `Flavor = FlavorFabric`. |
| `PaperMinecraftWorldSetup(root)` | `SeedWorld(root, WorldOpts)` | `WorldOpts` carries `Flavor`, `LevelName`, `Dims`, `Regions`, extra `Files`. Dim layout switched by flavor. |
| `createDimensionDirsWithRoot` | private to `seed_world.go`, flavor-switched | — |
| `HashDir`, `HashDirs`, `CheckDirs`, `getFileHash` | unchanged | flavor-neutral |

New `flavor.go`:
```go
type Flavor string
const (
    FlavorFabric  Flavor = "fabric"   // default — vanilla sibling dims, mods/, config/
    FlavorPaper   Flavor = "paper"    // plugins/, world_nether/world_the_end siblings
    FlavorVanilla Flavor = "vanilla"
    FlavorForge   Flavor = "forge"    // nested DIM-1/DIM1/dimensions/
)

// DimPath resolves a dimension ID to a relative path under the level dir,
// honoring flavor. minecraft:overworld always returns "".
func DimPath(flavor Flavor, dimID string) string
```

Compat shims stay through the migration so existing callers (`sync_integration_test.go`, retention/backup tests) don't all rewrite in one PR:

```go
// Deprecated: use SeedWorld(root, WorldOpts{Flavor: FlavorPaper, LevelName: "world"}).
func PaperMinecraftWorldSetup(root *os.Root) (string, []string, func(string) error, error)
// Deprecated: use SeedServerInstance(root, ServerOpts{Flavor: FlavorPaper, Version: version}).
func PaperInstanceSetup(root *os.Root, version string) (string, []string, func(string) error, error)
```

Delete shims in a final commit after `grep -rn "PaperMinecraftWorldSetup\|PaperInstanceSetup"` returns empty.

### File layout — `internal/testhelpers/` target

```
testhelpers/
  checksum.go                  # unchanged
  checksum_test.go             # unchanged
  flavor.go                    # Flavor enum + DimPath — NEW
  flavor_test.go
  seed_server.go               # was paperinstancesetup.go; flavor-aware
  seed_server_test.go
  seed_world.go                # was paperworldsetup.go; flavor-aware
  seed_world_test.go
  seed_ritualsync.go           # NEW
  seed_ritualsync_test.go
  seed_manifest.go             # NEW — uses real domain.Manifest
  seed_manifest_test.go
  seed_backup.go               # NEW
  seed_backup_test.go
  seed_settings.go             # NEW
  seed_settings_test.go
  seed_lock.go                 # NEW — session.lock with MC marker
  seed_lock_test.go
```

### Test tiers

| Tier | Build tag | Uses | Runtime |
|------|-----------|------|---------|
| Unit | none | pure Go, no I/O | <1s per package |
| Integration (fake cmd) | `//go:build windows` | two-seam harness, `exec.Command("cmd","/C",…)` as fake server | seconds |
| Reality check (optional) | `//go:build integration_real` | two-seam harness, real fabric via JDK | 30s+ per scenario |

Fake-cmd integration runs every push. Reality-check fabric runs on main-branch merge or nightly.

### Component-level unit tests (unchanged from prior plan)

- `serverprops.Update` — table-driven, round-trips known inputs.
- `eventWriter` — asserts one event per line, buffers incomplete lines across `Write` calls.
- `ServerService.Send` — errors when no stdin set; succeeds when set.
- `RunningState` — dummy `ServerLauncher` returning trivial cmd, assert state transitions unchanged.

No changes to existing state-machine unit tests beyond swapping `ServerRunner` for a trivial `ServerLauncher` returning `exec.Command("cmd", "/C", "exit 0")` on Windows.
