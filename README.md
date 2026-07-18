# R.I.T.U.A.L.

**Replicate Instances, Track Updates, Archive Legacy**

R.I.T.U.A.L. is a Golang application designed to orchestrate Minecraft Java servers with a mystical, crypt-themed workflow. It manages server lifecycle, world/plugin backups, manifests, and cloud/local synchronization, all while preserving history in a ritualistic and epic fashion.

## Overview

R.I.T.U.A.L. ensures:

• **Server Lifecycle Management**
  - Starts and stops Minecraft servers safely
  - Ensures no conflicts occur if another instance is running
  - Handles graceful shutdowns and crash recovery

• **Manifest & UUID Tracking**
  - Tracks UUIDs of worlds and plugins to prevent overwrites
  - Maintains FIFO + previous pointer history for reliability
  - Ensures concurrency safety across multiple orchestrators

• **Backups & Legacy Archiving**
  - Modular backup orchestration with template method pattern
  - Pluggable archive creation and cleanup strategies
  - Configurable storage backends (local/cloud)
  - Centralized retention policy management through Backupper
  - Prunes dangling or outdated backups automatically
  - Preserves server history like sacred artifacts

• **Distribution & Sync**
  - Uploads/downloads worlds and plugins to/from cloud storage (R2)
  - Ensures local and cloud states are consistent
  - Handles large file transfers with progress tracking

• **Monitoring & Validation**
  - Validates manifests and UUIDs before and after operations
  - Ensures the orchestration process runs safely and reliably
  - Provides comprehensive logging and error handling

## Core Architecture Components

R.I.T.U.A.L. implements a distributed orchestration system with mystical naming:

| Component | Domain | Responsibility |
|-----------|--------|----------------|
| **Molfar** | Orchestration | Central coordinator managing all system operations |
| **Librarian** | Manifest Management | Retrieves/stores local/remote manifest data |
| **Validator** | Validation | Performs instance integrity and consistency checks |
| **Backupper** | Backup Orchestration | Modular backup/archive management with pluggable strategies |
| **Storage** | Data Persistence | Unified interface for local/remote data operations |

## Operational Process Flow

R.I.T.U.A.L. follows a structured ritualistic workflow:

• **Initialization Phase**
  - Request manifest data from remote storage
  - Check for running instances to prevent conflicts
  - Write lock into remote manifest for exclusive access

• **Instance Synchronization**
  - Read local manifest for current state
  - Compare local and remote manifests for instance updates
  - Retrieve and replace outdated instances when required

• **World Data Management**
  - Compare world data against manifest versions
  - Update world data when synchronization required
  - Write current local metadata for tracking

• **Execution Phase**
  - Execute Java server instances
  - Monitor execution until completion
  - Write world data changes to storage

• **Termination Phase**
  - Store updated local manifest
  - Write manifest updates and release locks
  - Clean exit with proper resource cleanup

## Setup & Configuration

Target platform is **Windows**. macOS is supported as a dev-iteration host only.

### Prerequisites

#### All platforms

| Tool | Version | Purpose |
|------|---------|---------|
| [Go](https://go.dev/dl/) | 1.25+ | Compiler |
| [Node.js](https://nodejs.org/) | 18+ | GUI frontend (npm + vite) |
| [Task](https://taskfile.dev) | v3 | Build/test task runner (`Taskfile.yml`) |
| [Wails3 CLI](https://v3.wails.io) | v3.0.0-alpha.74 | GUI build + bindings generator |

Cross-platform installs for the two Go-based tools:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74
```

#### Windows (target)

- WebView2 runtime (preinstalled on Windows 10/11; otherwise NSIS installer bootstraps it)
- PowerShell 5.1+
- Task: `winget install Task.Task` (or `scoop install task`, or `choco install go-task`, or `go install` as above)
- Wails3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74`
- (Optional, for installer) [NSIS](https://nsis.sourceforge.io/Download)

#### macOS (dev only)

- Xcode Command Line Tools: `xcode-select --install` (needed for CGO + WKWebView)
- `brew install go node`
- Task: `brew install go-task/tap/go-task` (or `go install` as above)
- Wails3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74`

Verify each after install:

```bash
go version             # go1.25+
node --version         # v18+
task --version         # v3.x
wails3 version         # v3.0.0-alpha.74
```

### Environment Configuration

Copy `.env.example` to `.env.dev.local` / `.env.prod.local` (gitignored):

| Var | Purpose |
|-----|---------|
| `R2_ACCOUNT_ID` | Cloudflare R2 Account ID |
| `R2_ACCESS_KEY_ID` | Cloudflare R2 Access Key ID |
| `R2_SECRET_ACCESS_KEY` | Cloudflare R2 Secret Access Key |
| `R2_BUCKET_NAME` | R2 Bucket Name |
| `APP_NAME` | `ritualdev` (dev) or `ritual` (prod) — sets the user-data folder |

Values are injected at build time via `ldflags` (no runtime `.env` reads).

### Quick Start

```bash
# 1. clone + install deps
git clone <repo>
cd ritual
go mod tidy

# 2. build CLI (host-native — .exe on Windows, unsuffixed on mac/linux)
task cli:build:dev              # → bin/ritual_dev[.exe]
task cli:build:prod             # → bin/ritual_prod[.exe]

# 3. build GUI (Windows target)
task build                      # → bin/ritual.exe
task dev                        # dev mode with hot-reload
```

Every task has a `desc:` — run `task --list` for the full menu. See the header comment in [`Taskfile.yml`](Taskfile.yml) for conventions and known issues.

## Documentation

### Project Documentation
- **[Architecture Overview](docs/overview.md)** - High-level system architecture and components
- **[Project Structure](docs/structure.md)** - Detailed directory structure and component descriptions
- **[Build Process](docs/build.md)** - Environment files, version management, icon sources
- **[Taskfile.yml](Taskfile.yml)** - Tasks are self-documented (`task --list`); header explains conventions
- **[Defensive Programming Standards](docs/coding-practices.md)** - NASA JPL Power of Ten compliance guidelines
- **[Sprint Tracker](docs/progress.md)** - Development progress and sprint planning
- **[Architecture Diagrams](docs/ritual.drawio)** - Visual system architecture diagrams

### Development & Quality Assurance

### CI/CD Pipeline

The project uses GitHub Actions workflow:

• **Test Pipeline** - Runs on Windows with Go 1.25

Configuration files:
- `.github/workflows/ci.yml` - GitHub Actions workflow