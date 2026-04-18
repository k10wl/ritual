package services_test

import (
	"context"
	"errors"
	"maps"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mocks "ritual/internal/core/ports/mocks"
)

// TestSyncService_Download_NoMutation verifies the input local SyncState is
// not mutated by Download, even when an error surfaces from the engine.
func TestSyncService_Download_NoMutation(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}}
	remote := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h2", Size: 11},
		"b": {Hash: "h3", Size: 12},
	}}
	originalLocal := maps.Clone(local.XXHashMap)

	src := &mocks.MockStorageRepository{
		Label: "mock::remote",
		GetFunc: func(_ context.Context, _ string) ([]byte, error) {
			return nil, errors.New("forced fail")
		},
	}
	dst := &mocks.MockStorageRepository{Label: "mock::local"}

	svc := services.NewSyncService(nil, dst, src, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		"", "",
	)
	_, err := svc.Download(t.Context(), local, remote)
	require.Error(t, err)
	assert.Equal(t, originalLocal, local.XXHashMap, "input local map must not be mutated")
}

// TestSyncService_Download_EmptyDiff returns the remote state unchanged.
func TestSyncService_Download_EmptyDiff(t *testing.T) {
	state := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}}
	src := &mocks.MockStorageRepository{Label: "mock::remote"}
	dst := &mocks.MockStorageRepository{Label: "mock::local"}

	svc := services.NewSyncService(nil, dst, src, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		"", "",
	)
	result, err := svc.Download(t.Context(), state, state)
	require.NoError(t, err)
	assert.Equal(t, state.XXHashMap, result.XXHashMap)
}

// TestSyncService_Upload_RequiresScanner surfaces a clear error when no
// scanner was wired and Upload is called.
func TestSyncService_Upload_RequiresScanner(t *testing.T) {
	src := &mocks.MockStorageRepository{Label: "mock::local"}
	dst := &mocks.MockStorageRepository{Label: "mock::remote"}
	svc := services.NewSyncService(nil, src, dst, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		"", "",
	)
	_, err := svc.Upload(t.Context(), domain.SyncState{}, domain.SyncState{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanner not wired")
}

// TestSyncService_Upload_EmptyDiff returns the freshly scanned state.
func TestSyncService_Upload_EmptyDiff(t *testing.T) {
	hashMap := map[string]domain.FileEntry{"a": {Hash: "h1", Size: 10}}
	scanner := &mocks.MockDirectoryScanner{
		ScanFunc: func(_ context.Context) (map[string]domain.FileEntry, error) {
			return maps.Clone(hashMap), nil
		},
	}
	src := &mocks.MockStorageRepository{Label: "mock::local"}
	dst := &mocks.MockStorageRepository{Label: "mock::remote"}
	svc := services.NewSyncService(scanner, src, dst, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		"", "",
	)
	remote := domain.SyncState{XXHashMap: hashMap}
	result, err := svc.Upload(t.Context(), domain.SyncState{}, remote)
	require.NoError(t, err)
	assert.Equal(t, hashMap, result.XXHashMap)
}
