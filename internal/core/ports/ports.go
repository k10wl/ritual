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

	// DeleteBatch removes multiple keys in a single operation
	DeleteBatch(ctx context.Context, keys []string) error

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
	Run(server *domain.ServerRuntime) error

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
	// CheckWorld validates world data integrity
	CheckWorld(local *domain.Manifest, remote *domain.Manifest) error

	// CheckLock validates lock mechanism compliance
	CheckLock(local *domain.Manifest, remote *domain.Manifest) error
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
	Run(server *domain.ServerRuntime) error
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

// RetentionService defines the interface for backup retention operations
// Retentions clean up old backups after manifest is updated
type RetentionService interface {
	// Apply removes old backups exceeding the retention limit
	// Uses manifest's Backups to identify valid backups
	Apply(ctx context.Context) error
}

// ConditionService defines the interface for pre-flight condition checks
// Conditions validate system prerequisites before updaters can run
type ConditionService interface {
	// Check validates the condition
	// Returns nil if condition passes, error with descriptive message if fails
	Check(ctx context.Context) error
}

// DirectoryScanner produces an xxhash map of all files in the worlds directory.
// Implementations determine scanning strategy (full walk vs mtime-filtered).
type DirectoryScanner interface {
	Scan(ctx context.Context) (map[string]string, error)
}

// SyncService handles bidirectional synchronization between local and remote states.
type SyncService interface {
	Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
	Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error)
}
