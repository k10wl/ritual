package services_test

import (
	"context"
	"encoding/json"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"testing"
	"time"

	"os"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RitualUpdater Tests
//
// TDD approach: Write tests first, then implement RitualUpdater to make them pass.
// RitualUpdater checks if ritual version matches between local and remote.
// Currently a placeholder - returns error if versions don't match.

func setupRitualUpdaterServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.LibrarianService,
	*services.ValidatorService,
	func(),
) {
	tempDir := t.TempDir()
	remoteTempDir := t.TempDir()

	// Create roots for safe operations
	tempRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)

	remoteRoot, err := os.OpenRoot(remoteTempDir)
	require.NoError(t, err)

	// Create local storage (FS)
	localStorage, err := adapters.NewFSRepository(tempRoot)
	require.NoError(t, err)

	// Create remote storage (FS for testing)
	remoteStorage, err := adapters.NewFSRepository(remoteRoot)
	require.NoError(t, err)

	// Create librarian service
	librarianService, err := services.NewLibrarianService(localStorage, remoteStorage)
	require.NoError(t, err)

	// Create validator service
	validatorService, err := services.NewValidatorService()
	require.NoError(t, err)

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return localStorage, remoteStorage, librarianService, validatorService, cleanup
}

func createRitualTestManifest(ritualVersion string, instanceVersion string) *domain.Manifest {
	return &domain.Manifest{
		RitualVersion:   ritualVersion,
		InstanceVersion: instanceVersion,
		StoredWorlds:    []domain.World{},
		UpdatedAt:       time.Now(),
	}
}

func TestRitualUpdater_Run(t *testing.T) {
	t.Run("matching versions - no update needed", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Setup remote manifest
		remoteManifest := createRitualTestManifest("1.0.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Setup local manifest with same ritual version
		localManifest := createRitualTestManifest("1.0.0", "1.20.1")
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		// Create RitualUpdater
		updater, err := services.NewRitualUpdater(librarian, validator)
		require.NoError(t, err)

		// Execute update - should succeed
		err = updater.Run(ctx)
		assert.NoError(t, err)
	})

	t.Run("mismatched versions - returns error", func(t *testing.T) {
		localStorage, remoteStorage, librarian, validator, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Setup remote manifest with newer version
		remoteManifest := createRitualTestManifest("2.0.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Setup local manifest with older ritual version
		localManifest := createRitualTestManifest("1.0.0", "1.20.1")
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		// Create RitualUpdater
		updater, err := services.NewRitualUpdater(librarian, validator)
		require.NoError(t, err)

		// Execute update - should return error (placeholder for self-update)
		err = updater.Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ritual version mismatch")
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		_, _, librarian, validator, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewRitualUpdater(librarian, validator)
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})
}

func TestNewRitualUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		_, _, _, validator, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		_, err := services.NewRitualUpdater(nil, validator)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "librarian")
	})

	t.Run("nil validator returns error", func(t *testing.T) {
		_, _, librarian, _, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		_, err := services.NewRitualUpdater(librarian, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validator")
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		_, _, librarian, validator, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		updater, err := services.NewRitualUpdater(librarian, validator)
		assert.NoError(t, err)
		assert.NotNil(t, updater)
	})
}
