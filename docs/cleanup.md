# Cleanup: Remove Backup Before World Replacement in Prepare Phase

## Overview
Remove redundant backup operations from the `updateLocalWorlds` function in the Prepare phase. The backup functionality is already properly handled in the Exit phase, making pre-update backups unnecessary.

## Problem Statement
The `updateLocalWorlds` function (lines 218-264 in `molfar.go`) performs a backup of existing worlds before replacing them:
- Backup creates unnecessary I/O operations during Prepare
- Backup functionality is already handled in Exit phase via `backupper.Run()`
- The `copyWorldsToBackup` method duplicates functionality

## Architectural Reasoning
The system already has a dedicated `backupper` service that handles all backup operations at the orchestration level. Having low-level backup logic in `updateLocalWorlds` violates the separation of concerns:

- **backupper service**: Handles complete backup lifecycle at orchestration level (exit phase)
- **Service integration level**: Should orchestrate operations, not perform low-level file operations
- **Redundancy**: Backup logic exists in two places with different implementations
- **Single Responsibility**: Molfar should coordinate, not implement backup details

The `copyWorldsToBackup` method duplicates responsibility that belongs to the backupper service, creating architectural inconsistency and unnecessary coupling.

## Current Implementation
```238:244:internal/core/services/molfar.go
	// Copy current worlds to backup
	m.logger.Info("Backing up current worlds", "source", InstanceDir, "backup", filepath.Join(BackupDir, PreUpdateDir))
	err := m.copyWorldsToBackup(InstanceDir, filepath.Join(BackupDir, PreUpdateDir))
	if err != nil {
		m.logger.Error("Failed to backup current worlds", "error", err)
		return fmt.Errorf("failed to backup current worlds: %w", err)
	}
```

## Desired State
Remove backup call from `updateLocalWorlds`, streamline the function to:
1. Download and extract new worlds directly
2. Update manifest with new world information

Exit phase will handle backups via `backupper.Run()`.

## Implementation Steps

### 1. Modify `updateLocalWorlds` Function
**File**: `internal/core/services/molfar.go`  
**Lines**: 218-264

Remove lines 238-244 (backup call and error handling).

Simplified flow:
1. Log world update start
2. Download and extract new worlds
3. Update manifest
4. Log completion

### 2. Review `copyWorldsToBackup` Function
**File**: `internal/core/services/molfar.go`  
**Lines**: 266-302

**Options**:
- Remove function entirely if unused elsewhere
- Keep function if used by other components (verify via grep)

### 3. Update Tests
**File**: `internal/core/services/molfar_test.go`

**Actions**:
- Verify tests don't assert backup operations in Prepare phase
- Remove/update test expectations for `updateLocalWorlds` behavior
- Ensure Exit phase backup tests remain intact

## Files to Modify
- `internal/core/services/molfar.go`
  - Remove backup logic from `updateLocalWorlds` (lines 238-244)
  - Evaluate removal of `copyWorldsToBackup` (lines 266-302)
- `internal/core/services/molfar_test.go`
  - Review and update test expectations

## Benefits
- Reduced I/O operations during Prepare phase
- Clearer separation of concerns (Prepare handles updates, Exit handles backups)
- Simpler control flow
- Consistent architecture alignment

## Notes
- Backup remains functional in Exit phase
- No risk of data loss as Exit phase creates proper backups
- PreUpdateDir constant (line 23) may become unused

