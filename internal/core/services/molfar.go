package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	logger        *slog.Logger
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
	logger *slog.Logger,
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
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
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
		logger:       logger,
		workRoot:     workRoot,
	}

	return molfar, nil
}

// Prepare initializes the environment and validates prerequisites
// Runs all updaters in sequence
func (m *MolfarService) Prepare() error {
	if m == nil {
		return ErrMolfarNil
	}

	m.logger.Info("Starting preparation phase", "workRoot", m.workRoot.Name())
	ctx := context.Background()

	// Run all updaters
	for i, updater := range m.updaters {
		m.logger.Info("Running updater", "index", i)
		if err := updater.Run(ctx); err != nil {
			m.logger.Error("Updater failed", "index", i, "error", err)
			return fmt.Errorf("updater %d failed: %w", i, err)
		}
		m.logger.Info("Updater completed", "index", i)
	}

	m.logger.Info("Preparation phase completed successfully")
	return nil
}

// Run executes the main server orchestration process
// Already in Running state, coordinates server execution
func (m *MolfarService) Run(server *domain.Server, sessionID string) error {
	if m == nil {
		return ErrMolfarNil
	}
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if sessionID == "" {
		return errors.New("sessionID cannot be empty")
	}
	if m.serverRunner == nil {
		return ErrServerRunnerNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.logger.Info("Starting execution phase", "server_address", server.Address, "server_memory", server.Memory, "server_ip", server.IP, "server_port", server.Port)
	ctx := context.Background()

	// Fetch remote manifest before run
	remoteManifest, err := m.getRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localManifest, err := m.validateAndRetrieveManifest(ctx)
	if err != nil {
		return err
	}

	if err := m.acquireManifestLocks(ctx, localManifest, remoteManifest, sessionID); err != nil {
		return err
	}

	if err := m.executeServer(ctx, server); err != nil {
		return err
	}

	m.logger.Info("Execution phase completed")
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

	m.logger.Info("Retrieving local manifest for lock validation")
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get local manifest", "error", err)
		return nil, err
	}
	m.logger.Info("Retrieved local manifest", "instance_version", localManifest.InstanceVersion, "ritual_version", localManifest.RitualVersion)

	if localManifest.LockedBy != "" {
		m.logger.Error("Local manifest already locked", "locked_by", localManifest.LockedBy)
		return nil, errors.New("local manifest already locked")
	}
	m.logger.Info("Local manifest is unlocked, proceeding with lock acquisition")

	return localManifest, nil
}

// acquireManifestLocks generates lock ID and acquires locks on both manifests
func (m *MolfarService) acquireManifestLocks(ctx context.Context, localManifest, remoteManifest *domain.Manifest, sessionID string) error {
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

	// Re-check lock status to prevent race condition between Prepare and Run
	if localManifest.LockedBy != "" {
		m.logger.Error("Local manifest already locked", "locked_by", localManifest.LockedBy)
		return errors.New("local manifest already locked")
	}
	if remoteManifest.LockedBy != "" {
		m.logger.Error("Remote manifest already locked", "locked_by", remoteManifest.LockedBy)
		return errors.New("remote manifest already locked")
	}

	m.logger.Info("Generating unique lock identifier")
	hostname, err := os.Hostname()
	if err != nil {
		m.logger.Error("Failed to get hostname", "error", err)
		return err
	}

	lockID := fmt.Sprintf("%s"+config.LockIDSeparator+"%d"+config.LockIDSeparator+"%s", hostname, time.Now().Unix(), sessionID)
	m.logger.Info("Generated lock ID", "lock_id", lockID)
	localManifest.LockedBy = lockID
	remoteManifest.LockedBy = lockID

	m.logger.Info("Acquiring local manifest lock")
	err = m.librarian.SaveLocalManifest(ctx, localManifest)
	if err != nil {
		m.logger.Error("Failed to lock local manifest", "error", err)
		return err
	}
	m.logger.Info("Successfully locked local manifest")

	m.logger.Info("Acquiring remote manifest lock")
	err = m.librarian.SaveRemoteManifest(ctx, remoteManifest)
	if err != nil {
		m.logger.Error("Failed to lock remote manifest, rolling back local lock", "error", err)

		// Rollback: unlock local manifest to prevent orphaned lock
		localManifest.Unlock()
		if rollbackErr := m.librarian.SaveLocalManifest(ctx, localManifest); rollbackErr != nil {
			m.logger.Error("Failed to rollback local manifest unlock", "error", rollbackErr)
			return fmt.Errorf("failed to lock remote manifest: %w, rollback failed: %w", err, rollbackErr)
		}
		m.logger.Info("Successfully rolled back local manifest lock")

		return fmt.Errorf("failed to lock remote manifest: %w", err)
	}
	m.logger.Info("Successfully locked remote storage", "lock_id", lockID)

	// Store lock ID for ownership validation
	m.currentLockID = lockID

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

	m.logger.Info("Starting server execution", "server_address", server.Address)
	err := m.serverRunner.Run(server)
	if err != nil {
		m.logger.Error("Server execution failed", "error", err)
		return err
	}
	m.logger.Info("Server execution completed successfully")

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

	m.logger.Info("Starting exit phase")
	ctx := context.Background()

	// Skip backup and unlock if we don't own the lock
	if m.currentLockID == "" {
		m.logger.Info("No lock owned, skipping backup and unlock")
		return nil
	}

	// Run all backuppers and collect archive names
	var lastArchiveName string
	for i, backupper := range m.backuppers {
		m.logger.Info("Running backupper", "index", i)
		archiveName, err := backupper.Run(ctx)
		if err != nil {
			m.logger.Error("Backupper failed", "index", i, "error", err)
			return fmt.Errorf("backupper %d failed: %w", i, err)
		}
		m.logger.Info("Backupper completed", "index", i, "archive_name", archiveName)
		lastArchiveName = archiveName
	}

	// Update manifests with last archive name (from any backupper)
	var updatedManifest *domain.Manifest
	if lastArchiveName != "" {
		manifest, err := m.updateManifestsWithArchive(ctx, lastArchiveName)
		if err != nil {
			m.logger.Error("Failed to update manifests with archive", "error", err)
			return err
		}
		updatedManifest = manifest
	}

	// Apply retention policies after manifest is updated
	if updatedManifest != nil {
		for i, retention := range m.retentions {
			m.logger.Info("Running retention", "index", i)
			if err := retention.Apply(ctx, updatedManifest); err != nil {
				m.logger.Error("Retention failed", "index", i, "error", err)
				return fmt.Errorf("retention %d failed: %w", i, err)
			}
			m.logger.Info("Retention completed", "index", i)
		}
	}

	// Unlock manifests after successful backup
	if err := m.unlockManifests(ctx); err != nil {
		m.logger.Error("Failed to unlock manifests", "error", err)
		return err
	}

	m.logger.Info("Exit phase completed")
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

	m.logger.Info("Updating manifests with new archive", "archive_name", archiveName)

	// Get local manifest
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get local manifest for archive update", "error", err)
		return nil, err
	}

	// Create new world entry with archive name
	world, err := domain.NewWorld(archiveName)
	if err != nil {
		m.logger.Error("Failed to create world entry", "archive_name", archiveName, "error", err)
		return nil, err
	}

	// Add world to manifest
	localManifest.AddWorld(*world)

	// Save updated local manifest
	if err := m.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		m.logger.Error("Failed to save local manifest with archive", "error", err)
		return nil, err
	}

	// Save updated remote manifest
	if err := m.librarian.SaveRemoteManifest(ctx, localManifest); err != nil {
		m.logger.Error("Failed to save remote manifest with archive", "error", err)
		return nil, err
	}

	m.logger.Info("Successfully updated manifests with archive", "archive_name", archiveName)
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

	m.logger.Info("Unlocking manifests")

	// Get local manifest and validate ownership
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get local manifest for unlock", "error", err)
		return err
	}

	// Validate ownership: ensure we own the lock
	if localManifest == nil {
		m.logger.Error("Local manifest is nil")
		return errors.New("local manifest cannot be nil")
	}

	// Check if manifest is locked
	if !localManifest.IsLocked() {
		m.logger.Info("Local manifest is already unlocked")
		return nil
	}

	// Validate ownership: only unlock if we own the lock
	if m.currentLockID == "" {
		m.logger.Error("No lock ID stored, cannot validate ownership", "manifest_lock_id", localManifest.LockedBy)
		return errors.New("lock ownership validation failed: no lock ID stored")
	}

	if localManifest.LockedBy != m.currentLockID {
		m.logger.Error("Lock ownership mismatch", "expected", m.currentLockID, "actual", localManifest.LockedBy)
		return errors.New("lock ownership validation failed")
	}

	m.logger.Info("Validated lock ownership", "lock_id", localManifest.LockedBy)

	// Unlock both manifests
	localManifest.Unlock()
	err = m.librarian.SaveLocalManifest(ctx, localManifest)
	if err != nil {
		m.logger.Error("Failed to unlock local manifest", "error", err)
		return err
	}
	m.logger.Info("Successfully unlocked local manifest")

	// Get remote manifest for unlock
	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get remote manifest for unlock", "error", err)
		// Log but don't fail - local is already unlocked
		return fmt.Errorf("local manifest unlocked but failed to unlock remote: %w", err)
	}

	if remoteManifest != nil {
		remoteManifest.Unlock()
		err = m.librarian.SaveRemoteManifest(ctx, remoteManifest)
		if err != nil {
			m.logger.Error("Failed to unlock remote manifest", "error", err)
			// Log but don't fail - proceed anyway
			return fmt.Errorf("local manifest unlocked but failed to unlock remote: %w", err)
		}
		m.logger.Info("Successfully unlocked remote manifest")
	}

	// Clear stored lock ID
	m.currentLockID = ""

	m.logger.Info("Successfully unlocked all manifests")
	return nil
}

// Helper function for Run method
func (m *MolfarService) getRemoteManifest(ctx context.Context) (*domain.Manifest, error) {
	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get remote manifest", "error", err)
		return nil, err
	}
	if remoteManifest == nil {
		return nil, errors.New("remote manifest cannot be nil")
	}
	m.logger.Info("Retrieved remote manifest", "ritual_version", remoteManifest.RitualVersion, "instance_version", remoteManifest.InstanceVersion)
	return remoteManifest, nil
}
