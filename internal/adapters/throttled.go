package adapters

import (
	"context"
	"fmt"
	"io"
	"ritual/internal/core/ports"

	"golang.org/x/time/rate"
)

// ThrottledStorage decorates a StorageRepository with a byte-rate limit on
// PutStream / GetStream. Used to simulate constrained network bandwidth on
// the local-FS mock remote so the dev loop reflects real-world push/pull
// timings. Metadata ops (Exists, List, Delete, Copy) pass through unthrottled.
type ThrottledStorage struct {
	inner   ports.StorageRepository
	limiter *rate.Limiter
}

var _ ports.StorageRepository = (*ThrottledStorage)(nil)

// NewThrottledStorage wraps inner with a token-bucket limited to bytesPerSec.
// Burst capacity is 100 ms-worth of bytes, clamped to ≥ 64 KiB so a typical
// io.Copy 32 KiB chunk never exceeds the bucket and stalls indefinitely.
func NewThrottledStorage(inner ports.StorageRepository, bytesPerSec int) *ThrottledStorage {
	burst := bytesPerSec / 10
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return &ThrottledStorage{
		inner:   inner,
		limiter: rate.NewLimiter(rate.Limit(bytesPerSec), burst),
	}
}

func (t *ThrottledStorage) String() string {
	return fmt.Sprintf("throttled::%s", t.inner)
}

func (t *ThrottledStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	return t.inner.PutStream(ctx, key, &throttledReader{r: body, lim: t.limiter, ctx: ctx})
}

func (t *ThrottledStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := t.inner.GetStream(ctx, key)
	if err != nil {
		return nil, err
	}
	return &throttledReadCloser{ReadCloser: rc, lim: t.limiter, ctx: ctx}, nil
}

func (t *ThrottledStorage) Exists(ctx context.Context, key string) (bool, error) {
	return t.inner.Exists(ctx, key)
}

func (t *ThrottledStorage) Delete(ctx context.Context, key string) error {
	return t.inner.Delete(ctx, key)
}

func (t *ThrottledStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return t.inner.DeleteBatch(ctx, keys)
}

func (t *ThrottledStorage) List(ctx context.Context, prefix string) ([]string, error) {
	return t.inner.List(ctx, prefix)
}

func (t *ThrottledStorage) Copy(ctx context.Context, sourceKey, destKey string) error {
	return t.inner.Copy(ctx, sourceKey, destKey)
}

// Close releases the inner storage if it owns OS resources — the local-FS mock
// holds an os.Root handle (adapters.FSRepository) that keeps <root>/remote-mock
// open, which on Windows blocks directory removal until released. Backends with
// no handle to free (R2) don't implement io.Closer, so this is a no-op there.
func (t *ThrottledStorage) Close() error {
	if c, ok := t.inner.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type throttledReader struct {
	r   io.Reader
	lim *rate.Limiter
	ctx context.Context
}

func (tr *throttledReader) Read(p []byte) (int, error) {
	if burst := tr.lim.Burst(); len(p) > burst {
		p = p[:burst]
	}
	n, err := tr.r.Read(p)
	if n > 0 {
		if waitErr := tr.lim.WaitN(tr.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

type throttledReadCloser struct {
	io.ReadCloser
	lim *rate.Limiter
	ctx context.Context
}

func (tr *throttledReadCloser) Read(p []byte) (int, error) {
	if burst := tr.lim.Burst(); len(p) > burst {
		p = p[:burst]
	}
	n, err := tr.ReadCloser.Read(p)
	if n > 0 {
		if waitErr := tr.lim.WaitN(tr.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}
