package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
)

// Molfar constants
const (
	InstanceZipKey = "instance.zip"
	TempPrefix     = "temp"
	InstanceDir    = "instance"
	BackupDir      = "backups"
	PreUpdateDir   = "pre-update"
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
	logger        *slog.Logger
	workdir       string
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
	logger *slog.Logger,
	workdir string,
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
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if workdir == "" {
		return nil, errors.New("workdir cannot be empty")
	}

	molfar := &MolfarService{
		librarian:     librarian,
		validator:     validator,
		archive:       archive,
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
		serverRunner:  serverRunner,
		logger:        logger,
		workdir:       workdir,
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

	m.logger.Info("Starting preparation phase", "workdir", m.workdir)
	ctx := context.Background()

	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		m.logger.Error("Failed to get remote manifest", "error", err)
		return err
	}
	m.logger.Info("Retrieved remote manifest", "ritual_version", remoteManifest.RitualVersion, "instance_version", remoteManifest.InstanceVersion)

	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "key not found") {
			m.logger.Info("Local manifest not found, initializing new instance")
			if initErr := m.initializeLocalInstance(ctx, remoteManifest); initErr != nil {
				m.logger.Error("Failed to initialize local instance", "error", initErr)
				return initErr
			}
			// Re-fetch local manifest after initialization
			localManifest, err = m.librarian.GetLocalManifest(ctx)
			if err != nil {
				m.logger.Error("Failed to get local manifest after initialization", "error", err)
				return err
			}
			m.logger.Info("Successfully initialized local instance")
		} else {
			m.logger.Error("Failed to get local manifest", "error", err)
			return err
		}
	} else {
		m.logger.Info("Retrieved local manifest", "ritual_version", localManifest.RitualVersion, "instance_version", localManifest.InstanceVersion)
	}

	if err := m.validator.CheckLock(localManifest, remoteManifest); err != nil {
		m.logger.Error("Lock validation failed", "error", err)
		return err
	}
	m.logger.Info("Lock validation passed")

	if err := m.validator.CheckInstance(localManifest, remoteManifest); err != nil {
		if errors.Is(err, ErrOutdatedInstance) {
			m.logger.Info("Instance is outdated, updating local instance")
			if updateErr := m.updateLocalInstance(ctx, remoteManifest); updateErr != nil {
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
		if errors.Is(err, ErrOutdatedWorld) {
			m.logger.Info("World is outdated, updating local worlds")
			if updateErr := m.updateLocalWorlds(ctx, remoteManifest); updateErr != nil {
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
	instancePath := filepath.Join(m.workdir, InstanceDir)

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

	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	m.logger.Info("Extracting instance archive", "source", tempFilePath, "destination", instancePath)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		m.logger.Error("Failed to extract instance archive", "source", tempFilePath, "destination", instancePath, "error", err)
		return err
	}

	m.logger.Info("Cleaning up temporary files", "temp_key", tempKey)
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		m.logger.Error("Failed to cleanup temp files", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to cleanup temp %s: %w", InstanceZipKey, err)
	}

	// Always download latest world during initialization
	m.logger.Info("Downloading latest world during initialization")
	err = m.downloadAndExtractWorlds(ctx, remoteManifest, instancePath)
	if err != nil {
		m.logger.Error("Failed to download worlds during initialization", "error", err)
		return err
	}
	m.logger.Info("Successfully downloaded worlds during initialization")

	m.logger.Info("Saving local manifest", "instance_version", remoteManifest.InstanceVersion)
	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
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
	instancePath := filepath.Join(m.workdir, InstanceDir)

	m.logger.Info("Downloading updated instance from remote storage", "key", InstanceZipKey)
	instanceZipData, err := m.remoteStorage.Get(ctx, InstanceZipKey)
	if err != nil {
		m.logger.Error("Failed to get updated instance from remote storage", "key", InstanceZipKey, "error", err)
		return fmt.Errorf("failed to get %s from remote storage: %w", InstanceZipKey, err)
	}

	tempKey := filepath.Join(TempPrefix, InstanceZipKey)
	m.logger.Info("Storing updated instance in temporary storage", "temp_key", tempKey)
	err = m.localStorage.Put(ctx, tempKey, instanceZipData)
	if err != nil {
		m.logger.Error("Failed to store updated instance in temp storage", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to store %s in temp storage: %w", InstanceZipKey, err)
	}

	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	m.logger.Info("Extracting updated instance archive", "source", tempFilePath, "destination", instancePath)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		m.logger.Error("Failed to extract updated instance archive", "source", tempFilePath, "destination", instancePath, "error", err)
		return err
	}

	m.logger.Info("Cleaning up temporary files", "temp_key", tempKey)
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		m.logger.Error("Failed to cleanup temp files", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to cleanup temp %s: %w", InstanceZipKey, err)
	}

	m.logger.Info("Saving updated local manifest", "instance_version", remoteManifest.InstanceVersion)
	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
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
	instancePath := filepath.Join(m.workdir, InstanceDir)
	backupPath := filepath.Join(m.workdir, BackupDir, PreUpdateDir)

	// Copy current worlds to backup
	m.logger.Info("Backing up current worlds", "source", instancePath, "backup", backupPath)
	err := m.copyWorldsToBackup(instancePath, backupPath)
	if err != nil {
		m.logger.Error("Failed to backup current worlds", "error", err)
		return fmt.Errorf("failed to backup current worlds: %w", err)
	}

	// Download and extract new worlds
	m.logger.Info("Downloading and extracting new worlds")
	err = m.downloadAndExtractWorlds(ctx, remoteManifest, instancePath)
	if err != nil {
		m.logger.Error("Failed to download and extract new worlds", "error", err)
		return fmt.Errorf("failed to download and extract new worlds: %w", err)
	}

	// Update local manifest with new world information
	m.logger.Info("Updating local manifest with new world information")
	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
		m.logger.Error("Failed to save updated local manifest", "error", err)
		return fmt.Errorf("failed to save updated local manifest: %w", err)
	}

	m.logger.Info("Local worlds update completed successfully")
	return nil
}

// copyWorldsToBackup copies existing world directories to backup location
func (m *MolfarService) copyWorldsToBackup(instancePath, backupPath string) error {
	if instancePath == "" {
		return errors.New("instance path cannot be empty")
	}
	if backupPath == "" {
		return errors.New("backup path cannot be empty")
	}
	if m.localStorage == nil {
		return ErrStorageNil
	}

	m.logger.Info("Starting world backup process", "source", instancePath, "backup", backupPath)
	ctx := context.Background()
	worldDirs := []string{"world", "world_nether", "world_the_end"}

	for _, worldDir := range worldDirs {
		sourceKey := filepath.Join("instance", worldDir)
		destKey := filepath.Join("backup", worldDir)

		m.logger.Debug("Copying world directory", "world", worldDir, "source", sourceKey, "dest", destKey)
		err := m.localStorage.Copy(ctx, sourceKey, destKey)
		if err != nil {
			// Skip if source doesn't exist
			if strings.Contains(err.Error(), "source key not found") {
				m.logger.Debug("World directory not found, skipping", "world", worldDir)
				continue
			}
			m.logger.Error("Failed to copy world directory", "world", worldDir, "error", err)
			return fmt.Errorf("failed to copy %s to backup: %w", worldDir, err)
		}
		m.logger.Debug("Successfully backed up world directory", "world", worldDir)
	}

	m.logger.Info("World backup process completed successfully")
	return nil
}

// downloadAndExtractWorlds downloads worlds from remote storage and extracts them
func (m *MolfarService) downloadAndExtractWorlds(ctx context.Context, remoteManifest *domain.Manifest, instancePath string) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if remoteManifest == nil {
		return errors.New("remote manifest cannot be nil")
	}
	if instancePath == "" {
		return errors.New("instance path cannot be empty")
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

	// Get the latest world URI from remote manifest
	latestWorld := remoteManifest.GetLatestWorld()
	if latestWorld == nil {
		m.logger.Error("No worlds available in remote manifest")
		return fmt.Errorf("no worlds available in remote manifest")
	}

	m.logger.Info("Downloading worlds from remote storage", "world_uri", latestWorld.URI, "created_at", latestWorld.CreatedAt)
	// Download worlds from remote storage
	worldZipData, err := m.remoteStorage.Get(ctx, latestWorld.URI)
	if err != nil {
		m.logger.Error("Failed to get worlds from remote storage", "world_uri", latestWorld.URI, "error", err)
		return fmt.Errorf("failed to get %s from remote storage: %w", latestWorld.URI, err)
	}

	// Store in temp storage
	tempKey := filepath.Join(TempPrefix, latestWorld.URI)
	m.logger.Info("Storing worlds in temporary storage", "temp_key", tempKey)
	err = m.localStorage.Put(ctx, tempKey, worldZipData)
	if err != nil {
		m.logger.Error("Failed to store worlds in temp storage", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to store %s in temp storage: %w", latestWorld.URI, err)
	}

	// Extract worlds to instance directory
	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	m.logger.Info("Extracting worlds archive", "source", tempFilePath, "destination", instancePath)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		m.logger.Error("Failed to extract worlds archive", "source", tempFilePath, "destination", instancePath, "error", err)
		return fmt.Errorf("failed to extract worlds: %w", err)
	}

	// Cleanup temporary storage
	m.logger.Info("Cleaning up temporary world files", "temp_key", tempKey)
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		m.logger.Error("Failed to cleanup temp world files", "temp_key", tempKey, "error", err)
		return fmt.Errorf("failed to cleanup %s: %w", latestWorld.URI, err)
	}

	m.logger.Info("World download and extraction completed successfully")
	return nil
}

// Run executes the main server orchestration process
// Already in Running state, coordinates server execution
func (m *MolfarService) Run() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.serverRunner == nil {
		return ErrServerRunnerNil
	}

	m.logger.Info("Starting execution phase")
	// TODO: Implement server execution logic
	m.logger.Info("Execution phase completed")
	return nil
}

// Exit gracefully shuts down the server and cleans up resources
// Transitions to Exiting state
func (m *MolfarService) Exit() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.logger.Info("Starting exit phase")
	// TODO: Implement cleanup and lock release logic
	m.logger.Info("Exit phase completed")
	return nil
}
