# R.I.T.U.A.L. Project Overview

## Project Import Structure

All project imports follow the pattern: `ritual/...`

Example imports:
- `ritual/internal/core/domain` - Domain entities
- `ritual/internal/core/ports` - Interface definitions  
- `ritual/internal/core/services` - Business logic services
- `ritual/internal/adapters` - External system integrations

## High-Level Architecture

### Core Components

• **Molfar Orchestration Engine**
  - Central coordinator managing all system operations
  - Executes preparation, execution, and termination procedures
  - Coordinates validator, librarian, and storage components

• **Instance Management System**
  - Tracks server instance states and lifecycle
  - Manages instance provisioning and deprovisioning
  - Monitors resource utilization and availability

• **Manifest Management System (Librarian)**
  - Retrieves and stores local/remote manifest data
  - Manages manifest synchronization between storage repositories
  - Handles manifest version control and consistency

• **Validation System**
  - Performs instance integrity checks against manifests
  - Validates world data consistency
  - Enforces lock mechanism compliance
  - Implements CheckInstance, CheckWorld, and CheckLock operations

• **Storage Abstraction Layer**
  - Unified interface for local and remote data operations
  - Provides get, put, delete operations across storage backends
  - Supports distributed data persistence

### Operational Process Flow

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

### System Architecture

• **Distributed Architecture**
  - Multi-node server deployment with centralized coordination
  - Decentralized storage with local/remote synchronization
  - Lock-based concurrency control for safe operations

• **Data Flow**
  - Manifest-driven state management
  - Event-driven backup and update triggers
  - Asynchronous data synchronization between storage layers

• **Integration Points**
  - Minecraft server API integration for instance control
  - External backup storage systems via storage abstraction
  - Monitoring and alerting systems for operational oversight

### Testing Strategy
- Mock external dependencies through interfaces
- Test business logic in isolation
- Integration tests for adapter implementations
- Use testify framework for assertions and mocking