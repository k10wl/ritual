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

# >>> We are here

### Sprint 5: Retention Policy Implementation (1 week)
- [ ] **ROLLBACK**: Remove current O(n²) bubble sort from `RemoveOldestWorlds`
- [ ] **ROLLBACK**: Remove scattered retention logic from `manageWorldRetention`
- [ ] **ROLLBACK**: Remove dual-criteria conflict from `ManageLocalBackupRetention`
- [ ] **ROLLBACK**: Remove hardcoded retention limits and constants
- [ ] Implement centralized `RetentionPolicy` interface
- [ ] Create `RetentionEngine` with strategy pattern
- [ ] Implement efficient O(n log n) sorting algorithms
- [ ] Add data integrity validation before deletion
- [ ] Create retention configuration management
- [ ] Replace current retention implementation with centralized policy
- [ ] Add comprehensive retention policy tests
- [ ] Update `MolfarService` to use centralized retention

### Sprint 6: Integration Testing (1 week)
- [ ] Create end-to-end tests
- [ ] Validate system flow
- [ ] Update documentation
- [ ] Create deployment guide

## References
- Architecture: [structure.md](structure.md)
- Overview: [overview.md](overview.md)
- Project: [README.md](../README.md)
