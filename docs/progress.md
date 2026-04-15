# R.I.T.U.A.L. Sprint Tracker

### Foundation Components (Completed)
- [x] Project structure established
- [x] Domain entity `manifest.go` implemented
- [x] Port interfaces defined
- [x] Storage adapters `fs.go` and `r2.go` implemented
- [x] Basic CLI `main.go` created
- [x] Mock framework established

### Sprint 1: Foundation Layer (1 week)
- [x] Implement `world.go` entity
- [x] Add `ServerRunner` interface
- [x] Complete mock implementations
- [x] Create domain entity tests

### Sprint 2: Services Layer (2 weeks)
- [x] Implement `LibrarianService`
- [x] Create `LibrarianService` tests
- [x] Add context parameter to LibrarianService operations
- [x] Implement `ValidatorService`
- [x] Create `ValidatorService` tests
- [x] Add defensive validation compliance
- [x] Implement comprehensive error handling for validation operations
- [x] Add service dependency injection
- [x] Implement `ArchiveService`
- [x] Create `ArchiveService` tests
- [x] Add `Copy` method to `StorageRepository` interface
- [x] Implement `Copy` method in `FSRepository` and `R2Repository`
- [x] Create `ArchiveService` mock with comprehensive tests
- [x] Update documentation for archive service integration
- [x] Complete ArchiveService implementation with zip compression/extraction
- [x] Add ArchiveService interface to ports.go
- [x] Implement comprehensive test coverage for ArchiveService
- [x] Create MockArchiveService with advanced verification methods

### Sprint 3: Adapters Layer (2 weeks)
- [x] Implement `ServerRunner`
- [x] Create `Server` domain entity
- [x] Implement `ServerRunnerService`
- [x] Create `ServerRunnerAdapter`
- [x] Add comprehensive test coverage
- [x] Create mock implementations
- [x] Implement `CommandExecutor` adapter
- [x] Add `CommandExecutor` mock implementation
- [x] Complete ServerRunner integration with CommandExecutor
- [x] Enhance CLI adapter
- [x] Complete storage adapter methods
- [x] Create adapter tests

### Sprint 4: Orchestration Engine (2 weeks)
- [x] Implement `MolfarService`
- [x] Add lifecycle management
- [x] Integrate all services
- [x] Create orchestration tests
- [x] Implement outdated instance handling
- [x] Add instance update mechanism
- [x] Implement local backup functionality
- [x] Add local backup retention management (5 files max)
- [x] Add time-based backup scheduling (every 2 months)
- [x] Create comprehensive local backup tests
- [x] **CRITICAL**: Identify retention policy violations
- [x] **CRITICAL**: Define centralized retention policy architecture
- [x] **CRITICAL**: Document retention compliance requirements
- [x] Implement `PaperInstanceSetup` test helper
- [x] Add comprehensive test suite for PaperInstanceSetup
- [x] Create version parameter support for paper.yml configuration

### Sprint 5: Backup/Retention Redesign Completed
- [x] Redesigned backup/retention architecture with pure functions and strategy pattern
- [x] See spec: `docs/superpowers/specs/2026-04-14-backup-retention-design.md`
- [x] See plan: `docs/superpowers/plans/2026-04-14-backup-retention.md`

### Sprint 6: State Machine + EventBus Migration (Completed)
- [x] Retry coverage prereq (P-B): inline retry in R2, r2_classify, decorator deleted, RetryAttemptInfo event
- [x] Manifest-store prereq (P-A): ports.ManifestStore (2 methods, 2 instances), Manifest.UnmarshalJSON applies defaults on decode
- [x] EventBus port + adapter (non-blocking Publish, multi-subscriber fan-out; filtering via subscriber-side type switch; Decorator extension point documented)
- [x] Prompter port (RPC extracted from events)
- [x] StateName + Handler interface (no hooks — brackets are state pairs)
- [x] Machine.Run loop with StateChangedInfo emission
- [x] StateFactory + Deps (ctor injection, no god-struct, no Builder)
- [x] States: Preparing, Locking, Running, Exiting, Unlocking, Failed (all with per-state tests + factory smoke)
- [x] Service migrations: r2, sync, retention_logs, settings — all use EventBus; SendEvent + PromptEvent removed
- [x] cmd/cli cutover — Molfar removed; Librarian removed after consumer migration (SyncDownloadUpdater, SyncUploader, RitualUpdater, ManifestLockCondition)
- [x] Docs: state-machine.md promoted, event-architecture.md rewritten, progress entry added
- [x] Plans: `docs/superpowers/plans/2026-04-15-state-machine.md`, `2026-04-16-manifest-store.md`, `2026-04-16-retry-coverage.md`

# >>> We are here

### Sprint 7: Logging Integration (1 week)
- [ ] Create centralized logging mechanism with structured logging
- [ ] Implement log level configuration and filtering
- [ ] Add log rotation and retention policies
- [ ] Integrate logging across all services and adapters
- [ ] Create comprehensive logging tests and validation

### Sprint 8: Implementation & Testing (1 week)
- [ ] Create end-to-end tests
- [ ] Validate system flow
- [ ] Update documentation
- [ ] Create deployment guide

## Test Helpers

### PaperInstanceSetup
**Location**: `internal/testhelpers/paperinstancesetup.go`

Creates complete Paper Minecraft server instances for testing purposes.

**Features**:
- Creates server files: `server.properties`, `server.jar`, `eula.txt`, `bukkit.yml`, `spigot.yml`, `paper.yml`
- Creates plugin files: `worldedit`, `essentials`, `luckperms`, `vault` (jars and configs)
- Creates logs directory with `latest.log` and `debug.log`
- Accepts version parameter for `paper.yml` configuration
- Returns temp directory path, created files list, and comparison function
- Uses `os.Root` for secure file operations

**Usage**:
```go
tempDir, createdFiles, compareFunc, err := testhelpers.PaperInstanceSetup(dir, "1.20.1")
```

**Test Coverage**: Comprehensive test suite in `paperinstancesetup_test.go` with version validation, file structure verification, and comparison function testing.

### PaperWorldSetup
**Location**: `internal/testhelpers/paperworldsetup.go`

Creates Paper Minecraft world directories with region files for testing.

**Features**:
- Creates world directory structure
- Generates mock region files (.mca)
- Creates level.dat and other world metadata files
- Supports multiple world types (overworld, nether, end)

## References
- Architecture: [structure.md](structure.md)
- Overview: [overview.md](overview.md)
- Project: [README.md](../README.md)
