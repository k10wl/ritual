package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
	"time"
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

	ctx := context.Background()

	remoteManifest, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localManifest, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "key not found") {
			if initErr := m.initializeLocalInstance(ctx, remoteManifest); initErr != nil {
				return initErr
			}
			// Re-fetch local manifest after initialization
			localManifest, err = m.librarian.GetLocalManifest(ctx)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if err := m.validator.CheckLock(localManifest, remoteManifest); err != nil {
		return err
	}

	if err := m.validator.CheckInstance(localManifest, remoteManifest); err != nil {
		if errors.Is(err, ErrOutdatedInstance) {
			if updateErr := m.updateLocalInstance(ctx, remoteManifest); updateErr != nil {
				return updateErr
			}
		} else {
			return err
		}
	}

	if err := m.validator.CheckWorld(localManifest, remoteManifest); err != nil {
		if errors.Is(err, ErrOutdatedWorld) {
			if updateErr := m.updateLocalWorlds(ctx, remoteManifest); updateErr != nil {
				return updateErr
			}
		} else {
			return err
		}
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	remoteManifest.LockedBy = fmt.Sprintf("%s__%d", hostname, time.Now().Unix())
	err = m.librarian.SaveRemoteManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

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

	instancePath := filepath.Join(m.workdir, InstanceDir)

	instanceZipData, err := m.remoteStorage.Get(ctx, InstanceZipKey)
	if err != nil {
		return fmt.Errorf("failed to get %s from remote storage: %w", InstanceZipKey, err)
	}

	tempKey := filepath.Join(TempPrefix, InstanceZipKey)
	err = m.localStorage.Put(ctx, tempKey, instanceZipData)
	if err != nil {
		return fmt.Errorf("failed to store %s in temp storage: %w", InstanceZipKey, err)
	}

	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		return err
	}

	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		return fmt.Errorf("failed to cleanup temp %s: %w", InstanceZipKey, err)
	}

	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

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

	instancePath := filepath.Join(m.workdir, InstanceDir)

	instanceZipData, err := m.remoteStorage.Get(ctx, InstanceZipKey)
	if err != nil {
		return fmt.Errorf("failed to get %s from remote storage: %w", InstanceZipKey, err)
	}

	tempKey := filepath.Join(TempPrefix, InstanceZipKey)
	err = m.localStorage.Put(ctx, tempKey, instanceZipData)
	if err != nil {
		return fmt.Errorf("failed to store %s in temp storage: %w", InstanceZipKey, err)
	}

	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		return err
	}

	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		return fmt.Errorf("failed to cleanup temp %s: %w", InstanceZipKey, err)
	}

	err = m.librarian.SaveLocalManifest(ctx, remoteManifest)
	if err != nil {
		return err
	}

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

	instancePath := filepath.Join(m.workdir, InstanceDir)
	backupPath := filepath.Join(m.workdir, BackupDir, PreUpdateDir)

	// Copy current worlds to backup
	err := m.copyWorldsToBackup(instancePath, backupPath)
	if err != nil {
		return fmt.Errorf("failed to backup current worlds: %w", err)
	}

	// Download and extract new worlds
	err = m.downloadAndExtractWorlds(ctx, remoteManifest, instancePath)
	if err != nil {
		return fmt.Errorf("failed to download and extract new worlds: %w", err)
	}

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

	ctx := context.Background()
	worldDirs := []string{"world", "world_nether", "world_the_end"}

	for _, worldDir := range worldDirs {
		sourceKey := filepath.Join("instance", worldDir)
		destKey := filepath.Join("backup", worldDir)

		err := m.localStorage.Copy(ctx, sourceKey, destKey)
		if err != nil {
			// Skip if source doesn't exist
			if strings.Contains(err.Error(), "source key not found") {
				continue
			}
			return fmt.Errorf("failed to copy %s to backup: %w", worldDir, err)
		}
	}

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
		return fmt.Errorf("no worlds available in remote manifest")
	}

	// Download worlds from remote storage
	worldZipData, err := m.remoteStorage.Get(ctx, latestWorld.URI)
	if err != nil {
		return fmt.Errorf("failed to get %s from remote storage: %w", latestWorld.URI, err)
	}

	// Store in temp storage
	tempKey := filepath.Join(TempPrefix, latestWorld.URI)
	err = m.localStorage.Put(ctx, tempKey, worldZipData)
	if err != nil {
		return fmt.Errorf("failed to store %s in temp storage: %w", latestWorld.URI, err)
	}

	// Extract worlds to instance directory
	// Get the actual file path for unarchiving
	tempFilePath := filepath.Join(m.workdir, tempKey)
	err = m.archive.Unarchive(ctx, tempFilePath, instancePath)
	if err != nil {
		return fmt.Errorf("failed to extract worlds: %w", err)
	}

	// Cleanup temporary storage
	err = m.localStorage.Delete(ctx, tempKey)
	if err != nil {
		return fmt.Errorf("failed to cleanup %s: %w", latestWorld.URI, err)
	}

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

	return nil
}
