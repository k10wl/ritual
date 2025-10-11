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
}

// MolfarService defines the main orchestration interface
// Molfar coordinates the complete server lifecycle and manages all operations
type MolfarService interface {
	// Prepare initializes the environment and validates prerequisites
	Prepare() error

	// Run executes the main server orchestration process
	Run() error

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
