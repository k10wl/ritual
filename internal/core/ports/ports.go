package ports

import (
	"context"
	"os/exec"
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

// ValidatorService defines the validation interface
// Validator ensures instance integrity and validates data consistency
type ValidatorService interface {
	// CheckLock validates lock mechanism compliance
	CheckLock(local *domain.Manifest, remote *domain.Manifest) error
}

// CmdBuilder lazily creates the *exec.Cmd for the server process.
// Build receives the context so exec.CommandContext can wire cancellation.
type CmdBuilder interface {
	Build(ctx context.Context) (*exec.Cmd, error)
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
