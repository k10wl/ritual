package adapters

import (
	"context"
	"fmt"
	"io"
	"ritual/internal/core/ports"
	"sync/atomic"
)

// StorageCounters holds byte + op totals tapped by CounterStorage. All fields
// are safe for concurrent read/increment via the atomic.Int64 methods.
//
// Byte counters are layer-dependent — the decorator measures whichever bytes
// flow through the layer it is installed at:
//
//   - Above CompressingStorage → caller-side / logical bytes (uncompressed).
//     Drives BytesTotal / BytesDone for the progress bar — the units that
//     match PlanInfo.BytesTotal.
//   - Below CompressingStorage → backend-side / wire bytes (compressed).
//     Drives the smoothed speed label — the units that match an operator's
//     mental model of uplink/downlink.
//
// One counter pair per layer per side; install both for full visibility
// (see design-log/001-progress-projection.md §"Two counter layers" and the
// composition root in cmd/gui/main.go).
//
// Retry bytes: when an inner adapter rewinds its body and retries (e.g. R2
// over a transient 5xx), every retransmitted byte advances the counter at
// whichever layer the retry happens through. A counter below the retrying
// adapter sees the retry traffic; one above it does not.
type StorageCounters struct {
	BytesIn     atomic.Int64
	BytesOut    atomic.Int64
	OpsComplete atomic.Int64
	OpsFailed   atomic.Int64
}

// CounterStorage is a metrics-only decorator. It publishes no events; its
// exclusive job is to tee byte/op counts into shared atomics that a progress
// ticker (see internal/adapters/progress) snapshots at a UI cadence. Pair
// with ObservedStorage for per-op lifecycle events.
type CounterStorage struct {
	inner    ports.StorageRepository
	counters *StorageCounters
}

// NewCounterStorage wires inner through the tap. counters must not be nil.
func NewCounterStorage(inner ports.StorageRepository, counters *StorageCounters) *CounterStorage {
	return &CounterStorage{inner: inner, counters: counters}
}

func (c *CounterStorage) String() string { return fmt.Sprint(c.inner) }

// GetStream wraps the returned body in a tap-reader that increments BytesIn
// per Read. Op counters advance on body Close (success or failure).
func (c *CounterStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	body, err := c.inner.GetStream(ctx, key)
	if err != nil {
		c.counters.OpsComplete.Add(1)
		c.counters.OpsFailed.Add(1)
		return nil, err
	}
	return &counterTapReadCloser{
		inner:   body,
		counter: &c.counters.BytesIn,
		onClose: c.completeOp,
	}, nil
}

// PutStream wraps the body so every Read consumed by the inner adapter
// increments BytesOut. Op counters advance when the inner call returns.
// If body is also seekable, the wrapper preserves Seek so the inner
// adapter's retry rewind path (R2) still functions.
func (c *CounterStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	wrap := wrapCounterTap(body, &c.counters.BytesOut)
	err := c.inner.PutStream(ctx, key, wrap)
	c.completeOp(err)
	return err
}

// wrapCounterTap returns body wrapped so every Read consumed by the
// inner adapter advances counter. When body implements io.Seeker the
// returned wrapper passes Seek through; otherwise it is a plain Reader.
func wrapCounterTap(body io.Reader, counter *atomic.Int64) io.Reader {
	base := &counterTapReader{inner: body, counter: counter}
	if s, ok := body.(io.Seeker); ok {
		return &counterTapReadSeeker{counterTapReader: base, seeker: s}
	}
	return base
}

// Exists advances op counters only (no byte transfer).
func (c *CounterStorage) Exists(ctx context.Context, key string) (bool, error) {
	hit, err := c.inner.Exists(ctx, key)
	c.completeOp(err)
	return hit, err
}

func (c *CounterStorage) completeOp(err error) {
	c.counters.OpsComplete.Add(1)
	if err != nil {
		c.counters.OpsFailed.Add(1)
	}
}

// Delete forwards unchanged.
func (c *CounterStorage) Delete(ctx context.Context, key string) error {
	return c.inner.Delete(ctx, key)
}

// DeleteBatch forwards unchanged.
func (c *CounterStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return c.inner.DeleteBatch(ctx, keys)
}

// List forwards unchanged.
func (c *CounterStorage) List(ctx context.Context, prefix string) ([]string, error) {
	return c.inner.List(ctx, prefix)
}

// Copy forwards unchanged.
func (c *CounterStorage) Copy(ctx context.Context, src, dst string) error {
	return c.inner.Copy(ctx, src, dst)
}

// counterTapReadCloser increments counter by the number of bytes delivered on
// every Read. Mirrors the POC r2sim countingReader pattern verbatim.
type counterTapReadCloser struct {
	inner   io.ReadCloser
	counter *atomic.Int64
	onClose func(error)
	closed  bool
}

func (c *counterTapReadCloser) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		c.counter.Add(int64(n))
	}
	return n, err
}

func (c *counterTapReadCloser) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.inner.Close()
	c.onClose(err)
	return err
}

// counterTapReader taps Read calls against the counter. Used when the
// source body is not seekable.
type counterTapReader struct {
	inner   io.Reader
	counter *atomic.Int64
}

func (c *counterTapReader) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		c.counter.Add(int64(n))
	}
	return n, err
}

// counterTapReadSeeker wraps counterTapReader and re-exports Seek for
// seekable source bodies. Inner R2 retry rewind goes through this path.
type counterTapReadSeeker struct {
	*counterTapReader
	seeker io.Seeker
}

func (c *counterTapReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return c.seeker.Seek(offset, whence)
}

var _ ports.StorageRepository = (*CounterStorage)(nil)
