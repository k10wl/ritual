package services_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/domain"
	mocks "ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
)

func TestSyncService_Download_EmptyDiff(t *testing.T) {
	state := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}}
	svc := services.NewSyncService(nil, nil, nil, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		t.TempDir(), "sync/test",
	)
	result, err := svc.Download(context.Background(), state, state)
	require.NoError(t, err)
	assert.Equal(t, state.XXHashMap, result.XXHashMap)
}

func TestSyncService_Upload_EmptyDiff(t *testing.T) {
	hashMap := map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}
	mockScanner := &mocks.MockDirectoryScanner{
		ScanFunc: func(ctx context.Context) (map[string]domain.FileEntry, error) {
			return maps.Clone(hashMap), nil
		},
	}
	remote := domain.SyncState{XXHashMap: hashMap}

	svc := services.NewSyncService(mockScanner, nil, nil, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		t.TempDir(), "sync/test",
	)
	result, err := svc.Upload(context.Background(), domain.SyncState{}, remote)
	require.NoError(t, err)
	assert.Equal(t, hashMap, result.XXHashMap)
}

func TestSyncService_Download_ValueSemantics(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}}
	remote := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h2", Size: 11},
		"b": {Hash: "h3", Size: 12},
	}}
	originalLocal := maps.Clone(local.XXHashMap)

	mockRemote := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) {
			return []byte("data"), nil
		},
	}

	svc := services.NewSyncService(nil, nil, mockRemote, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		t.TempDir(), "sync/test",
	)
	_, err := svc.Download(context.Background(), local, remote)
	require.NoError(t, err)
	assert.Equal(t, originalLocal, local.XXHashMap, "original local state must not be mutated")
}

func TestSyncService_Download_StageFailure(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]domain.FileEntry{}}
	remote := domain.SyncState{XXHashMap: map[string]domain.FileEntry{
		"a": {Hash: "h1", Size: 10},
	}}

	mockRemote := &mocks.MockStorageRepository{
		GetFunc: func(ctx context.Context, key string) ([]byte, error) {
			return nil, errors.New("network error")
		},
	}

	svc := services.NewSyncService(nil, nil, mockRemote, nil,
		services.SyncConfig{Prefix: "test", LocalDir: t.TempDir()},
		t.TempDir(), "sync/test",
	)
	_, err := svc.Download(context.Background(), local, remote)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stage")
}
