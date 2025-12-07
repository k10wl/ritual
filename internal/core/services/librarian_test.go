package services

import (
	"context"
	"encoding/json"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLibrarianService_GetLocalManifest(t *testing.T) {
	tests := []struct {
		name           string
		storageData    []byte
		storageError   error
		expectedResult *domain.Manifest
		expectedError  bool
	}{
		{
			name: "successful retrieval",
			storageData: []byte(`{
				"ritual_version": "1.0.0",
				"locked_by": "",
				"instance_version": "test-instance",
				"worlds": [],
				"updated_at": "2023-01-01T00:00:00Z"
			}`),
			storageError: nil,
			expectedResult: &domain.Manifest{
				RitualVersion:   "1.0.0",
				LockedBy:        "",
				InstanceVersion: "test-instance",
				StoredWorlds:    []domain.World{},
			},
			expectedError: false,
		},
		{
			name:           "storage error",
			storageData:    nil,
			storageError:   errors.New("storage error"),
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name:           "invalid json",
			storageData:    []byte("invalid json"),
			storageError:   nil,
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name:           "empty data",
			storageData:    []byte(""),
			storageError:   nil,
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name:           "malformed json structure",
			storageData:    []byte(`{"ritual_version": "1.0.0", "invalid": }`),
			storageError:   nil,
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)
			mockStorage.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
				if key == "manifest.json" {
					return tt.storageData, tt.storageError
				}
				return nil, errors.New("unexpected key")
			}

			service, err := NewLibrarianService(mockStorage, mockStorage)
			assert.NoError(t, err)

			result, err := service.GetLocalManifest(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult.RitualVersion, result.RitualVersion)
				assert.Equal(t, tt.expectedResult.LockedBy, result.LockedBy)
				assert.Equal(t, tt.expectedResult.InstanceVersion, result.InstanceVersion)
				assert.Equal(t, tt.expectedResult.StoredWorlds, result.StoredWorlds)
			}
		})
	}
}

func TestLibrarianService_GetRemoteManifest(t *testing.T) {
	tests := []struct {
		name           string
		storageData    []byte
		storageError   error
		expectedResult *domain.Manifest
		expectedError  bool
	}{
		{
			name: "successful retrieval",
			storageData: []byte(`{
				"ritual_version": "1.0.0",
				"locked_by": "user::1234567890",
				"instance_version": "test-instance",
				"worlds": [],
				"updated_at": "2023-01-01T00:00:00Z"
			}`),
			storageError: nil,
			expectedResult: &domain.Manifest{
				RitualVersion:   "1.0.0",
				LockedBy:        "user::1234567890",
				InstanceVersion: "test-instance",
				StoredWorlds:    []domain.World{},
			},
			expectedError: false,
		},
		{
			name:           "storage error",
			storageData:    nil,
			storageError:   errors.New("storage error"),
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name:           "empty data",
			storageData:    []byte(""),
			storageError:   nil,
			expectedResult: nil,
			expectedError:  true,
		},
		{
			name:           "malformed json structure",
			storageData:    []byte(`{"ritual_version": "1.0.0", "invalid": }`),
			storageError:   nil,
			expectedResult: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)
			mockStorage.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
				if key == "manifest.json" {
					return tt.storageData, tt.storageError
				}
				return nil, errors.New("unexpected key")
			}

			service, err := NewLibrarianService(mockStorage, mockStorage)
			assert.NoError(t, err)

			result, err := service.GetRemoteManifest(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult.RitualVersion, result.RitualVersion)
				assert.Equal(t, tt.expectedResult.LockedBy, result.LockedBy)
				assert.Equal(t, tt.expectedResult.InstanceVersion, result.InstanceVersion)
				assert.Equal(t, tt.expectedResult.StoredWorlds, result.StoredWorlds)
			}
		})
	}
}

func TestLibrarianService_SaveLocalManifest(t *testing.T) {
	tests := []struct {
		name          string
		manifest      *domain.Manifest
		storageError  error
		expectedError bool
	}{
		{
			name: "successful save",
			manifest: &domain.Manifest{
				RitualVersion:   "1.0.0",
				LockedBy:        "",
				InstanceVersion: "test-instance",
			},
			storageError:  nil,
			expectedError: false,
		},
		{
			name: "storage error",
			manifest: &domain.Manifest{
				RitualVersion:   "1.0.0",
				InstanceVersion: "test-instance",
			},
			storageError:  errors.New("storage error"),
			expectedError: true,
		},
		{
			name:          "nil manifest",
			manifest:      nil,
			storageError:  nil,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)
			var callCount int
			mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
				callCount++
				if key != "manifest.json" {
					return errors.New("unexpected key")
				}
				if tt.manifest == nil {
					return errors.New("nil manifest")
				}
				var manifest domain.Manifest
				if err := json.Unmarshal(data, &manifest); err != nil {
					return err
				}
				return tt.storageError
			}

			service, err := NewLibrarianService(mockStorage, mockStorage)
			assert.NoError(t, err)

			err = service.SaveLocalManifest(context.Background(), tt.manifest)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.manifest == nil {
					assert.Equal(t, 0, callCount)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, callCount)
			}
		})
	}
}

func TestLibrarianService_SaveRemoteManifest(t *testing.T) {
	tests := []struct {
		name          string
		manifest      *domain.Manifest
		storageError  error
		expectedError bool
	}{
		{
			name: "successful save",
			manifest: &domain.Manifest{
				RitualVersion:   "1.0.0",
				LockedBy:        "user::1234567890",
				InstanceVersion: "test-instance",
			},
			storageError:  nil,
			expectedError: false,
		},
		{
			name: "storage error",
			manifest: &domain.Manifest{
				RitualVersion:   "1.0.0",
				InstanceVersion: "test-instance",
			},
			storageError:  errors.New("storage error"),
			expectedError: true,
		},
		{
			name:          "nil manifest",
			manifest:      nil,
			storageError:  nil,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)
			var callCount int
			mockStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
				callCount++
				if key != "manifest.json" {
					return errors.New("unexpected key")
				}
				if tt.manifest == nil {
					return errors.New("nil manifest")
				}
				var manifest domain.Manifest
				if err := json.Unmarshal(data, &manifest); err != nil {
					return err
				}
				return tt.storageError
			}

			service, err := NewLibrarianService(mockStorage, mockStorage)
			assert.NoError(t, err)

			err = service.SaveRemoteManifest(context.Background(), tt.manifest)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.manifest == nil {
					assert.Equal(t, 0, callCount)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, callCount)
			}
		})
	}
}

func TestNewLibrarianService_NilParameters(t *testing.T) {
	tests := []struct {
		name          string
		localStorage  ports.StorageRepository
		remoteStorage ports.StorageRepository
		expectedError string
	}{
		{
			name:          "nil local storage",
			localStorage:  nil,
			remoteStorage: mocks.NewMockStorageRepository(),
			expectedError: "localStorage cannot be nil",
		},
		{
			name:          "nil remote storage",
			localStorage:  mocks.NewMockStorageRepository(),
			remoteStorage: nil,
			expectedError: "remoteStorage cannot be nil",
		},
		{
			name:          "both nil",
			localStorage:  nil,
			remoteStorage: nil,
			expectedError: "localStorage cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewLibrarianService(tt.localStorage, tt.remoteStorage)
			assert.Error(t, err)
			assert.Nil(t, service)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestLibrarianService_Integration(t *testing.T) {
	mockLocalStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)
	mockRemoteStorage := mocks.NewMockStorageRepository().(*mocks.MockStorageRepository)

	service, err := NewLibrarianService(mockLocalStorage, mockRemoteStorage)
	assert.NoError(t, err)

	manifest := &domain.Manifest{
		RitualVersion:   "1.0.0",
		LockedBy:        "user::1234567890",
		InstanceVersion: "test-instance",
		StoredWorlds: []domain.World{
			{URI: "world1", CreatedAt: time.Now()},
		},
	}

	var localCallCount, remoteCallCount int
	mockLocalStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		localCallCount++
		if key != "manifest.json" {
			return errors.New("unexpected key")
		}
		return nil
	}

	mockRemoteStorage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		remoteCallCount++
		if key != "manifest.json" {
			return errors.New("unexpected key")
		}
		return nil
	}

	mockLocalStorage.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		if key != "manifest.json" {
			return nil, errors.New("unexpected key")
		}
		return json.Marshal(manifest)
	}

	mockRemoteStorage.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		if key != "manifest.json" {
			return nil, errors.New("unexpected key")
		}
		return json.Marshal(manifest)
	}

	err = service.SaveLocalManifest(context.Background(), manifest)
	assert.NoError(t, err)
	assert.Equal(t, 1, localCallCount)

	err = service.SaveRemoteManifest(context.Background(), manifest)
	assert.NoError(t, err)
	assert.Equal(t, 1, remoteCallCount)

	retrievedLocal, err := service.GetLocalManifest(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, manifest.RitualVersion, retrievedLocal.RitualVersion)
	assert.Equal(t, manifest.LockedBy, retrievedLocal.LockedBy)

	retrievedRemote, err := service.GetRemoteManifest(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, manifest.RitualVersion, retrievedRemote.RitualVersion)
	assert.Equal(t, manifest.LockedBy, retrievedRemote.LockedBy)
}
