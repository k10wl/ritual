package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
)

func newTestSyncService(
	scanner *mocks.MockWorldScanner,
	local *mocks.MockStorageRepository,
	remote *mocks.MockStorageRepository,
	librarian *mocks.MockLibrarianService,
) (*SyncService, chan ports.Event) {
	events := make(chan ports.Event, 100)
	svc, _ := NewSyncService(scanner, local, remote, librarian, events)
	return svc, events
}

// --- Constructor tests ---

func TestNewSyncService_NilScanner(t *testing.T) {
	_, err := NewSyncService(nil, &mocks.MockStorageRepository{}, &mocks.MockStorageRepository{}, &mocks.MockLibrarianService{}, nil)
	assert.ErrorIs(t, err, ErrSyncScannerNil)
}

func TestNewSyncService_NilLocal(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, nil, &mocks.MockStorageRepository{}, &mocks.MockLibrarianService{}, nil)
	assert.ErrorIs(t, err, ErrSyncLocalNil)
}

func TestNewSyncService_NilRemote(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, &mocks.MockStorageRepository{}, nil, &mocks.MockLibrarianService{}, nil)
	assert.ErrorIs(t, err, ErrSyncRemoteNil)
}

func TestNewSyncService_NilLibrarian(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, &mocks.MockStorageRepository{}, &mocks.MockStorageRepository{}, nil, nil)
	assert.ErrorIs(t, err, ErrSyncLibrarianNil)
}

// --- Download tests ---

func TestSyncService_Download_EmptyDiff(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	local := &mocks.MockStorageRepository{}
	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}

	hashMap := map[string]string{"a.dat": "h1"}

	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: hashMap}, nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: hashMap}, nil
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Download(context.Background())

	assert.NoError(t, err)
}

func TestSyncService_Download_HappyPath(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	local := &mocks.MockStorageRepository{}
	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}

	stored := make(map[string][]byte)
	var savedManifest *domain.Manifest

	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{}}, nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "h1"}}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error {
		savedManifest = m
		return nil
	}

	remote.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		return []byte("file-content"), nil
	}
	local.PutFunc = func(ctx context.Context, key string, data []byte) error {
		stored[key] = data
		return nil
	}
	local.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		if d, ok := stored[key]; ok {
			return d, nil
		}
		return nil, errors.New("not found")
	}
	local.DeleteFunc = func(ctx context.Context, key string) error {
		delete(stored, key)
		return nil
	}
	local.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		// Return current worlds files after P2 move
		var keys []string
		for k := range stored {
			if len(k) > 7 && k[:7] == "worlds/" {
				keys = append(keys, k)
			}
		}
		return keys, nil
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Download(context.Background())

	assert.NoError(t, err)
	require.NotNil(t, savedManifest)
	assert.Equal(t, "h1", savedManifest.XXHashMap["a.dat"])
}

func TestSyncService_Download_P1Failure(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	local := &mocks.MockStorageRepository{}
	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}

	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{}}, nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "h1"}}, nil
	}

	remote.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		return nil, errors.New("network error")
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Download(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download P1 failed")
}

// --- Upload tests ---

func TestSyncService_Upload_EmptyDiff(t *testing.T) {
	hashMap := map[string]string{"a.dat": "h1"}

	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return hashMap, nil
	}

	local := &mocks.MockStorageRepository{}
	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}

	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error {
		return nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: hashMap}, nil
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Upload(context.Background())

	assert.NoError(t, err)
}

func TestSyncService_Upload_HappyPath(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"a.dat": "new_hash"}, nil
	}

	localStore := make(map[string][]byte)
	localStore["worlds/a.dat"] = []byte("file-content")

	remoteStore := make(map[string][]byte)
	var savedRemoteManifest *domain.Manifest

	local := &mocks.MockStorageRepository{}
	local.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		if d, ok := localStore[key]; ok {
			return d, nil
		}
		return nil, errors.New("not found")
	}

	remote := &mocks.MockStorageRepository{}
	remote.PutFunc = func(ctx context.Context, key string, data []byte) error {
		remoteStore[key] = data
		return nil
	}
	remote.CopyFunc = func(ctx context.Context, src, dst string) error {
		remoteStore[dst] = remoteStore[src]
		return nil
	}
	remote.DeleteFunc = func(ctx context.Context, key string) error {
		delete(remoteStore, key)
		return nil
	}

	librarian := &mocks.MockLibrarianService{}
	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error {
		return nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "old_hash"}}, nil
	}
	librarian.SaveRemoteManifestFunc = func(ctx context.Context, m *domain.Manifest) error {
		savedRemoteManifest = m
		return nil
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Upload(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, remoteStore, "worlds/a.dat")
	require.NotNil(t, savedRemoteManifest)
	assert.Equal(t, "new_hash", savedRemoteManifest.XXHashMap["a.dat"])
}

func TestSyncService_Upload_P1Failure(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"a.dat": "new_hash"}, nil
	}

	local := &mocks.MockStorageRepository{}
	local.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		return nil, errors.New("disk read error")
	}

	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}
	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error {
		return nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "old_hash"}}, nil
	}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Upload(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload P1 failed")
}

func TestSyncService_Upload_WithDeletions(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"a.dat": "h1"}, nil // b.dat deleted locally
	}

	local := &mocks.MockStorageRepository{}
	local.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		return []byte("data"), nil
	}

	deletedRemoteKeys := []string{}
	remote := &mocks.MockStorageRepository{}
	remote.PutFunc = func(ctx context.Context, key string, data []byte) error { return nil }
	remote.CopyFunc = func(ctx context.Context, src, dst string) error { return nil }
	remote.DeleteFunc = func(ctx context.Context, key string) error {
		deletedRemoteKeys = append(deletedRemoteKeys, key)
		return nil
	}

	librarian := &mocks.MockLibrarianService{}
	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error { return nil }
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "h1", "b.dat": "h2"}}, nil
	}
	librarian.SaveRemoteManifestFunc = func(ctx context.Context, m *domain.Manifest) error { return nil }

	// No upload needed (a.dat unchanged), but deletion of b.dat
	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Upload(context.Background())

	assert.NoError(t, err)
	assert.Contains(t, deletedRemoteKeys, "worlds/b.dat")
}

func TestSyncService_Upload_ScanFailure(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return nil, errors.New("walk error")
	}

	local := &mocks.MockStorageRepository{}
	remote := &mocks.MockStorageRepository{}
	librarian := &mocks.MockLibrarianService{}

	svc, _ := newTestSyncService(scanner, local, remote, librarian)
	err := svc.Upload(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan worlds")
}

func TestSyncService_Download_NilService(t *testing.T) {
	var svc *SyncService
	err := svc.Download(context.Background())
	assert.ErrorIs(t, err, ErrSyncNil)
}

func TestSyncService_Upload_NilService(t *testing.T) {
	var svc *SyncService
	err := svc.Upload(context.Background())
	assert.ErrorIs(t, err, ErrSyncNil)
}
