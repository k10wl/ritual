package refs_test

import (
	"context"
	"io"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// faultyStorage wraps a real fsBundle's storage with per-key PutStream /
// Delete failure injection. Use it in tests that exercise spec
// crash-recovery rows whose shape is "operation X on key K fails;
// verify invariant Y holds on the remaining state."
//
// The embedded fsBundle provides every other StorageRepository method
// unchanged (through the keyCounter decorator), so faultyStorage drops
// in wherever a ports.StorageRepository is expected.
type faultyStorage struct {
	bundle     *fsBundle
	putFail    map[string]error
	deleteFail map[string]error
	listFail   map[string]error
	getFail    map[string]error
}

func newFaultyStorage(b *fsBundle) *faultyStorage {
	return &faultyStorage{
		bundle:     b,
		putFail:    map[string]error{},
		deleteFail: map[string]error{},
		listFail:   map[string]error{},
		getFail:    map[string]error{},
	}
}

func (f *faultyStorage) String() string { return "faulty::" + f.bundle.storage.String() }

// put proxies to the underlying fsBundle seeder so commit-fault scenarios
// can pre-populate blobs without hitting the PutStream fault injector.
func (f *faultyStorage) put(t *testing.T, key string, data []byte) {
	t.Helper()
	f.bundle.put(t, key, data)
}

// decodeRef exposes the underlying fsBundle's ref decoder so commit-fault
// scenarios can assert on the written ref JSON without bypassing the
// faulty wrapper.
func (f *faultyStorage) decodeRef(t *testing.T, id domain.RefID) (*domain.Ref, bool) {
	t.Helper()
	return f.bundle.decodeRef(t, id)
}

// mustGet proxies to the underlying fsBundle reader for the same reason.
func (f *faultyStorage) mustGet(t *testing.T, key string) []byte {
	t.Helper()
	return f.bundle.mustGet(t, key)
}

// keys proxies to the underlying fsBundle walker.
func (f *faultyStorage) keys() []string { return f.bundle.keys() }

// putHits / getHits proxy to the underlying key counter.
func (f *faultyStorage) putHits(key string) int { return f.bundle.putHits(key) }
func (f *faultyStorage) getHits(key string) int { return f.bundle.getHits(key) }

func (f *faultyStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	err, shouldFail := f.getFail[key]
	if shouldFail {
		return nil, err
	}
	return f.bundle.storage.GetStream(ctx, key)
}

func (f *faultyStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	err, shouldFail := f.putFail[key]
	if shouldFail {
		return err
	}
	return f.bundle.storage.PutStream(ctx, key, body)
}

func (f *faultyStorage) Exists(ctx context.Context, key string) (bool, error) {
	return f.bundle.storage.Exists(ctx, key)
}

func (f *faultyStorage) Delete(ctx context.Context, key string) error {
	err, shouldFail := f.deleteFail[key]
	if shouldFail {
		return err
	}
	return f.bundle.storage.Delete(ctx, key)
}

func (f *faultyStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return f.bundle.storage.DeleteBatch(ctx, keys)
}

func (f *faultyStorage) List(ctx context.Context, prefix string) ([]string, error) {
	err, shouldFail := f.listFail[prefix]
	if shouldFail {
		return nil, err
	}
	return f.bundle.storage.List(ctx, prefix)
}

func (f *faultyStorage) Copy(ctx context.Context, src, dst string) error {
	return f.bundle.storage.Copy(ctx, src, dst)
}

var _ ports.StorageRepository = (*faultyStorage)(nil)
