package ports

import (
	"context"
	"ritual/internal/core/domain"
)

// StorageRepository defines the interface for storage operations
// This abstraction allows switching between local filesystem and cloud storage
type StorageRepository interface {
	// Get retrieves data by key
	Get(ctx context.Context, key string) ([]byte, error)

	// Put stores data with the given key
	Put(ctx context.Context, key string, data []byte) error

	// Delete removes data by key
	Delete(ctx context.Context, key string) error

	// List returns all keys with the given prefix
	List(ctx context.Context, prefix string) ([]string, error)

	// Copy copies data from source key to destination key
	Copy(ctx context.Context, sourceKey string, destKey string) error
}

// MolfarService defines the main orchestration interface
// Molfar coordinates the complete server lifecycle and manages all operations
type MolfarService interface {
	// Prepare initializes the environment and validates prerequisites
	Prepare() error

	// Run executes the main server orchestration process
	Run(server *domain.Server) error

	// Exit gracefully shuts down the server and cleans up resources
	Exit() error
}

// LibrarianService defines the manifest management interface
// Librarian handles synchronization between local and remote manifests
type LibrarianService interface {
	// GetLocalManifest retrieves the local manifest
	GetLocalManifest(ctx context.Context) (*domain.Manifest, error)

	// GetRemoteManifest retrieves the remote manifest
	GetRemoteManifest(ctx context.Context) (*domain.Manifest, error)

	// SaveLocalManifest stores the manifest locally
	SaveLocalManifest(ctx context.Context, manifest *domain.Manifest) error

	// SaveRemoteManifest stores the manifest remotely
	SaveRemoteManifest(ctx context.Context, manifest *domain.Manifest) error
}

// ValidatorService defines the validation interface
// Validator ensures instance integrity and validates data consistency
type ValidatorService interface {
	// CheckInstance validates manifest structure and content
	CheckInstance(local *domain.Manifest, remote *domain.Manifest) error

	// CheckWorld validates world data integrity
	CheckWorld(local *domain.Manifest, remote *domain.Manifest) error

	// CheckLock validates lock mechanism compliance
	CheckLock(local *domain.Manifest, remote *domain.Manifest) error
}

// ArchiveService defines the archive management interface
// ArchiveService handles compression and extraction of data archives
type ArchiveService interface {
	// Archive compresses source to destination
	Archive(ctx context.Context, source string, destination string) error

	// Unarchive extracts archive to destination
	Unarchive(ctx context.Context, archive string, destination string) error
}

// CommandExecutor defines the command execution interface
// CommandExecutor abstracts command execution for testability
type CommandExecutor interface {
	// Execute runs a command with the given arguments and working directory
	Execute(command string, args []string, workingDir string) error
}

// ServerRunner defines the server execution interface
// ServerRunner handles the execution of Minecraft server processes
type ServerRunner interface {
	// Run executes the server process with the given server configuration
	Run(server *domain.Server) error
}

// BackupperService defines the backup orchestration interface
// BackupperService handles backup creation and storage
type BackupperService interface {
	// Run executes the backup orchestration process
	// Returns the archive name/URI that was created for manifest updates
	Run(ctx context.Context) (string, error)
}

// UpdaterService defines the interface for update operations
// Updaters handle downloading and extracting content from remote storage
type UpdaterService interface {
	// Run executes the update process
	// Returns nil if no update needed or update succeeded, error if update failed
	Run(ctx context.Context) error
}
