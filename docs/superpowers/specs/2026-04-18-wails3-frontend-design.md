# Wails3 Frontend — POC Design

Date: 2026-04-18
Status: draft, awaiting user review

## Goal

Replace the demo Wails3 scaffold (`Greet`, RAM polling, net list) with a minimal, Apple-philosophy GUI that surfaces the Ritual orchestrator end-to-end. Non-technical user should see one screen at a time, no jargon, no raw event stream.

Covers checklist stories #6, #7, #8, #9, #10, #13, #14 from `docs/stories-checklist.md`, plus lifecycle stories #5.1–#5.5, and the integration tests already asserting the same bus events the GUI consumes.

## User-facing stages

Five sequential screens. Loop: 1 → 2 → 3 → 4 → 1.

| # | Stage | Screen |
|---|---|---|
| 1 | `idle` | Port input + RAM input (defaults from `domain.DefaultSettings`) + big Start button |
| 2 | `downloading` | Single progress bar, `"<mb>/<mb> MB"`, one-line label (e.g. "Downloading world…") |
| 3 | `running` | Ready light, copyable join addresses, Stop button |
| 4 | `uploading` | Single progress bar, bytes display, label (e.g. "Backing up…") |
| 5 | `locked` | Friendly message: `"<holder> is playing. You'll get a turn when they finish."` + Check again |

Failure at any stage: red banner overlays current stage with plain-English `errorText` + Retry. Retry re-enters at the failed orchestrator stage (matches existing `RetryRequested` semantics).

## Two-window topology

| Window | Name | Hidden at startup | Purpose |
|---|---|---|---|
| Main | `main` | no | View model + controls |
| Logs | `logs` | yes | Log console, shown on demand via `ControlService.ShowLogs` |

Log lines are emitted window-scoped via `logsWindow.EmitEvent("log:line", LogLine{Ts,Level,Msg})` — never broadcast. The main window never receives `log:line` traffic.

Closing the main window triggers graceful shutdown via a `WindowClosing` hook: vetoes the close, publishes `StopRequested`, waits for terminal status, then `app.Quit()`. Second close click short-circuits and quits immediately.

```go
mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
    if shutdownStarted.Load() { return }
    shutdownStarted.Store(true)
    e.Cancel()
    go func() {
        bus.Publish(app.StopRequested{})
        waitForTerminal(bus) // blocks until StatusChanged{Done|Failed}
        app.Quit()
    }()
})
```

## Go projection adapter

Location: `internal/gui/projection/` (new package).

One subscriber to `ports.EventBus`. Folds bus events into a single `ViewModel`. On every fold, emits one typed Wails event named `ritual:view`.

```go
type Stage string

const (
    StageIdle        Stage = "idle"
    StageDownloading Stage = "downloading"
    StageRunning     Stage = "running"
    StageUploading   Stage = "uploading"
    StageLocked      Stage = "locked"
    StageFailed      Stage = "failed"
)

type ViewModel struct {
    Stage       Stage
    Progress    int           // 0..100, stage-scoped
    BytesDone   int64
    BytesTotal  int64         // 0 when unknown
    FilesDone   int
    FilesTotal  int
    Label       string
    ErrorText   string         // "" unless Stage==failed
    LockHolder  string         // "" unless Stage==locked
    ReadyLight  bool           // true after ServerReadyInfo
    Addresses   []JoinAddress
}
```

Bus-event → fold mapping (authoritative table):

| Bus event | Fold effect |
|---|---|
| `StateChangedInfo{To: Checking \| Fetching \| Acquiring}` | `Stage=downloading` |
| `sync.SyncCommitProgressInfo` / `sync.SyncStageProgressInfo` | `Progress`, `BytesDone`, `BytesTotal`, `FilesDone`, `FilesTotal`, `Label` |
| `StateChangedInfo{To: Running}` | `Stage=running` |
| `running.ServerStartingInfo` | `Label="Starting server…"` |
| `running.ServerReadyInfo` | `ReadyLight=true`, `Label="Ready"` |
| `running.ServerStoppingInfo` | `Label="Stopping…"` |
| `running.ServerStoppedInfo` | passthrough; state machine will transition |
| `StateChangedInfo{To: Publishing \| Backup \| Unlocking \| Retaining}` | `Stage=uploading`, `Label` |
| `app.StatusChanged{Done}` | `Stage=idle`, reset counters |
| `app.StatusChanged{Failed, Err}` | `Stage=failed`, `ErrorText=Err.Error()` (raw for POC; iterate in QA if ugly) |
| `acquiring.LockHeldInfo{Holder}` (new event, see below) | `Stage=locked`, `LockHolder=Holder` |

`Addresses` comes from the existing `NetInfoService.JoinAddresses` call — the projection invokes it once on `Stage=running` entry and caches until the stage leaves.

Publishing strategy: any fold that changes one field of `ViewModel` → emit `ritual:view`. No throttling for POC.

### Minor backend addition: `acquiring.LockHeldInfo`

The acquiring strategy currently encodes lock-held only as an error string `"already locked by <holder>"` (`internal/core/stages/acquiring/strategy.go:66`). For the stage-locked UI to show the holder safely, add a dedicated event published right before the error is set:

```go
// internal/core/stages/acquiring/events.go (new file, same package convention as running)
type LockHeldInfo struct{ Holder string }
func (e LockHeldInfo) String() string { return fmt.Sprintf("lock held by %s", e.Holder) }

// inside Handle, before `rs.Err = fmt.Errorf("already locked by %s", ...)`
publish(rs.Bus, LockHeldInfo{Holder: remote.LockedBy})
```

Existing integration test `TestIntegration_ManifestLocked_RejectStart` already covers the control flow — we just add an assertion that `LockHeldInfo` is emitted. No other change to pipeline semantics.

## Log sink adapter

Location: `internal/gui/logsink/` (new package).

Second subscriber to `ports.EventBus`. Formats every event's `String()` output into a `LogLine{Ts, Level, Msg}` and window-scoped emits on the logs window.

```go
application.RegisterEvent[LogLine]("log:line")
logsWindow.EmitEvent("log:line", LogLine{Ts: ..., Level: lvl, Msg: evt.String()})
```

`Level` derived from event type (`ErrorInfo` → "error", `RetryAttemptInfo` → "warn", everything else → "info").

## Service surface

Location: `internal/gui/services/control.go` (new).

```go
type ControlService struct { bus ports.EventBus; projection *projection.Store; logsWindow *application.WebviewWindow }

func (c *ControlService) Start(port int, memoryMB int) error  // publishes app.StartRequested, persists domain.Settings
func (c *ControlService) Stop() error                          // publishes app.StopRequested
func (c *ControlService) Retry() error                         // publishes app.RetryRequested
func (c *ControlService) GetSnapshot() ViewModel               // current projection state (for first render race)
func (c *ControlService) ShowLogs()                            // logsWindow.Show(); SetFocus()
func (c *ControlService) CopyAddress(addr string) error        // Wails clipboard
```

The existing `GreetService` / `SysInfoService` / `NetInfoService` are deleted — not used by the new frontend. `NetInfoService` logic is re-homed inside projection.

## POC command builder

Production code path is the real Minecraft Java launcher. For the POC we wire the `cmd/fakerun` binary so the GUI is driveable end-to-end without a JRE / Minecraft jar.

```go
// cmd/gui/main.go
// TODO(ritual-gui-poc): swap fakerunCmdBuilder for the real Minecraft cmd
// builder once the GUI loop is proven end-to-end.
cmdBuilder := fakerunCmdBuilder{Bin: fakerunBinPath()}
```

Every POC-only line is tagged `// TODO(ritual-gui-poc):`. One grep removes the tag-matched blocks when the real cmd builder lands.

## Frontend layout

Lit + TypeScript, no new framework.

```
frontend/src/
  main.ts                   // mounts <ritual-app>  (main window)
  logs.ts                   // mounts <ritual-logs> (logs window)
  ritual-app.ts             // subscribes ritual:view, renders stage by vm.stage
  stages/
    stage-idle.ts           // Port + RAM inputs + Start
    stage-downloading.ts    // bar + "12 / 84 MB" + label
    stage-running.ts        // ready light, IP list w/ Copy, Stop
    stage-uploading.ts      // bar + bytes + label
    stage-locked.ts         // friendly lock-held screen + Check again
    error-banner.ts         // red banner + Retry (renders above active stage)
  ritual-logs.ts            // subscribes log:line, ring buffer render
```

Root container:

```ts
@state() private vm: ViewModel = await GetSnapshot();
connectedCallback() {
  super.connectedCallback();
  this.off = Events.On('ritual:view', (e) => { this.vm = e.data; });
}
disconnectedCallback() { super.disconnectedCallback(); this.off(); }
```

Stage components have zero local state. All values come from `vm` via `.vm=${this.vm}` (Lit property binding — keeps object identity, no attribute stringification).

Header on every stage carries a small "Logs" button → `ControlService.ShowLogs()`.

Demo scaffold (`my-element.ts`, RAM polling, net polling, Greet) is deleted.

## Wails event registration

```go
// cmd/gui/main.go — in init()
application.RegisterEvent[projection.ViewModel]("ritual:view")
application.RegisterEvent[logsink.LogLine]("log:line")
```

Two typed events, nothing else. Wails binding generator produces TS types in `frontend/bindings/events/`.

## Test strategy

### Automated (Go only)

`internal/gui/projection/projection_test.go` — one test per fold row against an in-memory `ports.EventBus`. Same harness `internal/app/ritual_integration_test.go` uses. Each test feeds bus events and asserts the ordered sequence of `ViewModel` snapshots emitted.

Examples:
- `TestProjection_DownloadingToRunning_EmitsStageTransition`
- `TestProjection_BackupStarts_FlipsToUploading`
- `TestProjection_LockRejected_StageLockedWithHolder`
- `TestProjection_FailureDuringFetch_StageFailedWithErrorText`
- `TestProjection_ServerReady_FlipsReadyLight`

Adheres to project rules already in memory: 1s ceiling, `t.Context()` + timeout, no partial fixtures, assertion messages verbose.

### Manual QA

User launches `cmd/gui` against `fakerun` and drives it through the 5 stages + lock + failure paths, reports feedback. No Wails smoke script, no Playwright, no Lit unit tests for POC.

## File-level summary (new / modified / deleted)

**New**
- `internal/gui/projection/projection.go`
- `internal/gui/projection/viewmodel.go`
- `internal/gui/projection/projection_test.go`
- `internal/gui/logsink/logsink.go`
- `internal/gui/services/control.go`
- `frontend/src/ritual-app.ts`
- `frontend/src/ritual-logs.ts`
- `frontend/src/logs.ts`
- `frontend/src/stages/*.ts` (5 stage files + `error-banner.ts`)

**Modified**
- `cmd/gui/main.go` — wire bus, projection, logsink, two windows, WindowClosing hook, fakerun cmd builder (POC)
- `frontend/src/main.ts` — replace scaffold, mount `<ritual-app>`
- `frontend/index.html` — mount `<ritual-app>` instead of `<my-element>`

**Deleted**
- `frontend/src/my-element.ts`
- `internal/gui/services/greet.go`
- `internal/gui/services/sysinfo.go`
- `internal/gui/services/netinfo.go` (logic re-homed inside projection)
- `internal/gui/services/netinfo_darwin.go` / `netinfo_other.go` / tests — Windows-only project, cross-platform shim no longer needed

## Out of scope (explicit)

- Settings persistence UI (Port/RAM are edited at stage 1 only; `Settings.Save()` is called under the hood on Start, existing behavior).
- Dark/light theme toggle.
- i18n.
- Live-sync mid-game visualization beyond the `uploading` stage.
- Logs-window filtering, search, level switches.
- Any story in the checklist marked as backend-only (#3 server-loads-world, #5.6 ghost-process prevention).
