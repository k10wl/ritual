package services_test

import (
	"context"
	"encoding/json"
	"os"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RitualUpdater Tests
//
// TDD approach: Write tests first, then implement RitualUpdater to make them pass.
// RitualUpdater checks if ritual version matches between local and remote.
// If versions mismatch, downloads new binary, overwrites current, and restarts.

func setupRitualUpdaterServices(t *testing.T) (
	*adapters.FSRepository,
	*adapters.FSRepository,
	*services.LibrarianService,
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

	cleanup := func() {
		localStorage.Close()
		remoteStorage.Close()
	}

	return localStorage, remoteStorage, librarianService, cleanup
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
		localStorage, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
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

		// Create mock storage (won't be called since versions match)
		mockStorage := mocks.NewMockStorageRepository()

		// Create RitualUpdater
		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		require.NoError(t, err)

		// Execute update - should succeed without downloading
		err = updater.Run(ctx)
		assert.NoError(t, err)
	})

	t.Run("mismatched versions - downloads and updates binary", func(t *testing.T) {
		localStorage, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
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

		// Track if storage was called
		downloadCalled := false
		downloadKey := ""
		fakeBinary := []byte("fake binary content")

		mockStorage := &mocks.MockStorageRepository{
			GetFunc: func(ctx context.Context, key string) ([]byte, error) {
				downloadCalled = true
				downloadKey = key
				return fakeBinary, nil
			},
		}

		// Create RitualUpdater with exit disabled for testing
		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		require.NoError(t, err)

		// Execute update - will download but we can't test restart in unit test
		// The function will call os.Exit which we can't intercept easily
		// So we just verify the download was called with correct key
		err = updater.Run(ctx)

		// Since os.Exit is called, we won't reach here in real scenario
		// For testing purposes, we verify what we can
		assert.True(t, downloadCalled, "expected storage.Get to be called")
		assert.Equal(t, "ritual.exe", downloadKey, "expected download key to be ritual.exe")
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})

	t.Run("remote manifest fetch fails - returns error", func(t *testing.T) {
		localStorage, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Setup local manifest but no remote manifest
		localManifest := createRitualTestManifest("1.0.0", "1.20.1")
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		require.NoError(t, err)

		err = updater.Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get remote manifest")
	})

	t.Run("local manifest fetch fails - returns error", func(t *testing.T) {
		_, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Setup remote manifest but no local manifest
		remoteManifest := createRitualTestManifest("1.0.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		require.NoError(t, err)

		err = updater.Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get local manifest")
	})
}

func TestNewRitualUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		mockStorage := mocks.NewMockStorageRepository()

		_, err := services.NewRitualUpdater(nil, mockStorage)
		assert.Error(t, err)
		assert.ErrorIs(t, err, services.ErrRitualUpdaterLibrarianNil)
	})

	t.Run("nil storage returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		_, err := services.NewRitualUpdater(librarian, nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, services.ErrRitualUpdaterStorageNil)
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage)
		assert.NoError(t, err)
		assert.NotNil(t, updater)
	})
}
