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
  - Maintains up to 5 historical world/plugin backups
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

### Environment Configuration

Create environment configuration files:

• **Copy `.env.example` to `.env`**
• **Configure R2 storage credentials**

Required environment variables:
- `R2_ACCOUNT_ID` - Cloudflare R2 Account ID
- `R2_ACCESS_KEY_ID` - Cloudflare R2 Access Key ID  
- `R2_SECRET_ACCESS_KEY` - Cloudflare R2 Secret Access Key
- `R2_BUCKET_NAME` - R2 Bucket Name

### Quick Start

1. Clone repository
2. Copy `.env.example` to `.env`
3. Fill in R2 credentials
4. Run `go mod tidy`
5. Execute `go run cmd/cli/main.go`

## Development & Quality Assurance

### CI/CD Pipeline

The project uses GitHub Actions workflow:

• **Test Pipeline** - Runs on Windows with Go 1.25

Configuration files:
- `.github/workflows/ci.yml` - GitHub Actions workflow