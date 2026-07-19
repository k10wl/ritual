# 048 — Local-storage build variant (`gui:build:dev:local`)

## Background

Two stage axes already coexist (Taskfile header):

- **`:dev` / `:prod`** — which profile's R2 creds + identity get *baked* into the binary (build-time, in the task name).
- **`:local` / `:remote`** — which storage backend the *running* binary talks to. Today this is a **runtime** toggle only: `RITUAL_REMOTE_MODE=mock` → local-FS mock under `<root>/remote-mock`; anything else → R2 (`remote.ResolveModeFromEnv`, design-log/030). `gui:dev:local` / `gui:dev:remote` set it for `wails3 dev`.

Both the GUI (`cmd/gui/main.go:388`) and the publisher (`cmd/publish/main.go:65`) compose storage through the same `remote.Build(ctx, ResolveModeFromEnv(), bus)` and only ever touch `ports.StorageRepository` (`PutStream`/`List`/`Delete`/`GetStream`). So R2↔local-FS is already a zero-code substitution.

## Problem

We want to **test the full version/self-update lifecycle (037/038) entirely offline** against local files — publish vN, run vN-1, watch it list → download → atomic-swap → relaunch — without an R2 round-trip, and ideally against a *built / installable* binary, not just `wails3 dev`.

Two gaps:

1. **No build-time backend axis.** There is no `gui:build:dev:local` that produces a binary which *is* local-only. The only lever is a runtime env var, which (a) violates the project rule "every env-dependent task carries its stage in the name, never a hidden flag" (`feedback_task_naming_dev_prod`), and (b) is absent for an NSIS-installed / double-clicked binary — so we can't test the installed-update path offline.
2. **No ergonomic local publish.** `publish:dev` / `publish:prod` hardcode R2 (no `RITUAL_REMOTE_MODE`), so seeding the local store needs a manual `RITUAL_REMOTE_MODE=mock go run ./cmd/publish …`.

`prod` is remote-only by definition — no `prod:local`.

## Questions and Answers

**Q1. Bake the mode, or keep it runtime-only?**
Bake it (new ldflag), env still wins. Rationale: surfaces the backend in the task name (project rule), and is the *only* way to make an installed/double-clicked binary local — a runtime env var doesn't reach it. Note the runtime toggle already survives self-update relaunch (`exec.Command(exe)` leaves `cmd.Env` nil → child inherits env, `cmd/gui/main.go:263`), so baking is about ergonomics + the installed path, not correctness.

**Q2. Does the local variant get its own RootPath (isolated `remote-mock`), or share the dev one?**
**Share** (`AppName` stays `ritualdev`). Decisive reason: `cmd/publish` runs via `go run` with **no ldflag**, so its `config.AppName` is the default `ritualdev` → `RootPath = ~/k10wl/ritualdev` → `remote-mock` at `~/k10wl/ritualdev/remote-mock`. If the local GUI used a distinct `AppName` (e.g. `ritualdev-local`) it would read a *different* `remote-mock` than publish writes → mismatch. Sharing makes publish-local and run-local align with zero extra plumbing.

**Q3. Same binary filename as `dev:remote`, or distinct?**
**Distinct file, shared `AppName`.** `dev:local` → `bin/ritualdev-local.exe`; `dev:remote` → `bin/ritualdev.exe`. Decouple `OUT_NAME` (filename, self-describing, both coexist) from `APPNAME_LDFLAG` (`config.AppName=ritualdev`, shared RootPath per Q2). Otherwise both write `ritualdev.exe` and `gui:run:dev` becomes ambiguous about which backend it launches.

**Q4. Does `dev:local` still require `.env.dev.local`?**
Mock needs no creds, but the root `dotenv:` block is file-level and applies to all tasks. Keep requiring the file to *exist* (its contents are irrelevant for a local build — baked creds are unused when mode=mock). In practice the dev operator already has it. Documented caveat; not worth special-casing the dotenv block. *(Open: revisit if a creds-free contributor needs local-only builds.)*

**Q5. Precedence?**
`env RITUAL_REMOTE_MODE` (if set) → `bakedRemoteMode` (if set) → default `ModeR2`. Symmetric with baked creds (env overrides baked). Lets a baked-local binary be forced back to R2 without rebuild, and vice-versa.

## Design

### Backend resolution (`internal/subsystems/remote/build.go`)

```go
// Baked at build time via -ldflags -X ...remote.bakedRemoteMode=mock.
// Empty (the default / go-run) → falls through to ModeR2.
var bakedRemoteMode string

// ResolveMode: env wins, baked falls back, default R2 (design-log/030, 048).
func ResolveMode() Mode {
    if v := os.Getenv(EnvRemoteMode); v != "" {
        if v == "mock" {
            return ModeMock
        }
        return ModeR2
    }
    if bakedRemoteMode == "mock" {
        return ModeMock
    }
    return ModeR2
}
```

Rename `ResolveModeFromEnv` → `ResolveMode` (both call sites: `cmd/gui/main.go:388`, `cmd/publish/main.go:65`); keep `ResolveModeFromEnv` as a thin deprecated alias if anything else references it.

### Taskfile axis (root `Taskfile.yml`)

```yaml
gui:build:dev:local:
  desc: Build dev variant baked LOCAL-only → bin/ritualdev-local.exe (RITUAL_REMOTE_MODE=mock baked; no R2 hit).
  cmds: [RITUAL_ENV=dev REMOTE_MODE=mock DEV={{.DEV}} task gui:_build]
gui:build:dev:remote:
  desc: Build dev variant against R2 → bin/ritualdev.exe (dev R2 creds baked).
  cmds: [RITUAL_ENV=dev REMOTE_MODE= DEV={{.DEV}} task gui:_build]
# gui:build:dev kept as alias → gui:build:dev:remote (back-compat).

gui:run:dev:local:   { cmds: [RITUAL_ENV=dev REMOTE_MODE=mock task gui:_run] }
gui:run:dev:remote:  { cmds: [RITUAL_ENV=dev REMOTE_MODE= task gui:_run] }

publish:dev:local:   # seed the local store
  desc: Build dev:local + publish it into <root>/remote-mock (offline, no R2).
  cmds: [RITUAL_ENV=dev REMOTE_MODE=mock task _publish]
```

`prod` unchanged (remote-only). `gui:dev:local`/`gui:dev:remote` (wails3 dev iteration) unchanged.

### Build wiring (`build/windows/Taskfile.yml`, `build:native`)

- Decouple filename from AppName:
  ```yaml
  OUT_NAME:       '{{if eq .RITUAL_ENV "prod"}}{{.APP_NAME}}{{else}}{{.APP_NAME}}dev{{if eq .REMOTE_MODE "mock"}}-local{{end}}{{end}}'
  APP_IDENTITY:   '{{if eq .RITUAL_ENV "prod"}}{{.APP_NAME}}{{else}}{{.APP_NAME}}dev{{end}}'   # → config.AppName (shared RootPath)
  APPNAME_LDFLAG: '-X ritual/internal/config.AppName={{.APP_IDENTITY}}'
  MODE_LDFLAG:    '{{if eq .REMOTE_MODE "mock"}} -X ritual/internal/subsystems/remote.bakedRemoteMode=mock{{end}}'
  ```
- Append `{{.MODE_LDFLAG}}` inside the existing `LINK_FLAGS` ldflag string (alongside `R2_LDFLAGS`).
- `windows:run` `OUT_NAME` mirrors the same `-local` suffix logic so `gui:run:dev:local` finds the file.
- `_publish` `OUT_NAME` (root Taskfile) likewise, so publish targets the artifact `dev:local` produced.

`REMOTE_MODE` propagates as an env var through the `task gui:_build` subprocess (same mechanism `RITUAL_ENV`/`DEV` already use) and is read as `{{.REMOTE_MODE}}` in `build:native`.

## Examples

Full offline update loop:

```powershell
# Seed the local store with the current version, then bump + publish a newer one
task publish:dev:local                       # publishes vN into ~/k10wl/ritualdev/remote-mock
#   ... edit VersionPatch in internal/config/config.go (vN → vN+1) ...
task gui:build:dev:local                      # bin/ritualdev-local.exe is now vN-only? no — it's vN+1
# To watch an UPGRADE: keep a copy of the vN exe, publish vN+1, run the vN copy:
task publish:dev:local                        # now publishes vN+1
.\bin\ritualdev-local.exe                     # the older running copy lists mock → sees vN+1 → swaps → relaunches
```

✅ `gui:build:dev:local` → `bin/ritualdev-local.exe`, baked mock, `config.AppName=ritualdev`, RootPath `~/k10wl/ritualdev`.
✅ `go run ./cmd/publish` (default AppName=ritualdev) writes the same `remote-mock` the local GUI reads.
❌ Giving `dev:local` `AppName=ritualdev-local` — publish would write a different `remote-mock` than the GUI reads (Q2).

## Trade-offs

- **+** Backend is in the task name (project rule); installed binary testable offline; publish/run/GUI all align on one `remote-mock` for free.
- **+** Pure additive: existing `gui:build:dev` aliases to `:remote`; runtime `RITUAL_REMOTE_MODE` override preserved.
- **−** A baked-local binary can be `strings`-grepped to see it's mock — irrelevant (no creds, dev-only).
- **−** `dev:local` still needs `.env.dev.local` to exist (Q4) — minor, dev operator has it.
- **−** One more binary filename in `bin/`.

## Verification criteria

1. `task gui:build:dev:local` produces `bin/ritualdev-local.exe`; `strings`/Process Explorer shows it boots to the mock backend with **no** R2 network call (offline test passes).
2. `task publish:dev:local` writes `~/k10wl/ritualdev/remote-mock/bin/windows/amd64/<version>/<sha>.exe`.
3. Running an older `ritualdev-local.exe` against a newer published version performs list→download→swap→relaunch fully offline (037 flow on the dial).
4. `task gui:build:dev:remote` / `gui:build:dev` still produce `bin/ritualdev.exe` against R2, identical to today.
5. `task gui:build:prod` unchanged; no `prod:local` exists.
6. Existing tests green; add a `remote` unit test for `ResolveMode` precedence (env > baked > default).

## Open Questions

- **OQ1.** Add `:local`/`:remote` to `gui:package:dev` too (NSIS installer of the local variant), or is the bare exe enough for update testing? *(Proposed: defer — bare exe covers the loop.)*
- **OQ2.** Should `_publish` for `:local` skip the `test` + `lint` gates (faster inner loop), or keep them for parity? *(Proposed: keep gates; it's the same `_publish`.)*

## Implementation Results

Implemented as designed (OQ1 deferred, OQ2 keep-gates — the proposed defaults).

**Feature changes**
- `internal/subsystems/remote/build.go`: added `bakedRemoteMode` ldflag var; renamed `ResolveModeFromEnv` → `ResolveMode` with env-wins→baked→R2 precedence (Q5).
- Call sites updated: `cmd/gui/main.go`, `cmd/publish/main.go` (+ comment refresh).
- `Taskfile.yml`: `gui:build:dev:{local,remote}` (+ `gui:build:dev` alias→remote), `gui:run:dev:{local,remote}`, `publish:dev:local`; `_publish` OUT_NAME gains `-local` suffix + scopes `RITUAL_REMOTE_MODE=mock` to the publish command only.
- `build/windows/Taskfile.yml` `build:native` + `run`: `OUT_NAME` carries `-local` (filename), new `APP_IDENTITY` keeps `config.AppName=ritualdev` (shared RootPath — Q2/Q3), `MODE_LDFLAG` appended to `LINK_FLAGS`.
- Tests: renamed `TestResolveModeFromEnv`→`TestResolveMode`; new white-box `build_baked_test.go` covering baked-precedence.

**Deviation — pre-existing Windows test-isolation bugs surfaced while validating.** Running the full suite on Windows exposed a class of leaks unrelated to this feature; fixed here so the suite is green and trustworthy:
1. `adapters.ThrottledStorage` / `adapters.ServerCmdBuilder` held `os.Root` handles with no `Close()` — added `Close()` to both; the mock + cmdbuilder tests now release them before `t.TempDir` cleanup (Windows locks open dir handles, blocking `RemoveAll`).
2. `cmd/fakerun` test + `internal/integration` `TestMain` built the helper binary without `.exe` on Windows (`go build -o` appends it) → exec "not found" → 12 fakerun fails **and** every integration server test stalled on the 10s `waitReady` fallback (the multi-minute "hang"). Fixed: append `.exe` on `runtime.GOOS == "windows"`; tightened `waitReady` 10s→5s to fail fast.
3. `internal/integration` `buildPullingVerbs` leaked its workdir `os.Root` (unlike `buildCommittingVerbs`) → `worlds` dir stayed locked → cross-test state bleed + a cleanup failure. Threaded `t` through and registered the close. Integration package: **54.9s + 1 fail → ~8–10s green**.
4. `Taskfile.yml` `test:{helpers,unit,race}`: added a hard `-timeout` (`TEST_TIMEOUT`, default 60s / race 120s) so a future hang panics with a goroutine dump instead of running to Go's 10-minute default.

**Windows-stability hardening (user directive — "make the tests stable, not a memo").** Beyond the leaks above, two more pre-existing failures were root-caused and fixed so the whole suite is deterministically green:
5. `adapters` `TestCompressingStorage_EncoderPoolNoSerialization` was timing-flaky: it required N=10 concurrent 50ms-delayed pushes to finish under `2*D=100ms`, but Windows `time.Sleep` has ~15ms granularity and `go test ./...` runs packages in parallel, so a *correctly concurrent* run drifts past 100ms. Moved the ceiling to the midpoint `N*D/2=250ms` — still a 2× margin below the serialized `N*D=500ms` regression case, but jitter-proof. Holds over `-count=8`.
6. `subsystems/retention` `TestBuild_PrunesRefsAndSweepsOrphanBlobs` used a **stale fixture**, not a Windows bug: `refsRetention.parseTime` was switched to `domain.RefIDFormat` on 2026-06-05 (design-log/045 §Bug3) but `seedRef` still emitted compact `20060102150405` timestamps, which now fail to parse → every ref skipped → nothing pruned. Fixed the fixtures to `RefIDFormat` (`2026-04-20T10-00-00.000Z`, dash-only ⇒ Windows-filename-safe) and added a `seedRef` guard that fails loud if a ts isn't `RefIDFormat`, so the drift can't silently recur.

**Final test results:** full suite `go test ./... -timeout 60s` → **42 packages ok, 0 fail**. `task test` carries the hard timeout going forward.
