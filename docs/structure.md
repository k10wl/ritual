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
│   └── ritual.drawio            # Architecture diagrams
└── internal/
    ├── adapters/
    │   ├── cli.go               # CLI command handler
    │   ├── fs.go                # Local filesystem storage adapter
    │   ├── r2.go                # Cloudflare R2 storage adapter
    │   ├── serverrunner.go      # Server execution adapter
    │   └── commandexecutor.go   # Command execution adapter
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
        │       └── serverrunner_test.go # ServerRunner mock tests
        └── services/
            ├── archive.go       # Archive management service
            ├── archive_test.go  # ArchiveService tests
            ├── librarian.go     # Manifest management service
            ├── librarian_test.go # LibrarianService tests
            ├── molfar.go        # Main orchestration service
            ├── validator.go     # Validation service
            └── validator_test.go # ValidatorService tests
```

## Architecture Layers

### Core Domain Layer (`internal/core/domain/`)

Contains the core business entities:

- **`manifest.go`** - Central manifest tracking instance/worlds versions, locks, and metadata
- **`server.go`** - Server configuration entity with address parsing and validation
- **`world.go`** - World data entity with URI validation and timestamp tracking

#### Domain Entity Examples

```go
// internal/core/domain/manifest.go
type Manifest struct {
    Version      string    `json:"version"`
    LockedBy     string    `json:"locked_by"`     // {PC name}__{UNIX timestamp on 0 meridian}, or empty string if not locked
    InstanceID   string    `json:"instance_id"`
    StoredWorlds []World   `json:"worlds"`        // queue of latest worlds
    UpdatedAt    time.Time `json:"updated_at"`
}

type World struct {
    URI       string    `json:"uri"`
    CreatedAt time.Time `json:"created_at"`
}

func NewWorld(uri string) (*World, error) {
    if uri == "" {
        return nil, fmt.Errorf("URI cannot be empty")
    }
    return &World{
        URI:       uri,
        CreatedAt: time.Now(),
    }, nil
}

func (m *Manifest) IsLocked() bool {
    return m.LockedBy != ""
}

func (m *Manifest) Lock(lockBy string) {
    m.LockedBy = lockBy
    m.UpdatedAt = time.Now()
}
```

### Ports Layer (`internal/core/ports/`)

Defines interfaces for external dependencies and provides comprehensive mock implementations for testing:

- **`ports.go`** - All service and repository interfaces
  - `StorageRepository` - Storage operations interface
  - `MolfarService` - Main orchestration interface
  - `LibrarianService` - Manifest management interface
  - `ValidatorService` - Validation interface
  - `ArchiveService` - Archive management interface
  - `BackupperService` - Backup orchestration interface
  - `ServerRunner` - Server execution interface

- **Mock Implementations** (`mocks/` folder) - Complete mock implementations with test coverage
  - `storage.go` - MockStorageRepository with comprehensive testing utilities
  - `molfar.go` - MockMolfarService with status tracking and error simulation
  - `librarian.go` - MockLibrarianService with manifest synchronization logic
  - `validator.go` - MockValidatorService with configurable validation results
  - `archive.go` - MockArchiveService with archive operation simulation
  - `serverrunner.go` - MockServerRunner with server execution simulation

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
    Run() error
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

type ArchiveService interface {
    Archive(ctx context.Context, source string, destination string) error
    Unarchive(ctx context.Context, archive string, destination string) error
}

type BackupperService interface {
    Run() error
}
```

### Services Layer (`internal/core/services/`)

Implements core business logic:

- **`molfar.go`** - Central orchestration engine coordinating all operations
- **`librarian.go`** - Manifest synchronization and management
- **`validator.go`** - Instance integrity and conflict validation
- **`archive.go`** - Archive compression and extraction operations
- **`backupper.go`** - Backup orchestration engine with template method pattern

#### Service Implementation Examples

```go
// internal/core/services/molfar.go
type MolfarService struct {
    librarian LibrarianService
    validator ValidatorService
    storage   StorageRepository
}

func NewMolfarService(librarian LibrarianService, validator ValidatorService, storage StorageRepository) *MolfarService {
    return &MolfarService{
        librarian: librarian,
        validator: validator,
        storage:   storage,
    }
}

...

// internal/core/services/librarian.go
type LibrarianService struct {
    localStorage  StorageRepository
    remoteStorage StorageRepository
}

func NewLibrarianService(localStorage StorageRepository, remoteStorage StorageRepository) (*LibrarianService, error) {
    if localStorage == nil {
        return nil, fmt.Errorf("localStorage cannot be nil")
    }
    if remoteStorage == nil {
        return nil, fmt.Errorf("remoteStorage cannot be nil")
    }
    return &LibrarianService{
        localStorage: localStorage,
        remoteStorage: remoteStorage,
    }, nil
}

// internal/core/services/validator.go
type ValidatorService struct{}

func NewValidatorService() (*ValidatorService, error) {
    validator := &ValidatorService{}
    
    // Postcondition assertion (NASA JPL Rule 2)
    if validator == nil {
        return nil, ErrValidatorInitializationFailed
    }
    
    return validator, nil
}

func (v *ValidatorService) CheckInstance(local *domain.Manifest, remote *domain.Manifest) error {
    if v == nil {
        return errors.New("validator service cannot be nil")
    }
    if local == nil {
        return ErrLocalManifestNil
    }
    if remote == nil {
        return ErrRemoteManifestNil
    }
    // Additional validation logic...
}

// internal/core/services/backupper.go
type BackupperService struct {
    storage       StorageRepository
    archivePath   string
    buildArchive  func(string) (func(), error)
    markForCleanup func(StorageRepository) ([]domain.World, error)
}

func NewBackupperService(storage StorageRepository, archivePath string, buildArchive func(string) (func(), error), markForCleanup func(StorageRepository) ([]domain.World, error)) *BackupperService {
    return &BackupperService{
        storage:       storage,
        archivePath:   archivePath,
        buildArchive:  buildArchive,
        markForCleanup: markForCleanup,
    }
}

func (b *BackupperService) Run() error {
    // Check if archivePath exists
    // If missing → call buildArchive(targetPath) and defer cleanup()
    // validateArchive()
    // store()
    // applyRetention()
    // Return error if any step fails, else success
    ...
}

func (b *BackupperService) validateArchive() error {
    // Confirm archive exists, readable, and checksum valid
    ...
}

func (b *BackupperService) store() error {
    // Persist archive to storage backend
    ...
}

func (b *BackupperService) applyRetention() error {
    // Execute retention policy using markForCleanup and delete expired backups
    ...
}
```

### Adapters Layer (`internal/adapters/`)

Implements external system integrations:

- **`cli.go`** - Command-line interface handler
- **`fs.go`** - Local filesystem storage implementation
- **`r2.go`** - Cloudflare R2 cloud storage implementation
- **`serverrunner.go`** - Server execution implementation

#### Adapter Implementation Examples

```go
// internal/adapters/fs.go
type FSRepository struct {
    basePath string
}

func NewFSRepository(basePath string) *FSRepository {
    return &FSRepository{basePath: basePath}
}

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
- Coordinates initialization, execution, and termination phases
- Manages the complete server lifecycle
- Handles error recovery and cleanup

### Librarian (Manifest Management)
- Synchronizes local and remote manifests
- Manages lock mechanisms for concurrency control
- Handles version control and consistency

### Validator (Validation System)
- Performs instance integrity checks
- Validates world data consistency
- Enforces lock mechanism compliance
- Implements CheckInstance, CheckWorld, and CheckLock operations
- Provides comprehensive test coverage with testify framework

### Archive (Archive Management)
- Handles compression and extraction of data archives
- Supports ZIP archive format for world/plugin backups
- Manages archive lifecycle operations
- Integrates with storage abstraction for remote archive operations

### Backupper (Backup Orchestration Engine)
- **Template Method Pattern**: `Run()` defines fixed backup cycle skeleton with pluggable steps
- **Strategy Pattern**: `buildArchive` and `markForCleanup` are injected strategies for archive creation and cleanup
- **Facade Pattern**: Single `Run()` method hides internal orchestration complexity
- **Configuration-Driven**: Supports `storage` (local/cloud), `archivePath`, and strategy injection
- **Extensibility**: Archive creation, storage backend, and cleanup strategy can be swapped independently
- **Flow Orchestration**: Check archive existence → validate → store → apply retention → cleanup
- **Internal Methods**: `validateArchive()` confirms archive exists, readable, and checksum valid
- **Internal Methods**: `store()` persists archive to storage backend
- **Internal Methods**: `applyRetention()` executes retention policy using `markForCleanup` and deletes expired backups

### Retention (Data Lifecycle Management)
- **Backupper Integration**: Retention policies managed through Backupper component orchestration
- **Strategy Pattern**: Configurable retention strategies injected via `markForCleanup` function
- **Performance Compliance**: O(n log n) sorting algorithms, bounded operations
- **Data Integrity**: Backup verification before deletion
- **Configuration Management**: Structured configuration objects with weighted scoring
- **NASA JPL Compliance**: Defensive programming standards for mission-critical reliability


### Storage Abstraction
- Unified interface for local (filesystem) and remote (R2) storage
- Supports manifest, world data, and backup operations
- Provides Copy operation for efficient data movement
- Enables easy switching between storage backends

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

## Structure.md Authority
- **@structure.md is the authoritative source for project structure**
- All architectural decisions must align with structure.md definitions
- When in doubt about component placement or naming, consult structure.md first
- Structure.md contains detailed examples and implementation patterns
- Any structural changes require updating both code and structure.md documentation

This structure ensures R.I.T.U.A.L. maintains clean architecture while supporting the complex requirements of Minecraft server orchestration, manifest management, and distributed storage synchronization.
