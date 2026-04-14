package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

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
	svc, _ := NewSyncService(scanner, local, remote, librarian, events, "")
	return svc, events
}

// --- Constructor nil guards ---

func TestNewSyncService_NilScanner(t *testing.T) {
	_, err := NewSyncService(nil, &mocks.MockStorageRepository{}, &mocks.MockStorageRepository{}, &mocks.MockLibrarianService{}, nil, "")
	assert.ErrorIs(t, err, ErrSyncScannerNil)
}

func TestNewSyncService_NilLocal(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, nil, &mocks.MockStorageRepository{}, &mocks.MockLibrarianService{}, nil, "")
	assert.ErrorIs(t, err, ErrSyncLocalNil)
}

func TestNewSyncService_NilRemote(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, &mocks.MockStorageRepository{}, nil, &mocks.MockLibrarianService{}, nil, "")
	assert.ErrorIs(t, err, ErrSyncRemoteNil)
}

func TestNewSyncService_NilLibrarian(t *testing.T) {
	_, err := NewSyncService(&mocks.MockWorldScanner{}, &mocks.MockStorageRepository{}, &mocks.MockStorageRepository{}, nil, nil, "")
	assert.ErrorIs(t, err, ErrSyncLibrarianNil)
}

// --- Error paths (not covered by integration) ---

func TestSyncService_Download_P1Failure(t *testing.T) {
	librarian := &mocks.MockLibrarianService{}
	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{}}, nil
	}
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "h1"}}, nil
	}

	remote := &mocks.MockStorageRepository{}
	remote.GetFunc = func(ctx context.Context, key string) ([]byte, error) {
		return nil, errors.New("network error")
	}

	svc, _ := newTestSyncService(&mocks.MockWorldScanner{}, &mocks.MockStorageRepository{}, remote, librarian)
	err := svc.Download(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download P1 failed")
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

	librarian := &mocks.MockLibrarianService{}
	librarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{}, nil
	}
	librarian.SaveLocalManifestFunc = func(ctx context.Context, m *domain.Manifest) error { return nil }
	librarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return &domain.Manifest{XXHashMap: map[string]string{"a.dat": "old_hash"}}, nil
	}

	svc, _ := newTestSyncService(scanner, local, &mocks.MockStorageRepository{}, librarian)
	err := svc.Upload(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload P1 failed")
}

func TestSyncService_Upload_ScanFailure(t *testing.T) {
	scanner := &mocks.MockWorldScanner{}
	scanner.ScanFunc = func(ctx context.Context) (map[string]string, error) {
		return nil, errors.New("walk error")
	}

	svc, _ := newTestSyncService(scanner, &mocks.MockStorageRepository{}, &mocks.MockStorageRepository{}, &mocks.MockLibrarianService{})
	err := svc.Upload(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan worlds")
}

// --- Nil receiver guards ---

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
