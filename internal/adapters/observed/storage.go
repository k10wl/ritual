package observed

import (
	"context"
	"fmt"
	"io"
	"ritual/internal/core/ports"
	"time"
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

func (o *observedStorage) Copy(ctx context.Context, src, dst string) error {
	start := time.Now()
	err := o.inner.Copy(ctx, src, dst)
	o.publish(StorageCopyInfo{Store: o.label, SrcKey: src, DstKey: dst, DurationMs: sinceMs(start), Err: err})
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

// GetStream opens the inner stream and wraps it in a counting reader that
// publishes StorageGetStreamInfo on Close. When the inner call errors before a
// body is returned, the event is published immediately with 0 bytes + err.
func (o *observedStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	start := time.Now()
	body, err := o.inner.GetStream(ctx, key)
	if err != nil {
		o.publish(StorageGetStreamInfo{Store: o.label, Key: key, DurationMs: sinceMs(start), Err: err})
		return nil, err
	}
	return &countingReadCloser{
		inner: body,
		onClose: func(n int64, closeErr error) {
			o.publish(StorageGetStreamInfo{Store: o.label, Key: key, Bytes: n, DurationMs: sinceMs(start), Err: closeErr})
		},
	}, nil
}

// PutStream publishes StoragePutStreamInfo with the intended object size.
// When body is seekable the size is discovered via Seek(0,End) before the
// inner call (preserving prior behaviour so event consumers still see the
// "intended" size on failure). When body is a plain Reader — pull's rc
// from a non-seekable remote — a byte-counting tap records bytes actually
// consumed and the event reports that instead.
func (o *observedStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	start := time.Now()
	if seeker, ok := body.(io.Seeker); ok {
		size, sizeErr := seekableSize(seeker)
		if sizeErr == nil {
			putErr := o.inner.PutStream(ctx, key, body)
			o.publish(StoragePutStreamInfo{Store: o.label, Key: key, Bytes: size, DurationMs: sinceMs(start), Err: putErr})
			return putErr
		}
	}
	tap := &countingReader{inner: body}
	putErr := o.inner.PutStream(ctx, key, tap)
	o.publish(StoragePutStreamInfo{Store: o.label, Key: key, Bytes: tap.Count(), DurationMs: sinceMs(start), Err: putErr})
	return putErr
}

func seekableSize(s io.Seeker) (int64, error) {
	end, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = s.Seek(0, io.SeekStart)
	return end, err
}

// Exists wraps the inner call and publishes StorageExistsInfo with hit/miss.
func (o *observedStorage) Exists(ctx context.Context, key string) (bool, error) {
	start := time.Now()
	hit, err := o.inner.Exists(ctx, key)
	o.publish(StorageExistsInfo{Store: o.label, Key: key, Hit: hit, DurationMs: sinceMs(start), Err: err})
	return hit, err
}

// countingReadCloser tallies bytes streamed through a ReadCloser and invokes
// onClose exactly once, when the caller closes the body.
type countingReadCloser struct {
	inner   io.ReadCloser
	n       int64
	closed  bool
	onClose func(n int64, err error)
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	read, err := c.inner.Read(p)
	c.n += int64(read)
	return read, err
}

func (c *countingReadCloser) Close() error {
	if c.closed {
		return c.inner.Close()
	}
	c.closed = true
	err := c.inner.Close()
	c.onClose(c.n, err)
	return err
}

// countingReader is the fallback tap for non-seekable bodies: records
// bytes delivered to the inner adapter so the completion event can still
// report an accurate Bytes value when pre-discovery via Seek is not
// available.
type countingReader struct {
	inner io.Reader
	n     int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	read, err := c.inner.Read(p)
	c.n += int64(read)
	return read, err
}

func (c *countingReader) Count() int64 { return c.n }

func (o *observedStorage) List(ctx context.Context, prefix string) ([]string, error) {
	start := time.Now()
	keys, err := o.inner.List(ctx, prefix)
	o.publish(StorageListInfo{Store: o.label, Prefix: prefix, Count: len(keys), DurationMs: sinceMs(start), Err: err})
	return keys, err
}
