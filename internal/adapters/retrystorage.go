package adapters

import (
	"context"
	"time"

	retry "github.com/avast/retry-go/v4"

	"ritual/internal/core/ports"
)

// RetryStorageRepository decorates a StorageRepository with retry logic.
// SyncService and other consumers are unaware of retries — they see a normal StorageRepository.
type RetryStorageRepository struct {
	inner      ports.StorageRepository
	maxRetries uint
	baseDelay  time.Duration
	maxDelay   time.Duration
}

var _ ports.StorageRepository = (*RetryStorageRepository)(nil)

// NewRetryStorageRepository wraps a storage repository with exponential backoff retry.
func NewRetryStorageRepository(inner ports.StorageRepository, maxRetries uint, baseDelay, maxDelay time.Duration) *RetryStorageRepository {
	return &RetryStorageRepository{
		inner:      inner,
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
	}
}

func (r *RetryStorageRepository) retryOpts(ctx context.Context) []retry.Option {
	return []retry.Option{
		retry.Attempts(r.maxRetries),
		retry.Delay(r.baseDelay),
		retry.MaxDelay(r.maxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.Context(ctx),
	}
}

func (r *RetryStorageRepository) Get(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	err := retry.Do(func() error {
		var getErr error
		data, getErr = r.inner.Get(ctx, key)
		return getErr
	}, r.retryOpts(ctx)...)
	return data, err
}

func (r *RetryStorageRepository) Put(ctx context.Context, key string, data []byte) error {
	return retry.Do(func() error {
		return r.inner.Put(ctx, key, data)
	}, r.retryOpts(ctx)...)
}

func (r *RetryStorageRepository) Delete(ctx context.Context, key string) error {
	return retry.Do(func() error {
		return r.inner.Delete(ctx, key)
	}, r.retryOpts(ctx)...)
}

func (r *RetryStorageRepository) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := retry.Do(func() error {
		var listErr error
		keys, listErr = r.inner.List(ctx, prefix)
		return listErr
	}, r.retryOpts(ctx)...)
	return keys, err
}

func (r *RetryStorageRepository) Copy(ctx context.Context, sourceKey string, destKey string) error {
	return retry.Do(func() error {
		return r.inner.Copy(ctx, sourceKey, destKey)
	}, r.retryOpts(ctx)...)
}
