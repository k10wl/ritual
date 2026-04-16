# Taskfile

Ritual's GUI build is driven by [Task](https://taskfile.dev) (`task` CLI), a Go-based make-alternative with YAML config. The top-level `Taskfile.yml` includes platform-specific taskfiles under `build/<os>/Taskfile.yml` and a shared `build/Taskfile.yml`.

Target platform is Windows. macOS is supported as a dev-iteration host only.

## Prerequisites

See [README — Prerequisites](../README.md#prerequisites) for the authoritative list. Quick recap:

- Go 1.25+, Node.js 18+
- Task CLI (`go install github.com/go-task/task/v3/cmd/task@latest` or OS package manager)
- Wails3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74`)
- Windows: WebView2 runtime, PowerShell 5.1+, optional NSIS
- macOS: Xcode CLT (`xcode-select --install`)

## Invoking Tasks

Run from repo root. Both forms are equivalent:

```
task <name>           # direct
wails3 task <name>    # through wails3 (adds env + validation)
```

## Common Tasks

| Task                | Purpose                                                                         |
| ------------------- | ------------------------------------------------------------------------------- |
| `task dev`          | Dev mode: vite hot-reload + wails3 webview. Default port: 9245.                 |
| `task build`        | Production build for host OS. Output: `bin/ritual.exe` (win) or `bin/ritual` (mac). |
| `task package`      | Package installer (NSIS on Windows, `.app` bundle on macOS).                    |
| `task run`          | Run last-built binary.                                                          |
| `task run:server`   | Build + run server-mode (headless HTTP). **Broken on darwin in wails3 alpha.74.** |
| `task build:server` | Server-mode build only.                                                         |

## Platform-Specific Invocation

Direct platform tasks (skip `{{OS}}` dispatch):

| Task                    | Purpose                             |
| ----------------------- | ----------------------------------- |
| `task windows:build`    | Force Windows build (native or Docker cross-compile based on `CGO_ENABLED`). |
| `task windows:package`  | Build + create NSIS installer.      |
| `task darwin:build`     | Force macOS native build (CGO=1).   |
| `task darwin:package`   | macOS `.app` bundle.                |

## Build Pipeline

`task build` (Windows) executes in this order:

1. `common:go:mod:tidy` — `go mod tidy`
2. `common:build:frontend`:
   1. `common:install:frontend:deps` — `npm install`
   2. `common:generate:bindings` — `wails3 generate bindings ./...`
   3. `npm run build` — vite production build → `frontend/dist/`
3. `windows:generate:versioninfo` — runs `go run ./cmd/genversioninfo build/windows/info.json`, sourcing version/name from `internal/config/config.go`
4. `windows:generate:syso` — `wails3 generate syso` using `assets/baiki-prod.ico` (or `baiki-dev.ico` when `DEV=true`)
5. `go build -tags production -o bin/ritual.exe ./cmd/gui`

## Version & Icon Sources

Single source of truth lives in Go code and repository assets:

| Artefact                  | Source                                                 |
| ------------------------- | ------------------------------------------------------ |
| App version, product name | `internal/config/config.go`                            |
| Windows `.exe` icon       | `assets/baiki-prod.ico` (or `baiki-dev.ico` if `DEV=true`) |
| macOS `.icns` icon        | `build/darwin/icons.icns` (regenerate via `sips -s format icns assets/baiki-dev.ico --out build/darwin/icons.icns`) |
| Browser favicon           | `frontend/public/favicon.ico` (copied from `assets/baiki-dev.ico`) |

Bumping a version: edit `internal/config/config.go` constants and rebuild. `build/windows/info.json` is regenerated from that on every build.

## Useful Variables

Override via env or CLI:

```
task build ARCH=arm64 DEV=true
wails3 task dev WAILS_VITE_PORT=9999
```

| Var              | Default              | Purpose                             |
| ---------------- | -------------------- | ----------------------------------- |
| `APP_NAME`       | `ritual`             | Binary filename.                    |
| `MAIN_PKG`       | `./cmd/gui`          | Main package path.                  |
| `BIN_DIR`        | `bin`                | Output directory.                   |
| `DEV`            | unset                | `true` for dev icon + debug flags.  |
| `ARCH`           | host arch            | `amd64` / `arm64`.                  |
| `CGO_ENABLED`    | `0` (Windows build)  | Set `1` to force Docker cross-compile. |
| `WAILS_VITE_PORT`| `9245`               | Vite dev server port.               |

## Adding Tasks

Append to root `Taskfile.yml`:

```yaml
tasks:
  test:
    summary: Go unit tests
    cmds:
      - go test ./...

  test:frontend:
    summary: Frontend tests
    dir: frontend
    cmds:
      - npm test
```

Then `task test`.

## Troubleshooting

- **`appicon.png: no such file or directory`** — `common:generate:icons` dep left over somewhere. We source icons from `assets/baiki-*.ico` directly; remove the `common:generate:icons` dep from the offending task.
- **`permission denied` on `bin/ritual-server`** — `build:server` built a non-main package (produced an archive, not an executable). Ensure the task passes `{{.MAIN_PKG}}` (`./cmd/gui`).
- **Dev mode fails on macOS with symbol redeclaration** — known upstream bug in wails3 alpha.74 server-mode on darwin. Use full GUI dev mode, not `run:server`.
- **Bindings show `0 Services, 0 Methods`** — generator defaults to the current directory. Services live under `internal/gui/services/`, so the Taskfile must invoke `wails3 generate bindings ./...`.
