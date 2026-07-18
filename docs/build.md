# Build Process

Target platform is **Windows**. macOS builds are dev-iteration only.

All build orchestration lives in [`Taskfile.yml`](../Taskfile.yml) and platform-specific taskfiles under `build/<os>/`. Run via `task <name>` or `wails3 task <name>`. See [taskfile.md](taskfile.md) for the full task reference.

## Environment Files

Credentials live in gitignored `.env.{env}.local` files:

- `.env.dev.local` — Development R2 credentials
- `.env.prod.local` — Production R2 credentials

Format:

```
R2_ACCOUNT_ID=...
R2_ACCESS_KEY_ID=...
R2_SECRET_ACCESS_KEY=...
R2_BUCKET_NAME=...
APP_NAME=ritualdev         # or "ritual" for prod
```

`APP_NAME` determines the user-data folder (`%USERPROFILE%\k10wl\<APP_NAME>`).

Values are injected at build time via `ldflags` — no runtime `.env` reads.

## Building the CLI

Builds for the host OS — `.exe` on Windows, unsuffixed on macOS/Linux. Useful for running CLI logic against local fake-mc setups during GUI dev.

```bash
task cli:build:dev        # → bin/ritual_dev{.exe}
task cli:build:prod       # → bin/ritual_prod{.exe}
```

`task cli:build ENV=dev` is the underlying task. Required vars (`R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET_NAME`) are validated via `preconditions:` — the build aborts before compile if any are missing.

Pipeline:

1. **Windows hosts only**: `cmd/genversioninfo` emits `cmd/cli/info.json` from `internal/config/config.go`, then `wails3 generate syso` writes `cmd/cli/ritual_cli.syso` (icon from `assets/baiki-{env}.ico`)
2. `go build -ldflags "-X ..." -o bin/ritual_{env}{exeExt} ./cmd/cli`
3. `info.json` and `*.syso` cleaned up

On non-Windows hosts, the syso step is skipped (no Windows resource metadata on a unix binary).

## Building the GUI

```bash
task build                # → bin/ritual.exe (production)
task dev                  # dev mode: vite hot-reload + wails3 webview
task package              # NSIS installer → bin/ritual-<arch>-installer.exe
```

Pipeline (`task windows:build:native`):

1. `go mod tidy`
2. `wails3 generate bindings ./...` → `frontend/bindings/`
3. `npm install` + `npm run build` → `frontend/dist/`
4. `cmd/genversioninfo` → `build/windows/info.json`
5. `wails3 generate syso` → `cmd/gui/wails_windows_{arch}.syso` (icon from `assets/baiki-prod.ico`, `baiki-dev.ico` when `DEV=true`)
6. `go build -tags production -o bin/ritual.exe ./cmd/gui`

## Version Management

Single source of truth: `internal/config/config.go`.

```go
const (
    VersionMajor = 2
    VersionMinor = 0
    VersionPatch = 0
)

const (
    GroupName   = "k10wl"
    ProductName = "Ritual"
    Description = "Ritual - Minecraft Server Manager"
)
```

`cmd/genversioninfo` imports this package and emits wails3-format `info.json`. Bumping a version is a one-line change in `config.go` — rebuild picks it up automatically.

## Icon Sources

| Artefact                 | Source                                                                |
|--------------------------|-----------------------------------------------------------------------|
| Windows CLI `.syso`      | `assets/baiki-{env}.ico` via `task cli:build`                         |
| Windows GUI `.syso`      | `assets/baiki-prod.ico` / `baiki-dev.ico` via `task build`            |
| Browser favicon (dev)    | `frontend/public/favicon.ico` (copy of `assets/baiki-dev.ico`)        |
| macOS `.icns` (dev only) | `build/darwin/icons.icns` — regen: `sips -s format icns assets/baiki-dev.ico --out build/darwin/icons.icns` |

No duplicate icon pipelines — `assets/baiki-*.ico` is the single source.

## Windows Executable Metadata

Embedded into the `.syso` and surfaced in Explorer → Properties → Details:

- `ProductName`, `ProductVersion`
- `CompanyName`, `FileDescription`, `LegalCopyright`
- `Comments`

Note: `OriginalFilename` and `InternalName` (present under the legacy `goversioninfo` flow) are not part of wails3's info.json schema and have been dropped. They had no user-visible value.

## Files (Generated, Gitignored)

| File                        | Emitted by            | Cleaned up |
|-----------------------------|-----------------------|------------|
| `cmd/cli/info.json`         | `cmd/genversioninfo` | after CLI build |
| `cmd/cli/ritual_cli.syso`   | `wails3 generate syso`| after CLI build |
| `build/windows/info.json`   | `cmd/genversioninfo` | persisted between builds |
| `cmd/gui/wails_windows_*.syso` | `wails3 generate syso` | after GUI build |
| `bin/`                      | `go build`            | manual        |
| `frontend/dist/`            | `vite build`          | overwritten   |
| `frontend/node_modules/`    | `npm install`         | manual        |
| `frontend/bindings/`        | `wails3 generate bindings` | overwritten |
