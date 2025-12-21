# R.I.T.U.A.L. Project Structure

## Project Import Structure

All project imports follow the pattern: `ritual/...`

Example imports:
- `ritual/internal/core/domain` - Domain entities
- `ritual/internal/core/ports` - Interface definitions
- `ritual/internal/core/services` - Business logic services
- `ritual/internal/adapters` - External system integrations

## Overview

R.I.T.U.A.L. follows hexagonal architecture principles to achieve clean separation of concerns, testability, and maintainability. The structure is organized around the core domain of Minecraft server orchestration with mystical naming conventions.

## Directory Structure

```
ritual/
├── cmd/
│   └── cli/
│       └── main.go              # Application entry point
├── go.mod                       # Go module definition
├── go.sum                       # Go module checksums
├── README.md                    # Project documentation
├── docs/
│   ├── overview.md              # High-level architecture overview
│   ├── structure.md             # This file - project structure documentation
│   ├── progress.md              # Sprint tracker and progress
│   ├── cleanup.md               # Cleanup tasks documentation
│   └── coding-practices.md      # NASA JPL defensive programming standards
└── internal/
    ├── config/
    │   └── config.go           # Centralized configuration constants
    ├── adapters/
    │   ├── fs.go                # Local filesystem storage adapter
    │   ├── fs_test.go           # FSRepository tests
    │   ├── r2.go                # Cloudflare R2 storage adapter
    │   ├── r2_test.go           # R2Repository tests
    │   ├── serverrunner.go      # Server execution adapter
    │   ├── serverrunner_test.go # ServerRunner tests
    │   ├── commandexecutor.go   # Command execution adapter
    │   ├── commandexecutor_test.go # CommandExecutor tests
    │   └── streamer/            # Streaming archive operations
    │       ├── types.go         # Streamer types and interfaces
    │       ├── push.go          # Streaming upload (tar.gz creation)
    │       ├── push_test.go     # Push tests
    │       ├── pull.go          # Streaming download (tar.gz extraction)
    │       ├── pull_test.go     # Pull tests
    │       └── localwriter.go   # Local file writer for streaming
    ├── testhelpers/
    │   ├── paperinstancesetup.go    # Paper Minecraft server instance test helper
    │   ├── paperinstancesetup_test.go # PaperInstanceSetup test suite
    │   ├── paperworldsetup.go        # Paper Minecraft world test helper
    │   ├── paperworldsetup_test.go   # PaperWorldSetup test suite
    │   ├── checksum.go              # Checksum helper for tests
    │   └── checksum_test.go         # Checksum tests
    └── core/
        ├── domain/
        │   ├── manifest.go      # Manifest entity
        │   ├── manifest_test.go # Manifest entity tests
        │   ├── server.go        # Server entity
        │   ├── server_test.go   # Server entity tests
        │   ├── world.go         # World entity
        │   └── world_test.go    # World entity tests
        ├── ports/
        │   ├── ports.go         # Interface definitions
        │   └── mocks/           # Mock implementations for testing
        │       ├── storage.go       # Mock StorageRepository implementation
        │       ├── storage_test.go  # StorageRepository mock tests
        │       ├── molfar.go        # Mock MolfarService implementation
        │       ├── molfar_test.go   # MolfarService mock tests
        │       ├── librarian.go     # Mock LibrarianService implementation
        │       ├── librarian_test.go # LibrarianService mock tests
        │       ├── validator.go     # Mock ValidatorService implementation
        │       ├── validator_test.go # ValidatorService mock tests
        │       ├── serverrunner.go     # Mock ServerRunner implementation
        │       ├── serverrunner_test.go # ServerRunner mock tests
        │       ├── commandexecutor.go  # Mock CommandExecutor implementation
        │       ├── commandexecutor_test.go # CommandExecutor mock tests
        │       ├── backupper.go        # Mock BackupperService implementation
        │       ├── backupper_test.go   # BackupperService mock tests
        │       ├── updater.go          # Mock UpdaterService implementation
        │       └── updater_test.go     # UpdaterService mock tests
        └── services/
            ├── molfar.go            # Main orchestration service
            ├── molfar_test.go       # MolfarService tests
            ├── librarian.go         # Manifest management service
            ├── librarian_test.go    # LibrarianService tests
            ├── validator.go         # Validation service
            ├── validator_test.go    # ValidatorService tests
            ├── backupper_local.go   # Local backup service (streaming)
            ├── backupper_local_test.go # LocalBackupper tests
            ├── backupper_r2.go      # R2 backup service (streaming)
            ├── backupper_r2_test.go # R2Backupper tests
            ├── updater_ritual.go    # Ritual self-update service
            ├── updater_ritual_test.go # RitualUpdater tests
            ├── updater_instance.go  # Instance update service
            ├── updater_instance_test.go # InstanceUpdater tests
            ├── updater_worlds.go    # Worlds update service
            └── updater_worlds_test.go # WorldsUpdater tests
```

## Architecture Layers

### Configuration Layer (`internal/config/`)

Centralizes all application constants and configuration values:

- **`config.go`** - Single source of truth for all constants

#### Configuration Categories

```go
// Application identity
const (
    GroupName = "k10wl"
    AppName   = "ritualdev"
)

// Directory names
const (
    LocalBackups  = "world_backups"
    RemoteBackups = "worlds"
    InstanceDir   = "instance"
    TmpDir        = "temp"
)

// File names and keys
const (
    ManifestFilename   = "manifest.json"
    InstanceArchiveKey = "instance.tar.gz"
    RemoteBinaryKey    = "ritual.exe"
)

// Backup configuration
const (
    R2MaxBackups    = 5
    LocalMaxBackups = 10
    MaxFiles        = 1000
    TimestampFormat = "20060102150405"
    BackupExtension = ".tar.gz"
)

// World directories (relative to instance)
var WorldDirs = []string{
    "world",
    "world_nether",
    "world_the_end",
}

// Update process flags and timing
const (
    ReplaceFlag          = "--replace-old"
    CleanupFlag          = "--cleanup-update"
    UpdateProcessDelayMs = 500
    UpdateFilePattern    = "ritual_update_%d.exe"
    UpdateFileGlob       = "ritual_update_*.exe"
)

// Lock ID format
const (
    LockIDSeparator = "__"  // Format: {hostname}__{timestamp}
)

// S3/R2 configuration
const (
    S3PartSize       = 5 * 1024 * 1024  // 5 MB parts
    S3Concurrency    = 1                 // Sequential upload
    R2EndpointFormat = "https://%s.r2.cloudflarestorage.com"
)

// File permissions
const (
    DirPermission  = 0755
    FilePermission = 0644
)

// RootPath is computed at init from user home directory
var RootPath string
```

#### Usage Pattern

All services import from the config package instead of defining local constants:

```go
import "ritual/internal/config"

// Use centralized constants
timestamp := time.Now().Format(config.TimestampFormat)
key := config.RemoteBackups + "/" + timestamp + config.BackupExtension

// World directories
for _, dir := range config.WorldDirs {
    worldPath := filepath.Join(rootPath, config.InstanceDir, dir)
}
```

### Streaming Layer (`internal/adapters/streamer/`)

Provides streaming archive operations for efficient backup and update processes:

- **`types.go`** - Configuration types and interfaces for streaming operations
- **`push.go`** - Streaming upload with tar.gz creation directly to R2
- **`pull.go`** - Streaming download with tar.gz extraction from R2
- **`localwriter.go`** - Local file writer implementation for streaming to filesystem

#### Streamer Types

```go
// ConflictStrategy defines how to handle existing files during Pull
type ConflictStrategy int

const (
    Replace ConflictStrategy = iota // Overwrite existing files (default)
    Skip                            // Skip existing files
    Backup                          // Move existing to .bak
    Fail                            // Return error on conflict
)

// PushConfig configures the Push operation
type PushConfig struct {
    Dirs         []string    // Source directories to archive
    Bucket       string      // R2 bucket name
    Key          string      // R2 object key (path/filename.tar.gz)
    LocalPath    string      // Optional: local backup path
    ShouldBackup func() bool // Condition for local backup
}

// PullConfig configures the Pull operation
type PullConfig struct {
    Bucket   string                 // R2 bucket name
    Key      string                 // R2 object key
    Dest     string                 // Destination directory
    Conflict ConflictStrategy       // How to handle existing files
    Filter   func(name string) bool // Optional: filter files to extract
}

// S3StreamUploader interface for R2 streaming uploads
type S3StreamUploader interface {
    Upload(ctx context.Context, bucket, key string, body io.Reader) (int64, error)
}

// S3StreamDownloader interface for R2 streaming downloads
type S3StreamDownloader interface {
    Download(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}
```

### Core Domain Layer (`internal/core/domain/`)

Contains the core business entities:

- **`manifest.go`** - Central manifest tracking ritual/instance versions, locks, and world backups
- **`server.go`** - Server configuration entity with address parsing and validation
- **`world.go`** - World data entity with URI validation and timestamp tracking

#### Domain Entity Examples

```go
// internal/core/domain/manifest.go
type Manifest struct {
    RitualVersion   string    `json:"ritual_version"`   // Version of the ritual binary
    LockedBy        string    `json:"locked_by"`        // {hostname}__{UNIX timestamp}, or empty if not locked
    InstanceVersion string    `json:"instance_version"` // Version of the Minecraft instance
    StoredWorlds    []World   `json:"worlds"`           // Queue of latest world backups
    UpdatedAt       time.Time `json:"updated_at"`
}

// Key methods
func (m *Manifest) IsLocked() bool
func (m *Manifest) Lock(lockBy string)
func (m *Manifest) Unlock()
func (m *Manifest) AddWorld(world World)
func (m *Manifest) GetLatestWorld() *World
func (m *Manifest) Clone() *Manifest
func (m *Manifest) RemoveOldestWorlds(maxCount int) []World
```

### Ports Layer (`internal/core/ports/`)

Defines interfaces for external dependencies and provides comprehensive mock implementations for testing:

- **`ports.go`** - All service and repository interfaces
  - `StorageRepository` - Storage operations interface (Get, Put, Delete, List, Copy)
  - `MolfarService` - Main orchestration interface (Prepare, Run, Exit)
  - `LibrarianService` - Manifest management interface
  - `ValidatorService` - Validation interface
  - `CommandExecutor` - Command execution interface
  - `ServerRunner` - Server execution interface
  - `BackupperService` - Backup orchestration interface
  - `UpdaterService` - Update operations interface

- **Mock Implementations** (`mocks/` folder) - Complete mock implementations with test coverage
  - `storage.go` - MockStorageRepository with comprehensive testing utilities
  - `molfar.go` - MockMolfarService with status tracking and error simulation
  - `librarian.go` - MockLibrarianService with manifest synchronization logic
  - `validator.go` - MockValidatorService with configurable validation results
  - `serverrunner.go` - MockServerRunner with server execution simulation
  - `commandexecutor.go` - MockCommandExecutor with command simulation
  - `backupper.go` - MockBackupperService with backup operation simulation
  - `updater.go` - MockUpdaterService with update operation simulation

- **Test Coverage** (`mocks/` folder) - Each mock includes comprehensive test suites
  - `*_test.go` files provide 100% test coverage for all mock functionality
  - Tests cover success cases, error conditions, concurrency, and edge cases
  - Mock utilities enable isolated testing of dependent modules

#### Port Interface Examples

```go
// internal/core/ports/ports.go
type StorageRepository interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, data []byte) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
    Copy(ctx context.Context, sourceKey string, destKey string) error
}

type MolfarService interface {
    Prepare() error
    Run(server *domain.Server) error
    Exit() error
}

type LibrarianService interface {
    GetLocalManifest(ctx context.Context) (*domain.Manifest, error)
    GetRemoteManifest(ctx context.Context) (*domain.Manifest, error)
    SaveLocalManifest(ctx context.Context, manifest *domain.Manifest) error
    SaveRemoteManifest(ctx context.Context, manifest *domain.Manifest) error
}

type ValidatorService interface {
    CheckInstance(local *domain.Manifest, remote *domain.Manifest) error
    CheckWorld(local *domain.Manifest, remote *domain.Manifest) error
    CheckLock(local *domain.Manifest, remote *domain.Manifest) error
}

type CommandExecutor interface {
    Execute(command string, args []string, workingDir string) error
}

type ServerRunner interface {
    Run(server *domain.Server) error
}

type BackupperService interface {
    Run(ctx context.Context) (string, error)
}

type UpdaterService interface {
    Run(ctx context.Context) error
}
```

### Services Layer (`internal/core/services/`)

Implements core business logic:

- **`molfar.go`** - Central orchestration engine coordinating all operations
- **`librarian.go`** - Manifest synchronization and management
- **`validator.go`** - Instance integrity and conflict validation
- **`backupper_local.go`** - Local backup service with streaming tar.gz
- **`backupper_r2.go`** - R2 backup service with streaming tar.gz
- **`updater_ritual.go`** - Ritual self-update service (compares versions, downloads, replaces)
- **`updater_instance.go`** - Instance update service (downloads/extracts instance.tar.gz)
- **`updater_worlds.go`** - Worlds update service (downloads/extracts world backups)

#### Service Implementation Examples

```go
// internal/core/services/molfar.go
type MolfarService struct {
    updaters      []ports.UpdaterService   // Ordered list of updaters (ritual, instance, worlds)
    backuppers    []ports.BackupperService // Ordered list of backuppers (local, R2)
    serverRunner  ports.ServerRunner
    librarian     ports.LibrarianService
    logger        *slog.Logger
    workRoot      *os.Root
    currentLockID string // Tracks lock ownership for validation
}

func NewMolfarService(
    updaters []ports.UpdaterService,
    backuppers []ports.BackupperService,
    serverRunner ports.ServerRunner,
    librarian ports.LibrarianService,
    logger *slog.Logger,
    workRoot *os.Root,
) (*MolfarService, error)

// Prepare runs all updaters in sequence
func (m *MolfarService) Prepare() error

// Run executes server with lock management
func (m *MolfarService) Run(server *domain.Server) error

// Exit runs all backuppers and releases locks
func (m *MolfarService) Exit() error

// internal/core/services/backupper_local.go
type LocalBackupper struct {
    localStorage ports.StorageRepository
    workRoot     *os.Root
}

func (b *LocalBackupper) Run(ctx context.Context) (string, error) {
    // Streams world directories directly to tar.gz using streamer.Push
    // Applies retention policy after successful backup
}

// internal/core/services/backupper_r2.go
type R2Backupper struct {
    uploader      streamer.S3StreamUploader
    remoteStorage ports.StorageRepository
    bucket        string
    workRoot      *os.Root
    localPath     string      // Optional local backup path
    shouldBackup  func() bool // Condition for local backup
}

func (b *R2Backupper) Run(ctx context.Context) (string, error) {
    // Streams world directories directly to R2 with optional local copy
    // Applies retention policy after successful backup
}

// internal/core/services/updater_ritual.go
type RitualUpdater struct {
    librarian     ports.LibrarianService
    storage       ports.StorageRepository
    binaryVersion string // Compiled-in version string
}

func (u *RitualUpdater) Run(ctx context.Context) error {
    // Compares binaryVersion with remote manifest RitualVersion
    // Downloads new binary, writes to temp, launches with --replace-old flag
    // Handles Windows-compatible self-update process
}

// internal/core/services/updater_instance.go
type InstanceUpdater struct {
    librarian  ports.LibrarianService
    validator  ports.ValidatorService
    downloader streamer.S3StreamDownloader
    bucket     string
    workRoot   *os.Root
}

func (u *InstanceUpdater) Run(ctx context.Context) error {
    // Checks if local instance version differs from remote
    // Downloads and extracts instance.tar.gz using streamer.Pull
}

// internal/core/services/updater_worlds.go
type WorldsUpdater struct {
    librarian  ports.LibrarianService
    validator  ports.ValidatorService
    downloader streamer.S3StreamDownloader
    bucket     string
    workRoot   *os.Root
}

func (u *WorldsUpdater) Run(ctx context.Context) error {
    // Checks if local worlds are outdated
    // Downloads and extracts world archive using streamer.Pull
}
```

### Adapters Layer (`internal/adapters/`)

Implements external system integrations:

- **`fs.go`** - Local filesystem storage implementation (StorageRepository)
- **`r2.go`** - Cloudflare R2 cloud storage implementation (StorageRepository)
- **`serverrunner.go`** - Server execution implementation (ServerRunner)
- **`commandexecutor.go`** - Command execution implementation (CommandExecutor)
- **`streamer/`** - Streaming archive operations (see Streaming Layer above)

#### Adapter Implementation Examples

```go
// internal/adapters/fs.go
type FSRepository struct {
    root *os.Root
}

func NewFSRepository(root *os.Root) (*FSRepository, error) {
    if root == nil {
        return nil, errors.New("root cannot be nil")
    }
    return &FSRepository{root: root}, nil
}

// Implements: Get, Put, Delete, List, Copy

// internal/adapters/r2.go
type R2Repository struct {
    client *s3.Client
    bucket string
}

func NewR2Repository(client *s3.Client, bucket string) *R2Repository {
    return &R2Repository{
        client: client,
        bucket: bucket,
    }
}

// Implements: Get, Put, Delete, List, Copy

// internal/adapters/serverrunner.go
type ServerRunner struct {
    homedir         string
    commandExecutor ports.CommandExecutor
}

func NewServerRunner(homedir string, commandExecutor ports.CommandExecutor) (*ServerRunner, error) {
    if homedir == "" {
        return nil, fmt.Errorf("homedir cannot be empty")
    }
    if commandExecutor == nil {
        return nil, fmt.Errorf("command executor cannot be nil")
    }
    return &ServerRunner{
        homedir:         homedir,
        commandExecutor: commandExecutor,
    }, nil
}

func (s *ServerRunner) Run(server *domain.Server) error {
    // Execute Minecraft server process using command executor
    // Validates server configuration and executes server.bat
    // Returns error if server.bat not found or execution fails
}

// internal/adapters/commandexecutor.go
type CommandExecutor struct{}

func NewCommandExecutor() *CommandExecutor {
    return &CommandExecutor{}
}

func (e *CommandExecutor) Execute(command string, args []string, workingDir string) error {
    // Executes shell command with arguments in specified working directory
}
```

## Key Design Principles

### Hexagonal Architecture Benefits

1. **Separation of Concerns** - Each layer has distinct responsibilities
2. **Testability** - Easy to mock dependencies for unit testing
3. **Flexibility** - Can swap storage backends or add new integrations
4. **Maintainability** - Clear interfaces make code easier to understand
5. **Technology Agnostic** - Core logic independent of external systems

### Domain-Driven Design

- **Mystical Naming** - Preserves the ritualistic theme (Molfar, Librarian, Validator)
- **Business Focus** - Structure reflects Minecraft server orchestration domain
- **Clear Boundaries** - Each component has well-defined responsibilities

## Component Responsibilities

### Molfar (Orchestration Engine)
- Coordinates initialization (Prepare), execution (Run), and termination (Exit) phases
- Manages ordered execution of updaters during Prepare phase
- Manages ordered execution of backuppers during Exit phase
- Handles lock acquisition, validation, and release
- Coordinates server execution via ServerRunner

### Librarian (Manifest Management)
- Synchronizes local and remote manifests
- Manages lock mechanisms for concurrency control
- Handles version control and consistency
- Uses config.ManifestFilename for consistent file naming

### Validator (Validation System)
- Performs instance integrity checks (CheckInstance)
- Validates world data consistency (CheckWorld)
- Enforces lock mechanism compliance (CheckLock)
- Returns typed errors for different validation failures

### Updaters (Update Services)
Three specialized updaters execute during Prepare phase:

- **RitualUpdater**: Self-update for the ritual binary
  - Compares compiled-in version with remote manifest
  - Downloads new binary from R2 storage
  - Windows-compatible self-replacement process

- **InstanceUpdater**: Minecraft server instance updates
  - Compares local/remote instance versions
  - Downloads and extracts instance.tar.gz
  - Initializes new instances when local manifest missing

- **WorldsUpdater**: World data updates
  - Compares local/remote world versions
  - Downloads and extracts world archives
  - Handles "no worlds" case for fresh instances

### Backuppers (Backup Services)
Two specialized backuppers execute during Exit phase:

- **LocalBackupper**: Local filesystem backups
  - Streams world directories to tar.gz
  - Applies count-based retention (config.LocalMaxBackups)
  - Uses streamer.Push with LocalFileWriter

- **R2Backupper**: Cloud storage backups
  - Streams world directories directly to R2
  - Optional local copy via ShouldBackup condition
  - Applies count-based retention (config.R2MaxBackups)

### Retention (Data Lifecycle Management)
- **Built into Backuppers**: Each backupper has its own applyRetention() method
- **Count-Based**: LocalMaxBackups (10) and R2MaxBackups (5)
- **Sorted by Timestamp**: Newest backups kept, oldest deleted
- **Bounded Operations**: MaxFiles limit prevents runaway operations


### Test Helpers (`internal/testhelpers/`)

Provides comprehensive test utilities for Minecraft server testing:

- **`paperinstancesetup.go`** - Creates complete Paper Minecraft server instances for testing
  - Generates server files: `server.properties`, `server.jar`, `eula.txt`, `bukkit.yml`, `spigot.yml`, `paper.yml`
  - Creates plugin files: `worldedit`, `essentials`, `luckperms`, `vault` (jars and configs)
  - Creates logs directory with `latest.log` and `debug.log`
  - Accepts version parameter for `paper.yml` configuration
  - Returns temp directory path, created files list, and comparison function
  - Uses `os.Root` parameter for secure file operations (string paths removed)

- **`paperworldsetup.go`** - Creates Paper Minecraft world directories with region files
  - Generates world directory structure using os.Root for secure operations
  - Creates mock region files (.mca)
  - Creates level.dat and other world metadata files
  - Supports multiple world types (overworld, nether, end)
  - All operations use Root methods (Mkdir, WriteFile, etc.)

**Test Coverage**: Both helpers include comprehensive test suites with version validation, file structure verification, and comparison function testing. All test initialization uses os.OpenRoot before calling helper functions.

### Storage Abstraction
- Unified interface for local (filesystem) and remote (R2) storage
- Supports manifest, world data, and backup operations
- Provides Copy operation for efficient data movement
- Enables easy switching between storage backends
- Uses os.Root for all filesystem operations to prevent path traversal attacks

### Root-Based Security Architecture

All filesystem operations use `os.Root` to enforce secure path boundaries:

- **Path Security**: No string-based path construction - all operations constrained to Root boundary
- **Initialization Pattern**: `root, err := os.OpenRoot(basePath)` creates secured root
- **Service Integration**: All services (Molfar, Backupper, Archive) store `*os.Root` instead of string paths
- **Adapter Pattern**: FSRepository and other adapters accept Root in constructors
- **Operation Methods**: Use Root.Mkdir(), Root.ReadFile(), Root.WriteFile(), etc.
- **Zero Path Traversal**: Impossible to escape root boundary with string manipulation
- **Test Pattern**: Tests create roots before initializing services: `tempRoot, err := os.OpenRoot(tempDir)`

**Migration Complete**: root.md documents full conversion from string-based paths to os.Root across all layers (Phases 1-4 complete, Phase 5 remaining for adapter review).

### Logging
- Uses standard `log/slog` package
- Logger instance passed via dependency injection to services
- Structured logging with contextual fields

## Development Guidelines

### File Naming Conventions
- Use concise names: `fs.go` not `filesystem.go`
- Technology-specific: `r2.go`, `minecraft.go`
- Avoid underscores: Use camelCase for Go conventions

### Interface Design
- Keep interfaces focused and cohesive
- Define contracts at the ports layer
- Implement concrete types in adapters layer

### Testing Strategy
- Mock external dependencies through interfaces
- Test business logic in isolation
- Integration tests for adapter implementations
- Use testify framework for assertions and mocking

### Mock Testing Strategy
- **Comprehensive Mock Coverage** - All ports have fully tested mock implementations
- **Isolated Development** - Modules can be developed and tested independently using mocks
- **Error Simulation** - Mocks support configurable error conditions for robust testing
- **Concurrency Testing** - All mocks are thread-safe and support concurrent testing
- **Call Verification** - Mocks track method calls for verification and debugging
- **State Management** - Mocks maintain realistic state for testing complex scenarios
- **Testify Integration** - Use testify/mock for mock implementations and testify/assert for assertions

## Architecture Compliance

### Defensive Programming Standards

R.I.T.U.A.L. enforces NASA JPL Power of Ten defensive programming standards for mission-critical reliability:

**MANDATORY RULES:**
1. **Nil Validation** - All constructors and functions accepting interface/pointer parameters MUST validate non-nil before use. Return error, not panic.
2. **Runtime Assertions** - Average ≥2 assertions per function. Check preconditions, postconditions, invariants.
3. **Error Handling** - Check ALL return values from non-void functions. No ignored errors.
4. **Input Validation** - Validate all function parameters at entry. Check ranges, bounds, nil pointers.
5. **Fixed Bounds** - All loops must have statically determinable upper bounds.
6. **Function Size** - Limit functions to 60 lines. Extract complex logic.
7. **Pointer Safety** - Minimize pointer indirection. Validate before dereferencing.
8. **Memory Safety** - No dynamic heap allocation after initialization phase.
9. **Compiler Warnings** - Enable all warnings (-Wall -Wextra). Zero tolerance for warnings.
10. **Static Analysis** - Run static analysis tools. Fix all findings before merge.

**Implementation Requirements:**
- Reference [NASA JPL Power of Ten Rules](https://spinroot.com/static10/Src/DOC/PowerOfTen.pdf)
- Follow defensive programming patterns in `docs/coding-practices.md`
- All code must pass static analysis with zero warnings
- Functions must include pre/post condition assertions
- Error propagation must be explicit and handled at every layer

### Retention Policy Compliance

**CRITICAL PATH REQUIREMENTS:**
- **Backupper Integration**: All retention decisions flow through Backupper component orchestration
- **Performance Compliance**: O(n log n) sorting algorithms, bounded operations
- **Data Integrity**: Backup verification before deletion
- **Strategy Pattern**: Configurable retention strategies injected via `markForCleanup` function
- **Configuration Management**: Structured configuration objects with weighted scoring

**Retention Categories:**
- **World Retention**: Time-based with usage weighting (configurable max)
- **Local Backup Retention**: Dual-criteria time+count (configurable limits)

**Compliance Validation:**
- All retention operations must pass integrity validation
- Retention decisions must be logged with reasoning
- Failed retention operations must support rollback
- Concurrent retention operations must be thread-safe

## Documentation Requirements
- Each component must have GoDoc comments
- Architecture decisions must be documented
- API contracts must be specified
- Error handling strategies documented
- **Always reference @structure.md for architectural context and component relationships**
- Update structure.md when adding new components or changing architecture
- **AI must update progress tracking in docs/progress.md when implementing components**
- **MANDATORY**: Follow defensive programming standards per NASA JPL Power of Ten

## Main Initialization Pattern

### Service Integration Pattern

```go
// Typical initialization flow
func main() {
    // 1. Open root for filesystem operations
    workRoot, err := os.OpenRoot(config.RootPath)
    if err != nil {
        log.Fatal(err)
    }

    // 2. Create storage adapters
    localStorage, _ := adapters.NewFSRepository(workRoot)
    r2Client := createR2Client() // AWS SDK configuration
    remoteStorage := adapters.NewR2Repository(r2Client, bucketName)

    // 3. Create core services
    librarian, _ := services.NewLibrarianService(localStorage, remoteStorage)
    validator, _ := services.NewValidatorService()

    // 4. Create updaters (order matters: ritual → instance → worlds)
    updaters := []ports.UpdaterService{
        ritualUpdater,
        instanceUpdater,
        worldsUpdater,
    }

    // 5. Create backuppers (order matters: local → R2)
    backuppers := []ports.BackupperService{
        localBackupper,
        r2Backupper,
    }

    // 6. Create Molfar orchestrator
    molfar, _ := services.NewMolfarService(
        updaters,
        backuppers,
        serverRunner,
        librarian,
        slog.Default(),
        workRoot,
    )

    // 7. Execute lifecycle
    molfar.Prepare()
    molfar.Run(server)
    molfar.Exit()
}
```

### Test Setup Pattern

```go
// Test setup pattern (internal/core/services/molfar_test.go)
tempDir := t.TempDir()
tempRoot, err := os.OpenRoot(tempDir)
require.NoError(t, err)

// Create mock dependencies
mockLibrarian := mocks.NewMockLibrarianService()
mockValidator := mocks.NewMockValidatorService()
mockUpdaters := []ports.UpdaterService{mocks.NewMockUpdaterService()}
mockBackuppers := []ports.BackupperService{mocks.NewMockBackupperService()}
mockServerRunner := mocks.NewMockServerRunner()

// Create service under test
molfar, err := services.NewMolfarService(
    mockUpdaters,
    mockBackuppers,
    mockServerRunner,
    mockLibrarian,
    slog.Default(),
    tempRoot,
)
```

## Structure.md Authority
- **@structure.md is the authoritative source for project structure**
- All architectural decisions must align with structure.md definitions
- When in doubt about component placement or naming, consult structure.md first
- Structure.md contains detailed examples and implementation patterns
- Any structural changes require updating both code and structure.md documentation

This structure ensures R.I.T.U.A.L. maintains clean architecture while supporting the complex requirements of Minecraft server orchestration, manifest management, distributed storage synchronization, and centralized logging infrastructure.

