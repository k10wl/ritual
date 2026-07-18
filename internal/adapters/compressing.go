package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"ritual/internal/core/ports"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
	"github.com/klauspost/compress/zstd"
)

// errV1OnCompressing is returned by the deprecated V1 methods. CompressingStorage
// is a pure V2 decorator; V1 callers must migrate before reaching it.
var errV1OnCompressing = errors.New("use V2 methods on CompressingStorage")

// bufferSize is the scratch-buffer size used by io.CopyBuffer on both hot paths.
const bufferSize = 64 * 1024

// pools bundles the three sync.Pools backing one "generation" of encoder/
// decoder reuse. Swapped out wholesale by release() rather than drained item
// by item — sync.Pool has no enumerate/clear API, so the only way to make
// pooled zstd encoders/decoders GC-eligible on demand is to stop referencing
// the pool that holds them.
type pools struct {
	buf sync.Pool
	enc sync.Pool
	dec sync.Pool
}

func newPools() *pools {
	p := &pools{}
	p.buf.New = func() any {
		b := make([]byte, bufferSize)
		return &b
	}
	return p
}

// CompressingStorage decorates a StorageRepository with zstd-3 compression on
// PutStream and zstd-decode + xxhash integrity check on GetStream. Designed
// for the `objects/{hash}` keyspace: the key's basename is the expected xxhash.
//
// Resource reuse is symmetric across directions: a sync.Pool of encoders for
// PutStream and a sync.Pool of decoders for GetStream. Each pooled instance is
// Reset to its sink/source per call and returned on completion — parallel
// pushes and pulls run without contention and without per-call encoder/
// decoder allocation.
//
// Each pooled zstd encoder/decoder retains an 8 MB window+match-table buffer
// (zstd's default WindowSize) for as long as it sits in the pool, and the
// pool grows to the peak concurrent PutStream/GetStream count — up to
// pullConcurrency workers on both encode and decode sides during a sync.
// That's the right trade during a sync, but the buffers have no business
// surviving after — call Release once the sync/push/pull that needed them
// is done (composition root wires this to the sync-flow's terminal event)
// to make them GC-eligible immediately instead of waiting on sync.Pool's own
// eviction timing.
type CompressingStorage struct {
	inner ports.StorageRepository
	pools atomic.Pointer[pools]
}

// NewCompressingStorage builds a V2 decorator around inner. Encoders and
// decoders are created on demand and retained by their pools across calls.
func NewCompressingStorage(inner ports.StorageRepository) (*CompressingStorage, error) {
	c := &CompressingStorage{inner: inner}
	c.pools.Store(newPools())
	return c, nil
}

// Release swaps in a fresh, empty pool set so every encoder/decoder retained
// by the current one — and their 8 MB buffers — becomes garbage as soon as
// any in-flight PutStream/GetStream holding a reference to the old set
// finishes. Safe to call concurrently with in-flight operations: each one
// captured its own pool-set pointer at acquire time and keeps operating on
// it correctly; it just won't repopulate a pool anyone will read from again.
func (c *CompressingStorage) Release() {
	c.pools.Store(newPools())
}

// String composes label as "compressed::<inner>" so observability can tell the
// decorator apart from the raw adapter.
func (c *CompressingStorage) String() string { return "compressed::" + fmt.Sprint(c.inner) }

// Get is retained only to satisfy the deprecated V1 surface and always errors.
func (c *CompressingStorage) Get(context.Context, string) ([]byte, error) {
	return nil, errV1OnCompressing
}

// Put is retained only to satisfy the deprecated V1 surface and always errors.
func (c *CompressingStorage) Put(context.Context, string, []byte) error {
	return errV1OnCompressing
}

// Rename is retained only to satisfy the deprecated V1 surface and always errors.
func (c *CompressingStorage) Rename(context.Context, string, string) error {
	return errV1OnCompressing
}

// Delete tree-deletes inner key unchanged.
func (c *CompressingStorage) Delete(ctx context.Context, key string) error {
	return c.inner.Delete(ctx, key)
}

// DeleteBatch forwards to inner unchanged.
func (c *CompressingStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return c.inner.DeleteBatch(ctx, keys)
}

// List forwards to inner unchanged.
func (c *CompressingStorage) List(ctx context.Context, prefix string) ([]string, error) {
	return c.inner.List(ctx, prefix)
}

// Copy forwards to inner unchanged. Compression is per-blob, not per-keyspace,
// so a copy of already-compressed bytes stays byte-identical.
func (c *CompressingStorage) Copy(ctx context.Context, sourceKey, destKey string) error {
	return c.inner.Copy(ctx, sourceKey, destKey)
}

// Exists forwards to inner unchanged.
func (c *CompressingStorage) Exists(ctx context.Context, key string) (bool, error) {
	return c.inner.Exists(ctx, key)
}

// PutStream consumes body, zstd-compresses it to a local tempfile, then
// hands the rewound *os.File to inner.PutStream. Tempfile spool (not a
// memory buffer) keeps RAM flat regardless of blob size; the inner
// adapter receives a native io.ReadSeeker so SDK-level retry rewind
// works without a secondary buffer. The tempfile is removed on return
// whether inner succeeds or fails.
func (c *CompressingStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	p := c.pools.Load()

	tmp, err := os.CreateTemp("", "ritual-cmprs-*")
	if err != nil {
		return fmt.Errorf("failed to open compression spool for %s: %w", key, err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	enc, err := c.acquireEncoder(p, tmp)
	if err != nil {
		return fmt.Errorf("failed to acquire zstd encoder for %s: %w", key, err)
	}
	bufPtr := p.buf.Get().(*[]byte)
	_, copyErr := io.CopyBuffer(enc, body, *bufPtr)
	p.buf.Put(bufPtr)
	closeErr := enc.Close()
	c.releaseEncoder(p, enc)
	if copyErr != nil {
		return fmt.Errorf("failed to compress %s: %w", key, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to finalize compressed %s: %w", key, closeErr)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind compression spool for %s: %w", key, err)
	}
	return c.inner.PutStream(ctx, key, tmp)
}

// GetStream returns a reader that decompresses the inner body and verifies
// xxhash(plaintext) matches the key's basename on Close. Integrity mismatch
// surfaces as an error from Close, mirroring io semantics.
//
// A pooled zstd.Decoder is Reset to the inner body for this call and returned
// to the pool on Close; parallel callers each take their own decoder without
// contention and steady-state avoids per-call decoder allocations.
func (c *CompressingStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	expected, err := expectedHashFromKey(key)
	if err != nil {
		return nil, err
	}
	body, err := c.inner.GetStream(ctx, key)
	if err != nil {
		return nil, err
	}
	p := c.pools.Load()
	dec, err := c.acquireDecoder(p, body)
	if err != nil {
		_ = body.Close()
		return nil, fmt.Errorf("failed to build zstd decoder for %s: %w", key, err)
	}
	return &integrityReadCloser{
		inner:    body,
		decoder:  dec,
		expected: expected,
		key:      key,
		hasher:   xxhash.New(),
		release:  func() { c.releaseDecoder(p, dec) },
	}, nil
}

// acquireEncoder returns a ready-to-use zstd.Encoder bound to sink. Reuses a
// pooled encoder when available; otherwise creates a fresh one. The pool grows
// naturally up to the concurrent-PutStream high-water mark within p's
// generation and is discarded wholesale by Release once the sync is done.
func (c *CompressingStorage) acquireEncoder(p *pools, sink io.Writer) (*zstd.Encoder, error) {
	if v := p.enc.Get(); v != nil {
		enc := v.(*zstd.Encoder)
		enc.Reset(sink)
		return enc, nil
	}
	return zstd.NewWriter(sink, zstd.WithEncoderLevel(zstd.SpeedDefault))
}

// releaseEncoder detaches the sink and returns the encoder to p's pool.
// Reset(io.Discard) keeps the internal window and match tables allocated
// while breaking the reference to the caller-owned sink — cheap reuse as
// long as p is still the live generation; a no-op cost once Release has
// moved on, since nothing will Get() from an orphaned p again.
func (c *CompressingStorage) releaseEncoder(p *pools, enc *zstd.Encoder) {
	enc.Reset(io.Discard)
	p.enc.Put(enc)
}

// acquireDecoder returns a ready-to-use zstd.Decoder bound to src. Reuses a
// pooled decoder when available; otherwise creates a fresh one. Failure to
// Reset a pooled decoder falls back to a fresh decoder so one bad instance
// can't poison the pool.
func (c *CompressingStorage) acquireDecoder(p *pools, src io.Reader) (*zstd.Decoder, error) {
	if v := p.dec.Get(); v != nil {
		dec := v.(*zstd.Decoder)
		if err := dec.Reset(src); err == nil {
			return dec, nil
		}
		dec.Close()
	}
	return zstd.NewReader(src)
}

// releaseDecoder detaches the source and returns the decoder to p's pool.
// Reset(nil) keeps the 8 MB internal window and match tables allocated —
// same caveat as releaseEncoder regarding an orphaned p.
func (c *CompressingStorage) releaseDecoder(p *pools, dec *zstd.Decoder) {
	if err := dec.Reset(nil); err != nil {
		dec.Close()
		return
	}
	p.dec.Put(dec)
}

// expectedHashFromKey extracts the hash-tail of key (the final path segment).
// The spec addresses CompressingStorage exclusively from the `objects/{hash}`
// keyspace; the basename is the expected xxhash of the plaintext.
func expectedHashFromKey(key string) (uint64, error) {
	base := path.Base(path.Clean(key))
	if base == "." || base == "/" || base == "" {
		return 0, fmt.Errorf("invalid key for content-addressed blob: %q", key)
	}
	if len(base) != 16 {
		return 0, fmt.Errorf("invalid xxhash length in key %q: want 16 hex chars, got %d", key, len(base))
	}
	sum, err := strconv.ParseUint(base, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid xxhash in key %q: %w", key, err)
	}
	return sum, nil
}

// integrityReadCloser streams plaintext out of zstd while hashing bytes as they
// pass. On Close it compares the accumulated xxhash against the expected value
// and surfaces a mismatch as an error — callers should treat it as a fetch
// failure and retry / re-fetch upstream. release returns the decoder to its
// pool exactly once.
type integrityReadCloser struct {
	inner    io.ReadCloser
	decoder  *zstd.Decoder
	expected uint64
	key      string
	hasher   *xxhash.Digest
	release  func()
	eof      bool
	closed   bool
}

func (r *integrityReadCloser) Read(p []byte) (int, error) {
	n, err := r.decoder.Read(p)
	if n > 0 {
		_, _ = r.hasher.Write(p[:n])
	}
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return n, err
}

func (r *integrityReadCloser) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.release != nil {
		r.release()
	} else {
		r.decoder.Close()
	}
	innerErr := r.inner.Close()
	if r.eof && r.hasher.Sum64() != r.expected {
		return fmt.Errorf("integrity mismatch for %s: want %016x got %016x", r.key, r.expected, r.hasher.Sum64())
	}
	return innerErr
}

var _ ports.StorageRepository = (*CompressingStorage)(nil)
