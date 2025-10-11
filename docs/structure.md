# R.I.T.U.A.L. Project Structure

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
    │   └── minecraft.go         # Minecraft server integration adapter
    └── core/
        ├── domain/
        │   ├── manifest.go      # Manifest entity
        ├── ports/
        │   └── ports.go         # Interface definitions
        └── services/
            ├── molfar.go        # Main orchestration service
            ├── librarian.go     # Manifest management service
            └── validator.go     # Validation service
```

## Architecture Layers

### Core Domain Layer (`internal/core/domain/`)

Contains the core business entities:

- **`manifest.go`** - Central manifest tracking instance/worlds versions, locks, and metadata

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

func (m *Manifest) IsLocked() bool {
    return m.LockedBy != ""
}

func (m *Manifest) Lock(lockBy string) {
    m.LockedBy = lockBy
    m.LastUpdated = time.Now()
}
```

### Ports Layer (`internal/core/ports/`)

Defines interfaces for external dependencies:

- **`ports.go`** - All service and repository interfaces
  - `StorageRepository` - Storage operations interface
  - `MolfarService` - Main orchestration interface
  - `LibrarianService` - Manifest management interface
  - `ValidatorService` - Validation interface
  - `MinecraftAdapter` - Server control interface

#### Port Interface Examples

```go
// internal/core/ports/ports.go
type StorageRepository interface {
    Get(key string) ([]byte, error)
    Put(key string, data []byte) error
    Delete(key string) error
    List(prefix string) ([]string, error)
}

type MolfarService interface {
    Prepare() error
    Run() error
    Exit() error
}

type LibrarianService interface {
    GetLocalManifest() (*domain.Manifest, error)
    GetRemoteManifest() (*domain.Manifest, error)
    SaveLocalManifest(manifest *domain.Manifest) error
    SaveRemoteManifest(manifest *domain.Manifest) error
}

type ValidatorService interface {
    ValidateManifest(manifest *domain.Manifest) error
    ValidateServer(server *domain.Server) error
    CheckLock(local *domain.Manifest, remote *domain.Manifest) error
}
```

### Services Layer (`internal/core/services/`)

Implements core business logic:

- **`molfar.go`** - Central orchestration engine coordinating all operations
- **`librarian.go`** - Manifest synchronization and management
- **`validator.go`** - Instance integrity and conflict validation

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
    storage StorageRepository
}

func NewLibrarianService(localStorage StorageRepository, remoteStorage StorageRepository) *LibrarianService {
    return &LibrarianService{storage: storage}
}

// internal/core/services/validator.go
type ValidatorService struct{}

func NewValidatorService() *ValidatorService {
    return &ValidatorService{}
}
```

### Adapters Layer (`internal/adapters/`)

Implements external system integrations:

- **`cli.go`** - Command-line interface handler
- **`fs.go`** - Local filesystem storage implementation
- **`r2.go`** - Cloudflare R2 cloud storage implementation
- **`minecraft.go`** - Minecraft server control implementation

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

### Storage Abstraction
- Unified interface for local (filesystem) and remote (R2) storage
- Supports manifest, world data, and backup operations
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

This structure ensures R.I.T.U.A.L. maintains clean architecture while supporting the complex requirements of Minecraft server orchestration, manifest management, and distributed storage synchronization.
