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
	conditions    []ports.ConditionService
	updaters      []ports.UpdaterService
	exitUpdaters  []ports.UpdaterService
	retentions    []ports.RetentionService
	serverRunner  ports.ServerRunner
	librarian     ports.LibrarianService
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
	events        chan<- ports.Event
	workRoot      *os.Root
	currentLockID string // Tracks the current lock ID for ownership validation (internal use only)
}

// NewMolfarService creates a new Molfar orchestration service
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewMolfarService(
	conditions []ports.ConditionService,
	updaters []ports.UpdaterService,
	exitUpdaters []ports.UpdaterService,
	retentions []ports.RetentionService,
	serverRunner ports.ServerRunner,
	librarian ports.LibrarianService,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	events chan<- ports.Event,
	workRoot *os.Root,
) (*MolfarService, error) {
	if conditions == nil {
		return nil, errors.New("conditions slice cannot be nil")
	}
	for i, c := range conditions {
		if c == nil {
			return nil, fmt.Errorf("condition at index %d cannot be nil", i)
		}
	}
	if updaters == nil {
		return nil, errors.New("updaters slice cannot be nil")
	}
	for i, u := range updaters {
		if u == nil {
			return nil, fmt.Errorf("updater at index %d cannot be nil", i)
		}
	}
	if exitUpdaters == nil {
		return nil, errors.New("exitUpdaters slice cannot be nil")
	}
	for i, u := range exitUpdaters {
		if u == nil {
			return nil, fmt.Errorf("exit updater at index %d cannot be nil", i)
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
	if localStorage == nil {
		return nil, errors.New("localStorage cannot be nil")
	}
	if remoteStorage == nil {
		return nil, errors.New("remoteStorage cannot be nil")
	}
	if workRoot == nil {
		return nil, errors.New("workRoot cannot be nil")
	}

	molfar := &MolfarService{
		conditions:    conditions,
		updaters:      updaters,
		exitUpdaters:  exitUpdaters,
		retentions:    retentions,
		serverRunner:  serverRunner,
		librarian:     librarian,
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
		events:        events,
		workRoot:      workRoot,
	}

	return molfar, nil
}

// send safely sends an event to the channel
func (m *MolfarService) send(evt ports.Event) {
	ports.SendEvent(m.events, evt)
}

// Prepare initializes the environment and validates prerequisites
// Runs all conditions first, then all updaters in sequence
func (m *MolfarService) Prepare() error {
	if m == nil {
		return ErrMolfarNil
	}

	m.send(ports.StartEvent{Operation: "prepare"})
	m.send(ports.UpdateEvent{Operation: "prepare", Message: "Starting preparation phase", Data: map[string]any{"workRoot": m.workRoot.Name()}})
	ctx := context.Background()

	// Run all conditions first (includes manifest lock check)
	for i, condition := range m.conditions {
		m.send(ports.StartEvent{Operation: "condition"})
		m.send(ports.UpdateEvent{Operation: "condition", Message: "Checking condition", Data: map[string]any{"index": i}})
		if err := condition.Check(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "condition", Err: err})
			return fmt.Errorf("condition %d failed: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "condition"})
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
func (m *MolfarService) Run(server *domain.ServerRuntime) error {
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
		"ritual_version": localManifest.RitualVersion,
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
func (m *MolfarService) executeServer(ctx context.Context, server *domain.ServerRuntime) error {
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

// Exit gracefully shuts down: delta-sync upload, snapshot (if dirty), retention, unlock.
// Only runs if we own the lock.
func (m *MolfarService) Exit() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "exit"})
	m.send(ports.UpdateEvent{Operation: "exit", Message: "Starting exit phase"})
	defer m.send(ports.FinishEvent{Operation: "exit"})

	if m.currentLockID == "" {
		m.send(ports.UpdateEvent{Operation: "exit", Message: "No lock owned, skipping exit flow"})
		return nil
	}

	ctx := context.Background()

	// Capture pre-sync state. ShouldBackup compares local vs remote xxhash maps
	// to detect whether this session produced any world changes worth snapshotting.
	// Must capture before exit updaters run (sync would zero the diff).
	localBefore, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "exit", Err: err})
		return fmt.Errorf("get local manifest: %w", err)
	}
	remoteBefore, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.send(ports.ErrorEvent{Operation: "exit", Err: err})
		return fmt.Errorf("get remote manifest: %w", err)
	}

	// Delta sync upload.
	for i, u := range m.exitUpdaters {
		m.send(ports.StartEvent{Operation: "exit-updater"})
		m.send(ports.UpdateEvent{Operation: "exit-updater", Message: "Running exit updater", Data: map[string]any{"index": i}})
		if err := u.Run(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "exit-updater", Err: err})
			return fmt.Errorf("exit updater %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "exit-updater"})
	}

	// Snapshot backups only if worlds were dirty before sync.
	if ShouldBackup(localBefore.Worlds.SyncState, remoteBefore.Worlds.SyncState) {
		manifestAfter, err := m.librarian.GetLocalManifest(ctx)
		if err != nil {
			m.send(ports.ErrorEvent{Operation: "exit", Err: err})
			return fmt.Errorf("get manifest for backup: %w", err)
		}

		m.send(ports.StartEvent{Operation: "backup"})
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Creating local snapshot"})
		if err := CreateBackup(ctx, m.localStorage, config.WorldsDir, config.BackupsDir, manifestAfter); err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("local backup: %w", err)
		}
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Creating R2 snapshot"})
		if err := CreateBackup(ctx, m.remoteStorage, config.WorldsDir, config.BackupsDir, manifestAfter); err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("r2 backup: %w", err)
		}
		m.send(ports.FinishEvent{Operation: "backup"})
	} else {
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Worlds clean, skipping snapshot"})
	}

	// Retentions — always run.
	for i, r := range m.retentions {
		m.send(ports.StartEvent{Operation: "retention"})
		if err := r.Apply(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "retention", Err: err})
			return fmt.Errorf("retention %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "retention"})
	}

	return m.unlockManifests(ctx)
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

	// Unlock both manifests and stamp RitualVersion
	localManifest.Unlock()
	localManifest.RitualVersion = config.AppVersion
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
		"ritual_version": remoteManifest.RitualVersion,
	}})
	return remoteManifest, nil
}
