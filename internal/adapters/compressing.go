package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"ritual/internal/core/ports"
	"strconv"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/klauspost/compress/zstd"
)

// errV1OnCompressing is returned by the deprecated V1 methods. CompressingStorage
// is a pure V2 decorator; V1 callers must migrate before reaching it.
var errV1OnCompressing = errors.New("use V2 methods on CompressingStorage")

// bufferSize is the scratch-buffer size used by io.CopyBuffer on both hot paths.
const bufferSize = 64 * 1024

// CompressingStorage decorates a StorageRepository with zstd-3 compression on
// PutStream and zstd-decode + xxhash integrity check on GetStream. Designed
// for the `objects/{hash}` keyspace: the key's basename is the expected xxhash.
//
// Resource reuse follows the POC r2sim pattern. Push: one encoder serialised
// by encMu, Reset(sink) per call. Pull: a sync.Pool of decoders, each Reset
// per call and returned on body Close — parallel Gets run without contention
// and without per-call 1 MB decoder allocation.
type CompressingStorage struct {
	inner   ports.StorageRepository
	enc     *zstd.Encoder
	encMu   sync.Mutex
	bufPool sync.Pool
	decPool sync.Pool
}

// NewCompressingStorage builds a V2 decorator around inner. One zstd encoder
// is held for the decorator's lifetime and reset per PutStream call (serialised
// by encMu).
func NewCompressingStorage(inner ports.StorageRepository) (*CompressingStorage, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("failed to build zstd encoder: %w", err)
	}
	c := &CompressingStorage{inner: inner, enc: enc}
	c.bufPool.New = func() any {
		b := make([]byte, bufferSize)
		return &b
	}
	return c, nil
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

// PutStream consumes body, zstd-compresses it into a memory-resident buffer,
// then hands that buffer to inner.PutStream. The decorator's single encoder
// is reset per call under encMu; io.CopyBuffer uses a pooled 64 KB scratch.
func (c *CompressingStorage) PutStream(ctx context.Context, key string, body io.ReadSeeker) error {
	var sink bytes.Buffer
	c.encMu.Lock()
	c.enc.Reset(&sink)
	bufPtr := c.bufPool.Get().(*[]byte)
	_, copyErr := io.CopyBuffer(c.enc, body, *bufPtr)
	c.bufPool.Put(bufPtr)
	closeErr := c.enc.Close()
	c.encMu.Unlock()
	if copyErr != nil {
		return fmt.Errorf("failed to compress %s: %w", key, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to finalize compressed %s: %w", key, closeErr)
	}
	return c.inner.PutStream(ctx, key, bytes.NewReader(sink.Bytes()))
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
	dec, err := c.acquireDecoder(body)
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
		release:  func() { c.releaseDecoder(dec) },
	}, nil
}

// acquireDecoder returns a ready-to-use zstd.Decoder bound to src. Reuses a
// pooled decoder when available; otherwise creates a fresh one. Failure to
// Reset a pooled decoder falls back to a fresh decoder so one bad instance
// can't poison the pool.
func (c *CompressingStorage) acquireDecoder(src io.Reader) (*zstd.Decoder, error) {
	if v := c.decPool.Get(); v != nil {
		dec := v.(*zstd.Decoder)
		if err := dec.Reset(src); err == nil {
			return dec, nil
		}
		dec.Close()
	}
	return zstd.NewReader(src)
}

// releaseDecoder detaches the source and returns the decoder to the pool.
// Reset(nil) keeps the ~1 MB internal window and match tables allocated.
func (c *CompressingStorage) releaseDecoder(dec *zstd.Decoder) {
	if err := dec.Reset(nil); err != nil {
		dec.Close()
		return
	}
	c.decPool.Put(dec)
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
