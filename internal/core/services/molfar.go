package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
	"time"
)

// Molfar constants
const (
	InstanceZipKey = "instance.zip"
	TempPrefix     = config.TmpDir
	InstanceDir    = config.InstanceDir
)

// Molfar error constants
var (
	ErrLibrarianNil               = errors.New("librarian service cannot be nil")
	ErrValidatorNil               = errors.New("validator service cannot be nil")
	ErrArchiveNil                 = errors.New("archive service cannot be nil")
	ErrStorageNil                 = errors.New("storage repository cannot be nil")
	ErrServerRunnerNil            = errors.New("server runner cannot be nil")
	ErrMolfarInitializationFailed = errors.New("molfar initialization failed")
	ErrMolfarNil                  = errors.New("molfar service cannot be nil")
)

// MolfarService implements the main orchestration interface as a state machine
// Molfar coordinates the complete server lifecycle and manages all operations
type MolfarService struct {
	librarian     ports.LibrarianService
	validator     ports.ValidatorService
	archive       ports.ArchiveService
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
	serverRunner  ports.ServerRunner
	backupper     ports.BackupperService
	logger        *slog.Logger
	workRoot      *os.Root
	currentLockID string // Tracks the current lock ID for ownership validation (internal use only)
}

// NewMolfarService creates a new Molfar orchestration service
// Validates all dependencies are non-nil per NASA JPL defensive programming standards
func NewMolfarService(
	librarian ports.LibrarianService,
	validator ports.ValidatorService,
	archive ports.ArchiveService,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	serverRunner ports.ServerRunner,
	backupper ports.BackupperService,
	logger *slog.Logger,
	workRoot *os.Root,
) (*MolfarService, error) {
	if librarian == nil {
		return nil, ErrLibrarianNil
	}
	if validator == nil {
		return nil, ErrValidatorNil
	}
	if archive == nil {
		return nil, ErrArchiveNil
	}
	if localStorage == nil {
		return nil, ErrStorageNil
	}
	if remoteStorage == nil {
		return nil, ErrStorageNil
	}
	if serverRunner == nil {
		return nil, ErrServerRunnerNil
	}
	if backupper == nil {
		return nil, errors.New("backupper service cannot be nil")
	}
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if workRoot == nil {
		return nil, errors.New("workRoot cannot be nil")
	}

	molfar := &MolfarService{
		librarian:     librarian,
		validator:     validator,
		archive:       archive,
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
		serverRunner:  serverRunner,
		backupper:     backupper,
		logger:        logger,
		workRoot:      workRoot,
	}

	return molfar, nil
}

// Prepare initializes the environment and validates prerequisites
// Transitions to Running state
func (m *MolfarService) Prepare() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}
	if m.validator == nil {
		return ErrValidatorNil
	}

	m.logger.Info("Starting preparation phase", "workRoot", m.workRoot.Name())
	ctx := context.Background()

	remoteManifest, err := m.getRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localManifest, err := m.getOrInitializeLocalManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

	if err := m.validateManifests(localManifest, remoteManifest); err != nil {
		return err
	}

	m.logger.Info("Preparation phase completed successfully")
	return nil
}

// initializeLocalInstance sets up a new local instance when none exists
func (m *MolfarService) initializeLocalInstance(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}
	if m.archive == nil {
		return ErrArchiveNil
	}
	if m.localStorage == nil {
		return ErrStorageNil
	}
	if m.remoteStorage == nil {
		return ErrStorageNil
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}

	m.logger.Info("Initializing new local instance", "instance_version", remoteManifest.InstanceVersion)

	if err := m.downloadAndExtractInstance(ctx, InstanceDir); err != nil {
		return err
	}

	if err := m.downloadAndExtractWorlds(ctx, remoteManifest, InstanceDir); err != nil {
		return err
	}

	if err := m.librarian.SaveLocalManifest(ctx, remoteManifest); err != nil {
		m.logger.Error("Failed to save local manifest", "error", err)
		return err
	}

	m.logger.Info("Local instance initialization completed successfully")
	return nil
}

// updateLocalInstance replaces existing instance with updated version
func (m *MolfarService) updateLocalInstance(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}
	if m.archive == nil {
		return ErrArchiveNil
	}
	if m.localStorage == nil {
		return ErrStorageNil
	}
	if m.remoteStorage == nil {
		return ErrStorageNil
	}

	m.logger.Info("Updating local instance", "instance_version", remoteManifest.InstanceVersion)

	if err := m.downloadAndExtractInstance(ctx, InstanceDir); err != nil {
		return err
	}

	if err := m.librarian.SaveLocalManifest(ctx, remoteManifest); err != nil {
		m.logger.Error("Failed to save updated local manifest", "error", err)
		return err
	}

	m.logger.Info("Local instance update completed successfully")
	return nil
}

// updateLocalWorlds replaces existing worlds with updated versions
func (m *MolfarService) updateLocalWorlds(ctx context.Context, remoteManifest *domain.Manifest) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}
	if m.localStorage == nil {
		return ErrStorageNil
	}
	if m.archive == nil {
		return ErrArchiveNil
	}
	if m.remoteStorage == nil {
		return ErrStorageNil
	}

	m.logger.Info("Updating local worlds", "instance_version", remoteManifest.InstanceVersion)

	// Download and extract new worlds
	m.logger.Info("Downloading and extracting new worlds")
	if err := m.downloadAndExtractWorlds(ctx, remoteManifest, InstanceDir); err != nil {
		m.logger.Error("Failed to download and extract new worlds", "error", err)
		return fmt.Errorf("failed to download and extract new worlds: %w", err)
	}

	// Update local manifest with new world information
	m.logger.Info("Updating local manifest with new world information")
	if err := m.librarian.SaveLocalManifest(ctx, remoteManifest); err != nil {
		m.logger.Error("Failed to save updated local manifest", "error", err)
		return fmt.Errorf("failed to save updated local manifest: %w", err)
	}

	m.logger.Info("Local worlds update completed successfully")
	return nil
}

// downloadAndExtractWorlds downloads worlds from remote storage and extracts them
func (m *MolfarService) downloadAndExtractWorlds(ctx context.Context, remoteManifest *domain.Manifest, relInstanceDir string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}
	if relInstanceDir == "" {
		return errors.New("instance directory cannot be empty")
	}
	if m.localStorage == nil {
		return ErrStorageNil
	}
	if m.archive == nil {
		return ErrArchiveNil
	}
	if m.remoteStorage == nil {
		return ErrStorageNil
	}

	latestWorld := remoteManifest.GetLatestWorld()
	if latestWorld == nil {
		m.logger.Error("No worlds available in remote manifest")
		return fmt.Errorf("no worlds available in remote manifest")
	}

	sanitizedURI, err := m.sanitizeWorldURI(latestWorld.URI)
	if err != nil {
		return err
	}

	if err := m.downloadWorldArchive(ctx, sanitizedURI, latestWorld.CreatedAt); err != nil {
		return err
	}

	if err := m.extractWorldArchive(ctx, sanitizedURI, relInstanceDir); err != nil {
		return err
	}

	m.logger.Info("World download and extraction completed successfully")
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

	m.logger.Info("Starting execution phase", "server_address", server.Address, "server_memory", server.Memory, "server_ip", server.IP, "server_port", server.Port)
	ctx := context.Background()

	// Fetch remote manifest again before run
	remoteManifest, err := m.getRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localManifest, err := m.validateAndRetrieveManifest(ctx)
	if err != nil {
		return err
	}

	if err := m.acquireManifestLocks(ctx, localManifest, remoteManifest); err != nil {
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
	m.logger.Info("Retrieved hostname", "hostname", hostname)

	lockID := fmt.Sprintf("%s__%d", hostname, time.Now().Unix())
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

	m.logger.Info("Starting server execution", "server_address", server.Address, "bat_path", server.BatPath)
	err := m.serverRunner.Run(server)
	if err != nil {
		m.logger.Error("Server execution failed", "error", err)
		return err
	}
	m.logger.Info("Server execution completed successfully")

	return nil
}

// Exit gracefully shuts down the server and cleans up resources
// Transitions to Exiting state
func (m *MolfarService) Exit() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.backupper == nil {
		return errors.New("backupper service cannot be nil")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.logger.Info("Starting exit phase")
	ctx := context.Background()

	// Run backup process
	archiveName, err := m.backupper.Run()
	if err != nil {
		m.logger.Error("Backup execution failed", "error", err)
		return err
	}
	m.logger.Info("Backup completed successfully", "archive_name", archiveName)

	// Update manifests with new archive name
	if err := m.updateManifestsWithArchive(ctx, archiveName); err != nil {
		m.logger.Error("Failed to update manifests with archive", "error", err)
		return err
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
func (m *MolfarService) updateManifestsWithArchive(ctx context.Context, archiveName string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if archiveName == "" {
		return errors.New("archive name cannot be empty")
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.logger.Info("Updating manifests with new archive", "archive_name", archiveName)

	// Get local manifest
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get local manifest for archive update", "error", err)
		return err
	}

	// Create new world entry with archive name
	world, err := domain.NewWorld(archiveName)
	if err != nil {
		m.logger.Error("Failed to create world entry", "archive_name", archiveName, "error", err)
		return err
	}

	// Add world to manifest
	localManifest.AddWorld(*world)

	// Save updated local manifest
	if err := m.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		m.logger.Error("Failed to save local manifest with archive", "error", err)
		return err
	}

	// Save updated remote manifest
	if err := m.librarian.SaveRemoteManifest(ctx, localManifest); err != nil {
		m.logger.Error("Failed to save remote manifest with archive", "error", err)
		return err
	}

	m.logger.Info("Successfully updated manifests with archive", "archive_name", archiveName)
	return nil
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

// Helper functions for Prepare method
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

func (m *MolfarService) getOrInitializeLocalManifest(ctx context.Context, remoteManifest *domain.Manifest) (*domain.Manifest, error) {
	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "key not found") {
			m.logger.Info("Local manifest not found, initializing new instance")
			if initErr := m.initializeLocalInstance(ctx, remoteManifest); initErr != nil {
				m.logger.Error("Failed to initialize local instance", "error", initErr)
				return nil, initErr
			}
			localManifest, err = m.librarian.GetLocalManifest(ctx)
			if err != nil {
				m.logger.Error("Failed to get local manifest after initialization", "error", err)
				return nil, err
			}
			m.logger.Info("Successfully initialized local instance")
		} else {
			m.logger.Error("Failed to get local manifest", "error", err)
			return nil, err
		}
	} else {
		m.logger.Info("Retrieved local manifest", "ritual_version", localManifest.RitualVersion, "instance_version", localManifest.InstanceVersion)
	}
	if localManifest == nil {
		return nil, errors.New("local manifest cannot be nil")
	}
	return localManifest, nil
}

func (m *MolfarService) validateManifests(localManifest, remoteManifest *domain.Manifest) error {
	if err := m.validator.CheckLock(localManifest, remoteManifest); err != nil {
		m.logger.Error("Lock validation failed", "error", err)
		return err
	}
	m.logger.Info("Lock validation passed")

	if err := m.validator.CheckInstance(localManifest, remoteManifest); err != nil {
		if err.Error() == "outdated instance" {
			m.logger.Info("Instance is outdated, updating local instance")
			if updateErr := m.updateLocalInstance(context.Background(), remoteManifest); updateErr != nil {
				m.logger.Error("Failed to update local instance", "error", updateErr)
				return updateErr
			}
			m.logger.Info("Successfully updated local instance")
		} else {
			m.logger.Error("Instance validation failed", "error", err)
			return err
		}
	} else {
		m.logger.Info("Instance validation passed")
	}

	if err := m.validator.CheckWorld(localManifest, remoteManifest); err != nil {
		if err.Error() == "outdated world" {
			m.logger.Info("World is outdated, updating local worlds")
			if updateErr := m.updateLocalWorlds(context.Background(), remoteManifest); updateErr != nil {
				m.logger.Error("Failed to update local worlds", "error", updateErr)
				return updateErr
			}
			m.logger.Info("Successfully updated local worlds")
		} else {
			m.logger.Error("World validation failed", "error", err)
			return err
		}
	} else {
		m.logger.Info("World validation passed")
	}

	return nil
}

// Helper functions for instance operations
func (m *MolfarService) downloadAndExtractInstance(ctx context.Context, relInstanceDir string) error {
	m.logger.Info("Downloading instance from remote storage", "key", InstanceZipKey)
	instanceZipData, err := m.remoteStorage.Get(ctx, InstanceZipKey)
	if err != nil {
		m.logger.Error("Failed to get instance from remote storage", "key", InstanceZipKey, "error", err)
		return fmt.Errorf("failed to get %s from remote storage: %w", InstanceZipKey, err)
	}

	tempKey := filepath.Join(TempPrefix, InstanceZipKey)
	m.logger.Info("Storing instance in temporary storage", "temp_key", tempKey)
	err = m.localStorage.Put(ctx, tempKey, instanceZipData)
	if err != nil {
		m.logger.Error("Failed to store instance in temp storage", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to store %s in temp storage: %w", InstanceZipKey, err)
	}

	m.logger.Info("Extracting instance archive", "source", tempKey, "destination", relInstanceDir)
	err = m.archive.Unarchive(ctx, tempKey, relInstanceDir)
	if err != nil {
		m.logger.Error("Failed to extract instance archive", "source", tempKey, "destination", relInstanceDir, "error", err)
		return err
	}

	m.logger.Info("Cleaning up temporary files", "temp_key", tempKey)
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		m.logger.Error("Failed to cleanup temp files", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to cleanup temp %s: %w", InstanceZipKey, err)
	}

	return nil
}

// Helper functions for world operations
func (m *MolfarService) sanitizeWorldURI(uri string) (string, error) {
	sanitizedURI := filepath.ToSlash(filepath.Clean(uri))
	if !strings.HasPrefix(sanitizedURI, config.RemoteBackups+"/") {
		return "", fmt.Errorf("invalid world URI: %s", sanitizedURI)
	}
	return sanitizedURI, nil
}

func (m *MolfarService) downloadWorldArchive(ctx context.Context, sanitizedURI string, createdAt time.Time) error {
	m.logger.Info("Downloading worlds from remote storage", "world_uri", sanitizedURI, "created_at", createdAt)
	worldZipData, err := m.remoteStorage.Get(ctx, sanitizedURI)
	if err != nil {
		m.logger.Error("Failed to get worlds from remote storage", "world_uri", sanitizedURI, "error", err)
		return fmt.Errorf("failed to get %s from remote storage: %w", sanitizedURI, err)
	}

	tempKey := filepath.Join(TempPrefix, sanitizedURI)
	m.logger.Info("Storing worlds in temporary storage", "temp_key", tempKey)
	err = m.localStorage.Put(ctx, tempKey, worldZipData)
	if err != nil {
		m.logger.Error("Failed to store worlds in temp storage", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to store %s in temp storage: %w", sanitizedURI, err)
	}

	return nil
}

func (m *MolfarService) extractWorldArchive(ctx context.Context, sanitizedURI, relInstanceDir string) error {
	tempKey := filepath.Join(TempPrefix, sanitizedURI)
	m.logger.Info("Extracting worlds archive", "source", tempKey, "destination", relInstanceDir)
	err := m.archive.Unarchive(ctx, tempKey, relInstanceDir)
	if err != nil {
		m.logger.Error("Failed to extract worlds archive", "source", tempKey, "destination", relInstanceDir, "error", err)
		return fmt.Errorf("failed to extract worlds: %w", err)
	}

	m.logger.Info("Cleaning up temporary world files", "temp_key", tempKey)
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		m.logger.Error("Failed to cleanup temp world files", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to cleanup %s: %w", sanitizedURI, err)
	}

	return nil
}
