package observed

import (
	"context"
	"fmt"
	"time"

	"ritual/internal/core/ports"
)

// observedStorage wraps a StorageRepository and publishes one completion
// event per method call. Errors are forwarded verbatim to the caller; the
// event mirrors them for observability without altering control flow.
type observedStorage struct {
	inner ports.StorageRepository
	bus   ports.EventBus
	label string
}

// NewStorage decorates inner with bus-backed event publishing. The store
// label is captured once at construction via fmt.Sprint(inner) so adapters
// that mutate their identifier later still log under the original label.
func NewStorage(inner ports.StorageRepository, bus ports.EventBus) ports.StorageRepository {
	return &observedStorage{
		inner: inner,
		bus:   bus,
		label: fmt.Sprint(inner),
	}
}

// String forwards the inner adapter's identity so consumers that wrap an
// already-wrapped store see the original label.
func (o *observedStorage) String() string { return o.label }

func (o *observedStorage) publish(evt ports.Event) {
	if o.bus != nil {
		o.bus.Publish(evt)
	}
}

func sinceMs(start time.Time) int64 { return time.Since(start).Milliseconds() }

func (o *observedStorage) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	data, err := o.inner.Get(ctx, key)
	o.publish(StorageGetInfo{Store: o.label, Key: key, Bytes: len(data), DurationMs: sinceMs(start), Err: err})
	return data, err
}

func (o *observedStorage) Put(ctx context.Context, key string, data []byte) error {
	start := time.Now()
	err := o.inner.Put(ctx, key, data)
	o.publish(StoragePutInfo{Store: o.label, Key: key, Bytes: len(data), DurationMs: sinceMs(start), Err: err})
	return err
}

func (o *observedStorage) Copy(ctx context.Context, src, dst string) error {
	start := time.Now()
	err := o.inner.Copy(ctx, src, dst)
	o.publish(StorageCopyInfo{Store: o.label, SrcKey: src, DstKey: dst, DurationMs: sinceMs(start), Err: err})
	return err
}

func (o *observedStorage) Rename(ctx context.Context, src, dst string) error {
	start := time.Now()
	err := o.inner.Rename(ctx, src, dst)
	o.publish(StorageRenameInfo{Store: o.label, SrcKey: src, DstKey: dst, DurationMs: sinceMs(start), Err: err})
	return err
}

func (o *observedStorage) Delete(ctx context.Context, key string) error {
	start := time.Now()
	err := o.inner.Delete(ctx, key)
	o.publish(StorageDeleteInfo{Store: o.label, Key: key, DurationMs: sinceMs(start), Err: err})
	return err
}

func (o *observedStorage) DeleteBatch(ctx context.Context, keys []string) error {
	start := time.Now()
	err := o.inner.DeleteBatch(ctx, keys)
	o.publish(StorageDeleteBatchInfo{Store: o.label, Keys: keys, DurationMs: sinceMs(start), Err: err})
	return err
}

func (o *observedStorage) List(ctx context.Context, prefix string) ([]string, error) {
	start := time.Now()
	keys, err := o.inner.List(ctx, prefix)
	o.publish(StorageListInfo{Store: o.label, Prefix: prefix, Count: len(keys), DurationMs: sinceMs(start), Err: err})
	return keys, err
}
