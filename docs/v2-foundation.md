# v2 Foundation — Pre-GUI Rewrite Specification

## Motivation

v1 `cmd/cli/main.go` grew to 378 lines of mixed wiring, configuration, and side effects. Adding a GUI on top of this would inherit the mess: an untestable composition root, hardcoded subprocess launch, a storage layer that presumes network access, and build flows that block development on macOS.

v2 is **not a GUI port**. It is the foundation that makes a GUI (and LLM-assisted QA) trivial to add.

The target: every external dependency replaceable in under a minute. Dev loop on macOS with zero Windows, zero network, zero real Minecraft server. Prod behavior unchanged. GUI becomes a thin binding layer over ports that already exist and already pass tests without it.

---

## Principles

- **Logic before presentation.** GUI is the last mile, not the scaffold.
- **Ports are law.** Core imports nothing concrete. Every adapter hides behind an interface.
- **Composition is explicit.** No `func init()` doing silent work. Every dependency passed by argument.
- **Build tags select adapters.** No runtime flags choosing between real and fake.
- **Fixtures are first-class binaries.** Reproducible, stdin-driven, versioned alongside core.
- **Config comes from the environment.** No dotenv, no ldflags for secrets. Env vars only.

---

## Scope Cutoff

| v1 | v2 |
|---|---|
| `cmd/cli` entry | `cmd/gui` entry (binds Wails later) |
| PowerShell `build.ps1` | Mage (`magefile.go`) |
| ldflags-injected secrets | `os.Getenv` at startup |
| `.env.*.local` dotenv files | Shell-sourced env vars |
| 378-line `main.go` | `internal/app/` wire units, ≤80 LOC each |
| Java launched via `os/exec` in core | `ServerProcess` port, real/fixture adapters |
| Windows-only dev loop | macOS-native dev loop via `dev-local` tag |
| Version `1.x` | Version `2.0.0-alpha` during rewrite |

CLI binary deleted after v2 dev-local + prod parity confirmed.

---

## Build Matrix

Three mutually exclusive build modes. Selected via Go build tags — never runtime flags.

| Mode | Tag | Output | Storage | Subprocess | Icon | Use |
|------|-----|--------|---------|------------|------|-----|
| `prod` | *(none / `prod`)* | `ritual.exe` | R2 remote + OS local | Real Java | `baiki-prod.ico` | Production |
| `dev-remote` | `dev` | `ritual_dev.exe` | R2 remote + OS local | Real Java | `baiki-dev.ico` | Pre-release testing |
| `dev-local` | `dev,devlocal` | `ritual_dev.exe` | OS local for both slots | Fixture (`fakemc`) | `baiki-dev.ico` | macOS development |

Binary name is `ritual.exe` for prod; `ritual_dev.exe` for both dev modes. The two dev modes differ in adapters, not in identity — window title and console banner display the active mode.

### Tag combinations

- Default (no tags) = prod adapters.
- `-tags dev` = dev branding, R2 storage, real subprocess.
- `-tags dev,devlocal` = dev branding, OS-dir storage, fixture subprocess.

### Adapter selection via tags

```
internal/adapters/storage/
  r2.go            // no build tag — default
  local.go         // //go:build devlocal
internal/adapters/process/
  java.go          // //go:build !devlocal
  fixture.go       // //go:build devlocal
```

No `if mode == "dev-local"` in core. The compiler picks the adapter.

---

## Configuration

Environment variables only. Read once at startup in `internal/app/config.go`. No dotenv library. No ldflags for secrets.

### Contract

| Var | Required in | Purpose |
|-----|-------------|---------|
| `RITUAL_R2_ACCOUNT_ID` | prod, dev-remote | Cloudflare R2 account |
| `RITUAL_R2_ACCESS_KEY_ID` | prod, dev-remote | R2 access key |
| `RITUAL_R2_SECRET_ACCESS_KEY` | prod, dev-remote | R2 secret |
| `RITUAL_R2_BUCKET` | prod, dev-remote | R2 bucket name |
| `RITUAL_APP_NAME` | all | User-data folder name (`ritual` / `ritualdev`) |
| `RITUAL_LOCAL_ROOT` | dev-local | OS path for "local" storage slot |
| `RITUAL_REMOTE_ROOT` | dev-local | OS path acting as "remote" storage |
| `RITUAL_FIXTURE_BIN` | dev-local | Path to `fakemc` binary |
| `RITUAL_FIXTURE_SCRIPT` | dev-local (opt) | Scripted scenario for fixture stdin |

### Loading

```go
// internal/app/config.go
package app

import (
    "fmt"
    "os"
)

type Config struct {
    AppName      string
    R2           R2Config
    LocalRoot    string
    RemoteRoot   string
    FixtureBin   string
    FixtureScript string
}

func LoadConfig() (Config, error) {
    cfg := Config{
        AppName:       mustEnv("RITUAL_APP_NAME"),
        LocalRoot:     os.Getenv("RITUAL_LOCAL_ROOT"),
        RemoteRoot:    os.Getenv("RITUAL_REMOTE_ROOT"),
        FixtureBin:    os.Getenv("RITUAL_FIXTURE_BIN"),
        FixtureScript: os.Getenv("RITUAL_FIXTURE_SCRIPT"),
    }
    if r2, err := loadR2(); err == nil {
        cfg.R2 = r2
    }
    return cfg, validate(cfg)
}

func mustEnv(k string) string {
    v := os.Getenv(k)
    if v == "" {
        panic(fmt.Errorf("missing env: %s", k))
    }
    return v
}
```

Validation is tag-aware — `devlocal` build validates `RITUAL_LOCAL_ROOT` / `RITUAL_REMOTE_ROOT`; default build validates R2 vars. Split into `config_prod.go` / `config_devlocal.go` with build tags.

### Sourcing env on dev machines

Shell convention, not tool-imposed:

```bash
# ~/ritual-dev/env.sh
export RITUAL_APP_NAME=ritualdev
export RITUAL_LOCAL_ROOT=$HOME/ritual-dev/local
export RITUAL_REMOTE_ROOT=$HOME/ritual-dev/remote
export RITUAL_FIXTURE_BIN=$PWD/bin/fakemc
```

```bash
source ~/ritual-dev/env.sh
mage dev
```

Mage targets do not read dotenv. They require env already set. Fail loud on missing.

---

## Composition Root Refactor

v1 lesson: one 378-line `main.go` cannot be tested and cannot be understood. v2 splits it.

### Layout

```
internal/app/
  config.go              // Config + LoadConfig
  config_prod.go         //go:build !devlocal
  config_devlocal.go     //go:build devlocal
  wire.go                // top-level Wire(cfg) *Deps
  wire_storage.go        // storage adapters
  wire_process.go        // subprocess adapter
  wire_events.go         // event bus + subscribers
  wire_usecases.go       // core service wiring
  deps.go                // Deps struct definition
```

### Rules

- Each `wire_*.go` exposes one function: `wireX(cfg, prev *Deps) error`.
- No `func init()`. None. Not a single one.
- Each wire function is under 80 LOC. If larger, split further.
- Each wire function testable in isolation with a `Config` fixture.
- `cmd/gui/main.go` becomes:

```go
package main

import (
    "ritual/internal/app"
    "ritual/internal/gui"
)

func main() {
    cfg, err := app.LoadConfig()
    if err != nil {
        panic(err)
    }
    deps, err := app.Wire(cfg)
    if err != nil {
        panic(err)
    }
    defer deps.Shutdown()

    gui.Run(deps) // Wails or stub UI
}
```

Target: `main.go` under 30 lines. Forever.

---

## Ports Audit

Every external surface gets a port. Core imports only ports.

| Port | Real adapter | Fixture adapter |
|------|--------------|-----------------|
| `StorageRepository` (remote) | `r2.Client` | `localfs.Client` (devlocal) |
| `StorageRepository` (local) | `localfs.Client` | `localfs.Client` |
| `ServerProcess` | `java.Launcher` | `fakemc.Client` (devlocal) |
| `Clock` | `realClock` | `fakeClock` (tests) |
| `FileSystem` | `osRoot` | existing — already ported |
| `EventBus` | in-process pub/sub | same |

If a core package imports `os/exec`, `aws/sdk`, or any concrete adapter directly — it is a bug. Grep-gate in CI.

---

## Subprocess Abstraction

Java launch currently lives in core. v2 extracts it.

### Port

```go
// internal/core/ports/process.go
package ports

import (
    "context"
    "io"
)

type ServerProcess interface {
    Start(ctx context.Context) error
    Stdin() io.Writer
    Events() <-chan ProcessEvent
    Wait() error
    Stop(ctx context.Context) error
}

type ProcessEvent struct {
    Kind    ProcessEventKind
    Message string
}

type ProcessEventKind int

const (
    ProcessStarting ProcessEventKind = iota
    ProcessReady
    ProcessOutput
    ProcessExited
)
```

### Adapters

- `internal/adapters/process/java.go` — `os/exec` wrapper, reads real stdout.
- `internal/adapters/process/fixture.go` — launches `fakemc` binary with env + optional script path, pipes stdin/stdout the same way.

Core never knows which is active. Both emit the same `ProcessEvent` stream.

---

## Fixture Binary (`fakemc`)

Separate binary at `cmd/fakemc/main.go`. Builds independently with its own mage target. Shipped with dev-local builds, absent from prod.

### Behavior

Deterministic Minecraft-shaped process. No real networking, no real world. Reads commands from stdin, emits log lines on stdout.

### Stdin protocol

One command per line. Verbs:

| Command | Effect |
|---------|--------|
| `boot` | Emits `Starting server...` log sequence with configurable delay |
| `ready` | Emits `Done (Xs)! For help, type "help"` — signals port open |
| `write <relpath> <bytes-base64>` | Creates/overwrites file under configured world dir |
| `delete <relpath>` | Removes file |
| `tick` | Emits periodic log line (simulates runtime activity) |
| `chat <text>` | Emits `[Server] <text>` line |
| `save` | Emits `Saved the game` line |
| `shutdown` | Graceful exit sequence, then process exits |
| `crash <reason>` | Emits stack trace, exits non-zero |

### Stdout format

Mimics Minecraft log format: `[HH:MM:SS] [Server thread/INFO]: <message>`. Existing log scrapers (if any) continue to exercise real parsing paths.

### Config

```
FAKEMC_WORLD_DIR       // where fixture writes files
FAKEMC_BOOT_DELAY      // seconds before "Done!" after boot
FAKEMC_TICK_INTERVAL   // seconds between auto-ticks (0 = manual only)
```

### Scripted scenarios

Optional `--script path/to/scenario.txt` flag — reads the file as a stream of commands, one per line, with optional `sleep <ms>` interleaved. Enables:

- Reproducible integration tests
- LLM-driven test scenarios
- Demo recordings

---

## LLM QA Harness

Runs on top of all tests, not instead of them.

### Target: `mage qa`

1. Builds `dev-local` binary + `fakemc`.
2. Launches a scripted scenario under the fixture.
3. Captures stdout + storage snapshots at each step.
4. Feeds artifacts to an LLM reviewer with the scenario description.
5. LLM reports invariant violations, unexpected state, regressions vs previous run.

The harness is a thin shell over existing tests — it does not replace unit or integration suites. It adds a semantic review layer that catches what assertions miss.

Defer implementation until P1–P6 land. Specification captured here so design choices now do not block it later.

---

## Mage Build Tool

Replaces `build.ps1`. Pure Go. Cross-platform.

### Install (both macOS and Windows)

```bash
go install github.com/magefile/mage@latest
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
```

### Targets

| Target | Effect |
|--------|--------|
| `mage build:prod` | `ritual.exe` — prod tags, prod icon |
| `mage build:dev:remote` | `ritual_dev.exe` — `-tags dev`, dev icon |
| `mage build:dev:local` | `ritual_dev.exe` — `-tags dev,devlocal`, dev icon |
| `mage build:fakemc` | `bin/fakemc[.exe]` — fixture binary |
| `mage dev` | Builds dev:local + fakemc, runs with sourced env |
| `mage test` | `go test ./...` |
| `mage test:integration` | Integration tests against fixture |
| `mage qa` | LLM QA harness (deferred) |
| `mage clean` | Removes `ritual*.exe`, `bin/`, generated resources |
| `mage -l` | List targets |

### Magefile location

`magefile.go` at repo root, guarded by `//go:build mage`.

### Versioninfo generation

Mage reads `internal/config/config.go` version constants, writes `cmd/gui/versioninfo.json`, runs `go generate` for `resource.syso`. Icon path differs per build mode.

---

## Directory Layout (v2)

```
cmd/
  gui/
    main.go                // ≤30 LOC — load config, wire, run
    versioninfo.json       // generated, gitignored
    resource.syso          // generated, gitignored
  fakemc/
    main.go                // fixture binary
    protocol.go            // stdin command parser
    emitter.go             // stdout log writer

internal/
  app/
    config.go
    config_prod.go         //go:build !devlocal
    config_devlocal.go     //go:build devlocal
    deps.go
    wire.go
    wire_storage.go
    wire_process.go
    wire_events.go
    wire_usecases.go
  core/
    ports/
      process.go           // NEW — ServerProcess port
      storage.go           // existing
    domain/                // existing
    services/              // existing — now accept ServerProcess
  adapters/
    storage/
      r2.go                // prod remote
      localfs.go           // always built
    process/
      java.go              //go:build !devlocal
      fixture.go           //go:build devlocal

assets/
  baiki-prod.ico
  baiki-dev.ico

magefile.go                //go:build mage
docs/
  v2-foundation.md         // this file
```

---

## Migration Plan

Sequential. Each step self-contained, merges independently, ships on `feat/v2-foundation` branch.

### P0. Version bump + branch
- [ ] `VersionMajor = 2`, `VersionMinor = 0`, `VersionPatch = 0` in `config.go`.
- [ ] New branch `feat/v2-foundation` off `main`.

### P1. Mage baseline
- [ ] Install mage locally (macOS + Windows).
- [ ] `magefile.go` with `build:prod`, `build:dev:remote`, `clean` producing artifacts byte-equivalent to current `build.ps1` output.
- [ ] Parity checklist (version string, icon embed, binary size within 1%).
- [ ] Keep `build.ps1` alongside for one revision, delete after parity confirmed.

### P2. Config from env
- [ ] `internal/app/config.go` with `LoadConfig`.
- [ ] Remove ldflags secret injection from magefile.
- [ ] Shell env sourcing documented in README.
- [ ] Delete `.env.*.local` support path. Keep files gitignored; they become shell-source inputs only.

### P3. Composition root split
- [ ] Create `internal/app/` with `wire_*.go` units.
- [ ] Extract wiring from `cmd/cli/main.go` incrementally.
- [ ] No single file over 80 LOC in `internal/app/`.
- [ ] No `func init()` in the package.
- [ ] Each wire unit covered by a test that builds it from a `Config` fixture.

### P4. `cmd/gui` entry
- [ ] New `cmd/gui/main.go` reusing `internal/app/`.
- [ ] Window title + console banner surface build mode.
- [ ] No Wails yet — stub UI (blank window or console prompt).
- [ ] `mage build:prod` now produces `ritual.exe` from `cmd/gui`.

### P5. Subprocess port
- [ ] `internal/core/ports/process.go` defines `ServerProcess`.
- [ ] `internal/adapters/process/java.go` wraps existing `os/exec` code.
- [ ] Core services accept `ServerProcess` via constructor.
- [ ] CI grep-gate: `os/exec` import forbidden outside `internal/adapters/process`.

### P6. Storage port audit
- [ ] Confirm both storage slots pass through `StorageRepository`.
- [ ] Extract any remaining R2-specific code from core.
- [ ] `internal/adapters/storage/localfs.go` gains dual-role capability (local + remote slots point to different OS paths).

### P7. Fixture binary
- [ ] `cmd/fakemc/main.go` with stdin protocol.
- [ ] `internal/adapters/process/fixture.go` launches fakemc via `exec.Cmd`.
- [ ] Build tag wiring: `//go:build devlocal` selects fixture adapter.
- [ ] `mage dev` runs full dev-local loop on macOS.
- [ ] Scripted scenario support via `RITUAL_FIXTURE_SCRIPT`.

### P8. Dev-local wiring
- [ ] `config_devlocal.go` validates `LOCAL_ROOT`, `REMOTE_ROOT`, `FIXTURE_BIN`.
- [ ] `mage build:dev:local` produces working dev-local binary.
- [ ] Documented dev setup in README (env.sh sample).

### P9. Delete v1 CLI
- [ ] Run prod build from `cmd/gui` on Windows. Verify backup, lock, sync cycles match v1 behavior.
- [ ] Run dev-local on macOS. Verify fixture drives full state transitions.
- [ ] `rm -rf cmd/cli`.
- [ ] Update imports, docs, CI.

### P10. LLM QA harness
- [ ] `mage qa` target wiring.
- [ ] Baseline scenarios under `testdata/scenarios/`.
- [ ] Artifact capture + LLM review loop.

### Post-v2
- [ ] Wails v3 integration as thin binding layer over existing `app.Deps`.
- [ ] State-machine orchestrator migration (see `state-machine-proposal.md`).

---

## Verification

Each step lands with:

1. **Build parity** — outputs from old and new paths diffable.
2. **Test parity** — all existing tests continue to pass; new ports add isolated tests.
3. **Platform matrix** — macOS unit + dev-local integration; Windows prod build + smoke.

CI runs:

- macOS: `mage test`, `mage build:dev:local`, `mage test:integration` (fixture-driven)
- Windows: `mage test`, `mage build:prod`, smoke test real Java launch

Prod binaries are produced on Windows runners only. macOS cannot produce prod artifacts (CGo + MinGW chain not required until Wails lands; once Wails is added, this locks hard).

---

## Non-Goals

- Refactoring state-machine orchestration (separate track, per `state-machine-proposal.md`).
- Changing delta-sync semantics (v2 here refers to entry-point/foundation v2, orthogonal to `delta-sync-v2.md`).
- Introducing Wails. That step is trivial on top of this foundation and deferred until P1–P9 are green.
- Cross-compiling prod from macOS. Windows runner handles it.

---

## Success Criteria

- `cmd/gui/main.go` ≤ 30 LOC.
- No file in `internal/app/` exceeds 80 LOC.
- Zero `func init()` in `internal/app/`.
- Zero `os/exec` imports outside `internal/adapters/process`.
- Full dev loop on macOS — clone repo, source env, `mage dev` — no Windows, no Java, no network.
- `mage build:prod` on Windows produces byte-identical behavior to v1 prod builds for all existing integration scenarios.
- Adding a new external dependency requires: one port, one prod adapter, one fixture adapter, one wire line. No core changes.

When all six hold, v2 foundation is done. Wails becomes the next ticket.
