package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"time"
)

// Molfar error constants
var (
	ErrLibrarianNil               = errors.New("librarian service cannot be nil")
	ErrValidatorNil               = errors.New("validator service cannot be nil")
	ErrServerRunnerNil            = errors.New("server runner cannot be nil")
	ErrMolfarInitializationFailed = errors.New("molfar initialization failed")
	ErrMolfarNil                  = errors.New("molfar service cannot be nil")
)

// MolfarService implements the main orchestration interface as a state machine
// Molfar coordinates the complete server lifecycle and manages all operations
type MolfarService struct {
	updaters      []ports.UpdaterService
	backuppers    []ports.BackupperService
	retentions    []ports.RetentionService
	serverRunner  ports.ServerRunner
	librarian     ports.LibrarianService
	events        chan<- ports.Event
	workRoot      *os.Root
	currentLockID string // Tracks the current lock ID for ownership validation (internal use only)
}

// NewMolfarService creates a new Molfar orchestration service
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewMolfarService(
	updaters []ports.UpdaterService,
	backuppers []ports.BackupperService,
	retentions []ports.RetentionService,
	serverRunner ports.ServerRunner,
	librarian ports.LibrarianService,
	events chan<- ports.Event,
	workRoot *os.Root,
) (*MolfarService, error) {
	if updaters == nil {
		return nil, errors.New("updaters slice cannot be nil")
	}
	for i, u := range updaters {
		if u == nil {
			return nil, fmt.Errorf("updater at index %d cannot be nil", i)
		}
	}
	if backuppers == nil {
		return nil, errors.New("backuppers slice cannot be nil")
	}
	for i, b := range backuppers {
		if b == nil {
			return nil, fmt.Errorf("backupper at index %d cannot be nil", i)
		}
	}
	if retentions == nil {
		return nil, errors.New("retentions slice cannot be nil")
	}
	for i, r := range retentions {
		if r == nil {
			return nil, fmt.Errorf("retention at index %d cannot be nil", i)
		}
	}
	if serverRunner == nil {
		return nil, ErrServerRunnerNil
	}
	if librarian == nil {
		return nil, ErrLibrarianNil
	}
	if workRoot == nil {
		return nil, errors.New("workRoot cannot be nil")
	}

	molfar := &MolfarService{
		updaters:     updaters,
		backuppers:   backuppers,
		retentions:   retentions,
		serverRunner: serverRunner,
		librarian:    librarian,
		events:       events,
		workRoot:     workRoot,
	}

	return molfar, nil
}

// send safely sends an event to the channel
func (m *MolfarService) send(evt ports.Event) {
	ports.SendEvent(m.events, evt)
}

// Prepare initializes the environment and validates prerequisites
// Runs all updaters in sequence
func (m *MolfarService) Prepare() error {
	if m == nil {
		return ErrMolfarNil
	}

	m.send(ports.StartEvent{Operation: "prepare"})
	m.send(ports.UpdateEvent{Operation: "prepare", Message: "Starting preparation phase", Data: map[string]any{"workRoot": m.workRoot.Name()}})
	ctx := context.Background()

	// Check if remote manifest is locked before running updates
	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "prepare", Err: err})
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest.IsLocked() {
		err := fmt.Errorf("remote manifest is locked by %s", remoteManifest.LockedBy)
		m.send(ports.ErrorEvent{Operation: "prepare", Err: err})
		return err
	}

	// Run all updaters
	for i, updater := range m.updaters {
		m.send(ports.StartEvent{Operation: "updater"})
		m.send(ports.UpdateEvent{Operation: "updater", Message: "Running updater", Data: map[string]any{"index": i}})
		if err := updater.Run(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "updater", Err: err})
			return fmt.Errorf("updater %d failed: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "updater"})
	}

	m.send(ports.UpdateEvent{Operation: "prepare", Message: "Preparation phase completed successfully"})
	m.send(ports.FinishEvent{Operation: "prepare"})
	return nil
}

// Run executes the main server orchestration process
// Already in Running state, coordinates server execution
func (m *MolfarService) Run(server *domain.Server) error {
	if m == nil {
		return ErrMolfarNil
	}
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if m.serverRunner == nil {
		return ErrServerRunnerNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "run"})
	m.send(ports.UpdateEvent{Operation: "run", Message: "Starting execution phase", Data: map[string]any{
		"server_address": server.Address,
		"server_memory":  server.Memory,
		"server_ip":      server.IP,
		"server_port":    server.Port,
	}})
	ctx := context.Background()

	// Fetch remote manifest before run
	remoteManifest, err := m.getRemoteManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return err
	}

	localManifest, err := m.validateAndRetrieveManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return err
	}

	if err := m.acquireManifestLocks(ctx, localManifest, remoteManifest); err != nil {
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return err
	}

	if err := m.executeServer(ctx, server); err != nil {
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return err
	}

	m.send(ports.UpdateEvent{Operation: "run", Message: "Execution phase completed"})
	m.send(ports.FinishEvent{Operation: "run"})
	return nil
}

// validateAndRetrieveManifest retrieves and validates the local manifest
func (m *MolfarService) validateAndRetrieveManifest(ctx context.Context) (*domain.Manifest, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if m.librarian == nil {
		return nil, ErrLibrarianNil
	}

	m.send(ports.UpdateEvent{Operation: "run", Message: "Retrieving local manifest for lock validation"})
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return nil, err
	}
	m.send(ports.UpdateEvent{Operation: "run", Message: "Retrieved local manifest", Data: map[string]any{
		"instance_version": localManifest.InstanceVersion,
		"ritual_version":   localManifest.RitualVersion,
	}})

	if localManifest.LockedBy != "" {
		err := errors.New("local manifest already locked")
		m.send(ports.ErrorEvent{Operation: "run", Err: err})
		return nil, err
	}
	m.send(ports.UpdateEvent{Operation: "run", Message: "Local manifest is unlocked, proceeding with lock acquisition"})

	return localManifest, nil
}

// acquireManifestLocks generates lock ID and acquires locks on both manifests
func (m *MolfarService) acquireManifestLocks(ctx context.Context, localManifest, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if localManifest == nil {
		return errors.New("local manifest cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "lock"})

	// Re-check lock status to prevent race condition between Prepare and Run
	if localManifest.LockedBy != "" {
		err := errors.New("local manifest already locked")
		m.send(ports.ErrorEvent{Operation: "lock", Err: err})
		return err
	}
	if remoteManifest.LockedBy != "" {
		err := errors.New("remote manifest already locked")
		m.send(ports.ErrorEvent{Operation: "lock", Err: err})
		return err
	}

	m.send(ports.UpdateEvent{Operation: "lock", Message: "Generating unique lock identifier"})
	hostname, err := os.Hostname()
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "lock", Err: err})
		return err
	}

	lockID := fmt.Sprintf("%s"+config.LockIDSeparator+"%d", hostname, time.Now().UnixNano())
	m.send(ports.UpdateEvent{Operation: "lock", Message: "Generated lock ID", Data: map[string]any{"lock_id": lockID}})
	localManifest.LockedBy = lockID
	remoteManifest.LockedBy = lockID

	m.send(ports.UpdateEvent{Operation: "lock", Message: "Acquiring local manifest lock"})
	err = m.librarian.SaveLocalManifest(ctx, localManifest)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "lock", Err: err})
		return err
	}
	m.send(ports.UpdateEvent{Operation: "lock", Message: "Successfully locked local manifest"})

	m.send(ports.UpdateEvent{Operation: "lock", Message: "Acquiring remote manifest lock"})
	err = m.librarian.SaveRemoteManifest(ctx, remoteManifest)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "lock", Err: fmt.Errorf("failed to lock remote manifest: %w", err)})

		// Rollback: unlock local manifest to prevent orphaned lock
		localManifest.Unlock()
		if rollbackErr := m.librarian.SaveLocalManifest(ctx, localManifest); rollbackErr != nil {
			m.send(ports.ErrorEvent{Operation: "lock", Err: fmt.Errorf("rollback failed: %w", rollbackErr)})
			return fmt.Errorf("failed to lock remote manifest: %w, rollback failed: %w", err, rollbackErr)
		}
		m.send(ports.UpdateEvent{Operation: "lock", Message: "Successfully rolled back local manifest lock"})

		return fmt.Errorf("failed to lock remote manifest: %w", err)
	}
	m.send(ports.UpdateEvent{Operation: "lock", Message: "Successfully locked remote storage", Data: map[string]any{"lock_id": lockID}})

	// Store lock ID for ownership validation
	m.currentLockID = lockID

	m.send(ports.FinishEvent{Operation: "lock"})
	return nil
}

// SetLockIDForTesting sets the current lock ID (for testing only)
// This is exported for testing purposes to simulate lock ownership
func (m *MolfarService) SetLockIDForTesting(lockID string) {
	m.currentLockID = lockID
}

// executeServer runs the server using the server runner
func (m *MolfarService) executeServer(ctx context.Context, server *domain.Server) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if m.serverRunner == nil {
		return ErrServerRunnerNil
	}

	m.send(ports.StartEvent{Operation: "server"})
	m.send(ports.UpdateEvent{Operation: "server", Message: "Starting server execution", Data: map[string]any{"server_address": server.Address}})
	err := m.serverRunner.Run(server)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "server", Err: err})
		return err
	}
	m.send(ports.UpdateEvent{Operation: "server", Message: "Server execution completed successfully"})
	m.send(ports.FinishEvent{Operation: "server"})

	return nil
}

// Exit gracefully shuts down the server and cleans up resources
// Runs all backuppers in sequence only if we own the lock
func (m *MolfarService) Exit() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "exit"})
	m.send(ports.UpdateEvent{Operation: "exit", Message: "Starting exit phase"})
	ctx := context.Background()

	// Skip backup and unlock if we don't own the lock
	if m.currentLockID == "" {
		m.send(ports.UpdateEvent{Operation: "exit", Message: "No lock owned, skipping backup and unlock"})
		m.send(ports.FinishEvent{Operation: "exit"})
		return nil
	}

	// Run all backuppers and collect archive names
	var lastArchiveName string
	for i, backupper := range m.backuppers {
		m.send(ports.StartEvent{Operation: "backup"})
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Running backupper", Data: map[string]any{"index": i}})
		archiveName, err := backupper.Run(ctx)
		if err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("backupper %d failed: %w", i, err)
		}
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Backupper completed", Data: map[string]any{"index": i, "archive_name": archiveName}})
		m.send(ports.FinishEvent{Operation: "backup"})
		lastArchiveName = archiveName
	}

	// Update manifests with last archive name (from any backupper)
	var updatedManifest *domain.Manifest
	if lastArchiveName != "" {
		manifest, err := m.updateManifestsWithArchive(ctx, lastArchiveName)
		if err != nil {
			m.send(ports.ErrorEvent{Operation: "exit", Err: err})
			return err
		}
		updatedManifest = manifest
	}

	// Apply retention policies after manifest is updated
	if updatedManifest != nil {
		for i, retention := range m.retentions {
			m.send(ports.StartEvent{Operation: "retention"})
			m.send(ports.UpdateEvent{Operation: "retention", Message: "Running retention", Data: map[string]any{"index": i}})
			if err := retention.Apply(ctx, updatedManifest); err != nil {
				m.send(ports.ErrorEvent{Operation: "retention", Err: err})
				return fmt.Errorf("retention %d failed: %w", i, err)
			}
			m.send(ports.FinishEvent{Operation: "retention"})
		}
	}

	// Unlock manifests after successful backup
	if err := m.unlockManifests(ctx); err != nil {
		m.send(ports.ErrorEvent{Operation: "exit", Err: err})
		return err
	}

	m.send(ports.UpdateEvent{Operation: "exit", Message: "Exit phase completed"})
	m.send(ports.FinishEvent{Operation: "exit"})
	return nil
}

// updateManifestsWithArchive updates both local and remote manifests with the new archive name
// Returns the updated manifest for use in retention policies
func (m *MolfarService) updateManifestsWithArchive(ctx context.Context, archiveName string) (*domain.Manifest, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if archiveName == "" {
		return nil, errors.New("archive name cannot be empty")
	}
	if m.librarian == nil {
		return nil, ErrLibrarianNil
	}

	m.send(ports.UpdateEvent{Operation: "exit", Message: "Updating manifests with new archive", Data: map[string]any{"archive_name": archiveName}})

	// Get local manifest
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		return nil, err
	}

	// Create new world entry with archive name
	world, err := domain.NewWorld(archiveName)
	if err != nil {
		return nil, err
	}

	// Add world to manifest
	localManifest.AddWorld(*world)

	// Save updated local manifest
	if err := m.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return nil, err
	}

	// Stamp RitualVersion before saving to remote
	localManifest.RitualVersion = config.AppVersion

	// Save updated remote manifest
	if err := m.librarian.SaveRemoteManifest(ctx, localManifest); err != nil {
		return nil, err
	}

	m.send(ports.UpdateEvent{Operation: "exit", Message: "Successfully updated manifests with archive", Data: map[string]any{"archive_name": archiveName}})
	return localManifest, nil
}

// unlockManifests removes locks from both local and remote manifests
func (m *MolfarService) unlockManifests(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "unlock"})
	m.send(ports.UpdateEvent{Operation: "unlock", Message: "Unlocking manifests"})

	// Get local manifest and validate ownership
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return err
	}

	// Validate ownership: ensure we own the lock
	if localManifest == nil {
		err := errors.New("local manifest cannot be nil")
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return err
	}

	// Check if manifest is locked
	if !localManifest.IsLocked() {
		m.send(ports.UpdateEvent{Operation: "unlock", Message: "Local manifest is already unlocked"})
		m.send(ports.FinishEvent{Operation: "unlock"})
		return nil
	}

	// Validate ownership: only unlock if we own the lock
	if m.currentLockID == "" {
		err := errors.New("lock ownership validation failed: no lock ID stored")
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return err
	}

	if localManifest.LockedBy != m.currentLockID {
		err := errors.New("lock ownership validation failed")
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return err
	}

	m.send(ports.UpdateEvent{Operation: "unlock", Message: "Validated lock ownership", Data: map[string]any{"lock_id": localManifest.LockedBy}})

	// Unlock both manifests
	localManifest.Unlock()
	err = m.librarian.SaveLocalManifest(ctx, localManifest)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return err
	}
	m.send(ports.UpdateEvent{Operation: "unlock", Message: "Successfully unlocked local manifest"})

	// Get remote manifest for unlock
	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
		return fmt.Errorf("local manifest unlocked but failed to unlock remote: %w", err)
	}

	if remoteManifest != nil {
		remoteManifest.Unlock()
		remoteManifest.RitualVersion = config.AppVersion
		err = m.librarian.SaveRemoteManifest(ctx, remoteManifest)
		if err != nil {
			m.send(ports.ErrorEvent{Operation: "unlock", Err: err})
			return fmt.Errorf("local manifest unlocked but failed to unlock remote: %w", err)
		}
		m.send(ports.UpdateEvent{Operation: "unlock", Message: "Successfully unlocked remote manifest"})
	}

	// Clear stored lock ID
	m.currentLockID = ""

	m.send(ports.UpdateEvent{Operation: "unlock", Message: "Successfully unlocked all manifests"})
	m.send(ports.FinishEvent{Operation: "unlock"})
	return nil
}

// Helper function for Run method
func (m *MolfarService) getRemoteManifest(ctx context.Context) (*domain.Manifest, error) {
	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remoteManifest == nil {
		return nil, errors.New("remote manifest cannot be nil")
	}
	m.send(ports.UpdateEvent{Operation: "run", Message: "Retrieved remote manifest", Data: map[string]any{
		"ritual_version":   remoteManifest.RitualVersion,
		"instance_version": remoteManifest.InstanceVersion,
	}})
	return remoteManifest, nil
}
