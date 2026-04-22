package refs_test

import (
	"context"
	"io"
)

// faultyStorage wraps memStorage with per-key PutStream / Delete failure
// injection. Use it in tests that exercise spec crash-recovery rows whose
// shape is "operation X on key K fails; verify invariant Y holds on the
// remaining state."
//
// The embedded *memStorage provides every other StorageRepository method
// unchanged, so faultyStorage drops in wherever a memStorage is expected.
type faultyStorage struct {
	*memStorage
	putFail    map[string]error
	deleteFail map[string]error
}

func newFaultyStorage() *faultyStorage {
	return &faultyStorage{
		memStorage: newMemStorage(),
		putFail:    map[string]error{},
		deleteFail: map[string]error{},
	}
}

func (f *faultyStorage) PutStream(ctx context.Context, key string, body io.ReadSeeker) error {
	err, shouldFail := f.putFail[key]
	if shouldFail {
		return err
	}
	return f.memStorage.PutStream(ctx, key, body)
}

func (f *faultyStorage) Delete(ctx context.Context, key string) error {
	err, shouldFail := f.deleteFail[key]
	if shouldFail {
		return err
	}
	return f.memStorage.Delete(ctx, key)
}
