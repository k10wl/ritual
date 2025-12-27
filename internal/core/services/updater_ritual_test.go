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
		Backups:    []domain.World{},
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

		// Create RitualUpdater with binary version matching remote
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
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

		// Create RitualUpdater with older binary version
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		// Execute update - will download but we can't test restart in unit test
		// The function will call os.Exit which we can't intercept easily
		// So we just verify the download was called with correct key
		err = updater.Run(ctx)

		// Since os.Exit is called, we won't reach here in real scenario
		// For testing purposes, we verify what we can
		assert.True(t, downloadCalled, "expected storage.Get to be called")
		// Key is always ritual.exe by convention
		assert.Equal(t, "ritual.exe", downloadKey, "expected download key to be ritual.exe")
	})

	t.Run("nil context - returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		err = updater.Run(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})

	t.Run("remote manifest fetch fails - returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// No remote manifest setup - will fail to fetch

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		err = updater.Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get remote manifest")
	})

	t.Run("first run - creates local manifest from remote", func(t *testing.T) {
		localStorage, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Setup remote manifest with newer version (but no local manifest)
		remoteManifest := createRitualTestManifest("2.0.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Mock storage returns fake binary
		mockStorage := &mocks.MockStorageRepository{
			GetFunc: func(ctx context.Context, key string) ([]byte, error) {
				return []byte("fake binary"), nil
			},
		}

		// Binary version is older than remote, so update will be triggered
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		// Run will create local manifest from remote since it doesn't exist
		_ = updater.Run(ctx)

		// Verify local manifest was created
		data, err := localStorage.Get(ctx, "manifest.json")
		require.NoError(t, err)

		var savedManifest domain.Manifest
		err = json.Unmarshal(data, &savedManifest)
		require.NoError(t, err)
		assert.Equal(t, "2.0.0", savedManifest.RitualVersion)
	})
}

func TestNewRitualUpdater(t *testing.T) {
	t.Run("nil librarian returns error", func(t *testing.T) {
		mockStorage := mocks.NewMockStorageRepository()

		_, err := services.NewRitualUpdater(nil, mockStorage, "1.0.0")
		assert.Error(t, err)
		assert.ErrorIs(t, err, services.ErrRitualUpdaterLibrarianNil)
	})

	t.Run("nil storage returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		_, err := services.NewRitualUpdater(librarian, nil, "1.0.0")
		assert.Error(t, err)
		assert.ErrorIs(t, err, services.ErrRitualUpdaterStorageNil)
	})

	t.Run("empty version returns error", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		mockStorage := mocks.NewMockStorageRepository()

		_, err := services.NewRitualUpdater(librarian, mockStorage, "")
		assert.Error(t, err)
		assert.ErrorIs(t, err, services.ErrRitualUpdaterVersionEmpty)
	})

	t.Run("valid dependencies returns updater", func(t *testing.T) {
		_, _, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		mockStorage := mocks.NewMockStorageRepository()

		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		assert.NoError(t, err)
		assert.NotNil(t, updater)
	})
}

func TestIsVersionOlder(t *testing.T) {
	tests := []struct {
		name     string
		local    string
		remote   string
		expected bool
	}{
		// Equal versions - no update
		{"equal versions", "1.0.0", "1.0.0", false},
		{"equal two part", "1.0", "1.0", false},
		{"equal single part", "1", "1", false},

		// Local older - should update
		{"major older", "1.0.0", "2.0.0", true},
		{"minor older", "1.1.0", "1.2.0", true},
		{"patch older", "1.0.1", "1.0.2", true},
		{"all parts older", "1.2.3", "2.3.4", true},
		{"minor older with same major", "2.1.0", "2.5.0", true},
		{"patch older with same major minor", "3.2.1", "3.2.9", true},

		// Local newer - no update
		{"major newer", "2.0.0", "1.0.0", false},
		{"minor newer", "1.2.0", "1.1.0", false},
		{"patch newer", "1.0.2", "1.0.1", false},
		{"all parts newer", "2.3.4", "1.2.3", false},

		// Different length versions
		{"shorter local is older", "1.0", "1.0.1", true},
		{"shorter remote not older", "1.0.1", "1.0", false},
		{"two vs three parts equal prefix", "1.2", "1.2.0", true},
		{"single vs triple", "1", "1.0.1", true},

		// Edge cases
		{"zero versions", "0.0.0", "0.0.1", true},
		{"large numbers", "10.20.30", "10.20.31", true},
		{"large major", "99.0.0", "100.0.0", true},

		// Real world scenarios
		{"bugfix update", "0.9.0", "0.9.1", true},
		{"minor update", "0.9.0", "0.10.0", true},
		{"major update", "0.9.0", "1.0.0", true},
		{"downgrade blocked", "1.0.0", "0.9.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := services.IsVersionOlder(tt.local, tt.remote)
			assert.Equal(t, tt.expected, result, "IsVersionOlder(%q, %q) = %v, want %v",
				tt.local, tt.remote, result, tt.expected)
		})
	}
}

func TestRitualUpdater_VersionComparison(t *testing.T) {
	t.Run("binary newer than remote - no update", func(t *testing.T) {
		_, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Remote has older version
		remoteManifest := createRitualTestManifest("1.0.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		downloadCalled := false
		mockStorage := &mocks.MockStorageRepository{
			GetFunc: func(ctx context.Context, key string) ([]byte, error) {
				downloadCalled = true
				return []byte("binary"), nil
			},
		}

		// Binary version is newer than remote
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "2.0.0")
		require.NoError(t, err)

		err = updater.Run(ctx)
		assert.NoError(t, err)
		assert.False(t, downloadCalled, "should not download when binary is newer")
	})

	t.Run("minor version update triggers download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Remote has newer minor version
		remoteManifest := createRitualTestManifest("1.1.0", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Local manifest for update
		localManifest := createRitualTestManifest("1.0.0", "1.20.1")
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		downloadCalled := false
		mockStorage := &mocks.MockStorageRepository{
			GetFunc: func(ctx context.Context, key string) ([]byte, error) {
				downloadCalled = true
				return []byte("binary"), nil
			},
		}

		// Binary version is older than remote
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		// Will fail at file write but we just want to verify download was attempted
		_ = updater.Run(ctx)
		assert.True(t, downloadCalled, "should download when binary minor version is older")
	})

	t.Run("patch version update triggers download", func(t *testing.T) {
		localStorage, remoteStorage, librarian, cleanup := setupRitualUpdaterServices(t)
		defer cleanup()

		ctx := context.Background()

		// Remote has newer patch version
		remoteManifest := createRitualTestManifest("1.0.1", "1.20.1")
		remoteManifestData, err := json.Marshal(remoteManifest)
		require.NoError(t, err)
		err = remoteStorage.Put(ctx, "manifest.json", remoteManifestData)
		require.NoError(t, err)

		// Local manifest for update
		localManifest := createRitualTestManifest("1.0.0", "1.20.1")
		localManifestData, err := json.Marshal(localManifest)
		require.NoError(t, err)
		err = localStorage.Put(ctx, "manifest.json", localManifestData)
		require.NoError(t, err)

		downloadCalled := false
		mockStorage := &mocks.MockStorageRepository{
			GetFunc: func(ctx context.Context, key string) ([]byte, error) {
				downloadCalled = true
				return []byte("binary"), nil
			},
		}

		// Binary version is older than remote
		updater, err := services.NewRitualUpdater(librarian, mockStorage, "1.0.0")
		require.NoError(t, err)

		_ = updater.Run(ctx)
		assert.True(t, downloadCalled, "should download when binary patch version is older")
	})
}
