package adapters

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"ritual/internal/adapters/observed"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCompressingOnFS(t *testing.T) (*CompressingStorage, *FSRepository) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	fs, err := NewFSRepository(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })
	dec, err := NewCompressingStorage(fs)
	require.NoError(t, err)
	return dec, fs
}

func keyFor(payload []byte) string {
	return fmt.Sprintf("objects/%016x", xxhash.Sum64(payload))
}

func TestCompressingStorage_Roundtrip(t *testing.T) {
	dec, _ := newCompressingOnFS(t)
	payload := []byte("roundtrip payload that should compress and then decompress identically")
	key := keyFor(payload)

	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	rc, err := dec.GetStream(t.Context(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close(), "integrity check on Close")
	assert.Equal(t, payload, got, "decompressed stream equals original")
}

func TestCompressingStorage_IntegrityMismatch(t *testing.T) {
	dec, fs := newCompressingOnFS(t)
	payload := []byte("original bytes for integrity test")
	key := keyFor(payload)

	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	tamperedKey := fmt.Sprintf("objects/%016x", uint64(0xdeadbeefcafef00d))
	rc, err := fs.GetStream(t.Context(), key)
	require.NoError(t, err)
	compressed, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.NoError(t, fs.PutStream(t.Context(), tamperedKey, bytes.NewReader(compressed)))

	stream, err := dec.GetStream(t.Context(), tamperedKey)
	require.NoError(t, err, "GetStream opens the stream even when hash will mismatch")
	_, err = io.Copy(io.Discard, stream)
	require.NoError(t, err, "decompression succeeds; mismatch surfaces only at Close")
	closeErr := stream.Close()
	require.Error(t, closeErr, "Close must surface integrity mismatch")
	assert.True(t, strings.Contains(closeErr.Error(), "integrity mismatch"), "error mentions mismatch: %v", closeErr)
}

func TestCompressingStorage_TinyBlob(t *testing.T) {
	dec, fs := newCompressingOnFS(t)
	payload := []byte("abcd")
	key := keyFor(payload)

	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	raw, err := fs.GetStream(t.Context(), key)
	require.NoError(t, err)
	stored, err := io.ReadAll(raw)
	require.NoError(t, err)
	_ = raw.Close()
	require.GreaterOrEqual(t, len(stored), 4, "even tiny blobs become zstd frames (frame header cost)")
	assert.NotEqual(t, payload, stored, "stored bytes are a zstd frame, not raw input")

	rc, err := dec.GetStream(t.Context(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "tiny blob roundtrips byte-equal")
}

func TestCompressingStorage_LargeFileCompresses(t *testing.T) {
	dec, fs := newCompressingOnFS(t)
	payload := make([]byte, 10*1024*1024)
	chunk := []byte("highly-compressible-repetitive-content-block-")
	for i := 0; i < len(payload); i++ {
		payload[i] = chunk[i%len(chunk)]
	}
	key := keyFor(payload)

	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	raw, err := fs.GetStream(t.Context(), key)
	require.NoError(t, err)
	stored, err := io.ReadAll(raw)
	require.NoError(t, err)
	_ = raw.Close()
	require.Less(t, len(stored), len(payload)/2, "10 MB repetitive payload must compress below 5 MB (got %d bytes)", len(stored))

	rc, err := dec.GetStream(t.Context(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "large file roundtrips byte-equal")
}

func TestCompressingStorage_V1MethodsError(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	_, err := dec.Get(t.Context(), "k")
	assert.ErrorIs(t, err, errV1OnCompressing)
	assert.Error(t, dec.Put(t.Context(), "k", []byte("x")), "Put must error")
	assert.Error(t, dec.Rename(t.Context(), "a", "b"), "Rename must error")
}

func TestCompressingStorage_PassthroughOps(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	payload := []byte("passthrough-data")
	key := keyFor(payload)
	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	hit, err := dec.Exists(t.Context(), key)
	require.NoError(t, err)
	assert.True(t, hit, "Exists passthrough sees inner blob")

	keys, err := dec.List(t.Context(), "objects")
	require.NoError(t, err)
	assert.NotEmpty(t, keys, "List passthrough returns inner keys")

	require.NoError(t, dec.Delete(t.Context(), key))
	hit, err = dec.Exists(t.Context(), key)
	require.NoError(t, err)
	assert.False(t, hit, "Delete passthrough removes inner blob")
}

func TestCompressingStorage_InvalidKey(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	_, err := dec.GetStream(t.Context(), "objects/not-hex")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "xxhash"), "error mentions xxhash parse failure: %v", err)
}

func TestCompressingStorage_Concurrent(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	const workers = 8
	const perWorker = 6
	payloads := make([][]byte, workers*perWorker)
	keys := make([]string, len(payloads))
	for i := range payloads {
		p := make([]byte, 4096+i*17)
		for j := range p {
			p[j] = byte(i + j)
		}
		payloads[i] = p
		keys[i] = keyFor(p)
	}

	errs := make(chan error, len(payloads))
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for k := range perWorker {
				idx := base*perWorker + k
				if err := dec.PutStream(t.Context(), keys[idx], bytes.NewReader(payloads[idx])); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "parallel PutStream must not corrupt shared encoder")
	}

	for i, want := range payloads {
		rc, err := dec.GetStream(t.Context(), keys[i])
		require.NoError(t, err)
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close(), "integrity must hold for every concurrently-written blob")
		assert.Equal(t, want, got, "blob %d roundtrip mismatch — encoder state leaked across goroutines", i)
	}
}

func TestCompressingStorage_ChainedWithObserved(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	fs, err := NewFSRepository(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	bus := NewEventBus(32)
	ch, cancel := bus.Subscribe()
	defer cancel()

	compressed, err := NewCompressingStorage(fs)
	require.NoError(t, err)
	observedLayer := observed.NewStorage(compressed, bus)

	payload := bytes.Repeat([]byte("chained-decorator-line\n"), 256)
	key := keyFor(payload)

	require.NoError(t, observedLayer.PutStream(t.Context(), key, bytes.NewReader(payload)))

	rc, err := observedLayer.GetStream(t.Context(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "FS→Compressing→observed stack roundtrips byte-equal")

	hit, err := observedLayer.Exists(t.Context(), key)
	require.NoError(t, err)
	assert.True(t, hit, "Exists must see the blob through both decorators")

	seen := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
drain:
	for {
		select {
		case evt := <-ch:
			seen[fmt.Sprintf("%T", evt)] = true
		case <-timeout:
			break drain
		}
	}
	assert.True(t, seen["observed.StoragePutStreamInfo"], "PutStream event must reach the bus through the stack")
	assert.True(t, seen["observed.StorageGetStreamInfo"], "GetStream event must fire after body Close")
	assert.True(t, seen["observed.StorageExistsInfo"], "Exists event must fire")
}

// TestCompressingStorage_DecoderPoolReuse pins the invariant that GetStream
// reuses pooled zstd.Decoders. It measures average allocations per call over
// many iterations; a regression that dropped sync.Pool and went back to
// zstd.NewReader per call would blow this ceiling because each fresh decoder
// allocates its ~1 MB window and internal buffers.
//
// The ceiling is intentionally loose — the test guards against the class of
// bug (per-call decoder allocation), not an exact count.
func TestCompressingStorage_DecoderPoolReuse(t *testing.T) {
	dec, _ := newCompressingOnFS(t)
	payload := bytes.Repeat([]byte("pool-reuse-probe-"), 64)
	key := keyFor(payload)
	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))

	rc, err := dec.GetStream(t.Context(), key)
	require.NoError(t, err)
	_, _ = io.ReadAll(rc)
	require.NoError(t, rc.Close())

	avgAllocs := testing.AllocsPerRun(50, func() {
		rc, err := dec.GetStream(t.Context(), key)
		if err != nil {
			t.Fatalf("GetStream: %v", err)
		}
		if _, err := io.ReadAll(rc); err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	const ceiling = 50.0
	assert.Less(t, avgAllocs, ceiling,
		"allocs/call=%.1f exceeded ceiling=%.0f — decoder pool likely regressed to per-call zstd.NewReader",
		avgAllocs, ceiling)
	t.Logf("GetStream steady-state allocs/call=%.1f (ceiling=%.0f)", avgAllocs, ceiling)
}

// TestCompressingStorage_ReleaseThenReuse pins that Release doesn't corrupt
// in-flight or subsequent operations: a decoder/encoder acquired before
// Release must still finish correctly, and the next call after Release must
// still round-trip (it just can't reuse the discarded pool — it lazily
// builds fresh instances, per NewCompressingStorage's doc).
func TestCompressingStorage_ReleaseThenReuse(t *testing.T) {
	cs, _ := newCompressingOnFS(t)
	payload := bytes.Repeat([]byte("release-then-reuse-probe-"), 64)
	key := keyFor(payload)
	require.NoError(t, cs.PutStream(t.Context(), key, bytes.NewReader(payload)))

	rc, err := cs.GetStream(t.Context(), key)
	require.NoError(t, err)

	// Release while rc is still open and unread — the in-flight decoder was
	// acquired from the pool generation now being discarded; it must still
	// finish this read correctly.
	cs.Release()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got)

	// A subsequent call must still round-trip against the fresh generation.
	payload2 := bytes.Repeat([]byte("post-release-probe-"), 64)
	key2 := keyFor(payload2)
	require.NoError(t, cs.PutStream(t.Context(), key2, bytes.NewReader(payload2)))
	rc2, err := cs.GetStream(t.Context(), key2)
	require.NoError(t, err)
	got2, err := io.ReadAll(rc2)
	require.NoError(t, err)
	require.NoError(t, rc2.Close())
	assert.Equal(t, payload2, got2)
}

func TestCompressingStorage_ConcurrentPull(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	const blobs = 24
	payloads := make([][]byte, blobs)
	keys := make([]string, blobs)
	for i := range payloads {
		p := make([]byte, 2048+i*53)
		for j := range p {
			p[j] = byte((i*7 + j) & 0xff)
		}
		payloads[i] = p
		keys[i] = keyFor(p)
		require.NoError(t, dec.PutStream(t.Context(), keys[i], bytes.NewReader(p)))
	}

	const workers = 8
	errs := make(chan error, blobs*workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := range blobs {
				idx := (base + i) % blobs
				rc, err := dec.GetStream(t.Context(), keys[idx])
				if err != nil {
					errs <- fmt.Errorf("get %d: %w", idx, err)
					return
				}
				got, readErr := io.ReadAll(rc)
				closeErr := rc.Close()
				if readErr != nil {
					errs <- fmt.Errorf("read %d: %w", idx, readErr)
					return
				}
				if closeErr != nil {
					errs <- fmt.Errorf("close %d (integrity): %w", idx, closeErr)
					return
				}
				if !bytes.Equal(got, payloads[idx]) {
					errs <- fmt.Errorf("blob %d mismatch — pooled decoder leaked state across goroutines", idx)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "parallel GetStream must not corrupt pooled decoders")
	}
}

func TestCompressingStorage_ConcurrentPushPull(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	const seed = 12
	seeded := make([][]byte, seed)
	seededKeys := make([]string, seed)
	for i := range seeded {
		p := bytes.Repeat([]byte(fmt.Sprintf("seed-%02d-", i)), 200)
		seeded[i] = p
		seededKeys[i] = keyFor(p)
		require.NoError(t, dec.PutStream(t.Context(), seededKeys[i], bytes.NewReader(p)))
	}

	const pushers, pullers, perWorker = 4, 4, 6
	errs := make(chan error, (pushers+pullers)*perWorker)
	var wg sync.WaitGroup

	for w := range pushers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := range perWorker {
				p := bytes.Repeat([]byte(fmt.Sprintf("push-%d-%d-", base, i)), 150)
				k := keyFor(p)
				if err := dec.PutStream(t.Context(), k, bytes.NewReader(p)); err != nil {
					errs <- fmt.Errorf("push %d-%d: %w", base, i, err)
					return
				}
				rc, err := dec.GetStream(t.Context(), k)
				if err != nil {
					errs <- fmt.Errorf("readback %d-%d open: %w", base, i, err)
					return
				}
				got, _ := io.ReadAll(rc)
				if closeErr := rc.Close(); closeErr != nil {
					errs <- fmt.Errorf("readback %d-%d close: %w", base, i, closeErr)
					return
				}
				if !bytes.Equal(got, p) {
					errs <- fmt.Errorf("readback %d-%d mismatch", base, i)
					return
				}
			}
		}(w)
	}

	for w := range pullers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := range perWorker {
				idx := (base*perWorker + i) % seed
				rc, err := dec.GetStream(t.Context(), seededKeys[idx])
				if err != nil {
					errs <- fmt.Errorf("pull %d-%d: %w", base, i, err)
					return
				}
				got, _ := io.ReadAll(rc)
				if closeErr := rc.Close(); closeErr != nil {
					errs <- fmt.Errorf("pull %d-%d close: %w", base, i, closeErr)
					return
				}
				if !bytes.Equal(got, seeded[idx]) {
					errs <- fmt.Errorf("pull %d-%d mismatch — encoder/decoder resources mixed up under load", base, i)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "mixed push/pull concurrency must leave encoder + decoder pools independent")
	}
}

// TestCompressingStorage_EncoderPoolNoSerialization pins the invariant that
// concurrent PutStream calls run in parallel — no shared encoder lock. The
// regression class it guards: replacing the encoder pool with a single
// encoder + mutex would force N slow bodies to drain sequentially, multiplying
// wall time by N. The test injects per-blob read latency D and asserts the
// total wall time stays well under N*D.
func TestCompressingStorage_EncoderPoolNoSerialization(t *testing.T) {
	dec, _ := newCompressingOnFS(t)

	const N = 10
	const D = 50 * time.Millisecond
	payloads := make([][]byte, N)
	for i := range payloads {
		p := make([]byte, 4096+i*7)
		for j := range p {
			p[j] = byte(i + j)
		}
		payloads[i] = p
	}

	start := time.Now()
	errs := make(chan error, N)
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := &slowReader{src: bytes.NewReader(payloads[i]), delay: D}
			errs <- dec.PutStream(t.Context(), keyFor(payloads[i]), body)
		}(i)
	}
	wg.Wait()
	close(errs)
	elapsed := time.Since(start)

	for err := range errs {
		require.NoError(t, err, "concurrent PutStream must not fail")
	}

	// Discriminate concurrent (~D, sleeps overlap) from serialized (N*D, sleeps
	// run back-to-back under an encoder lock). Ceiling fixed at 2s rather than
	// derived from N*D: shared CI runners (goroutines queued behind other
	// packages' concurrent `go test ./...` load, ~15ms Windows timer granularity)
	// were observed exceeding the earlier 1s ceiling with no lock involved
	// (atomic.Pointer[pools] swap, no shared mutex across PutStream calls — see
	// compressing.go), so N*D-derived ceilings aren't a reliable serialization
	// signal on noisy hardware. 2s still catches catastrophic serialization while
	// giving contention-only slowdowns enough room.
	serialized := time.Duration(N) * D
	ceiling := 2 * time.Second
	assert.Less(t, elapsed, ceiling,
		"wall=%s exceeded ceiling=%s for N=%d concurrent slow-body pushes — encoder lock likely regressed (serialized N*D=%s)",
		elapsed, ceiling, N, serialized)

	for i, want := range payloads {
		rc, err := dec.GetStream(t.Context(), keyFor(want))
		require.NoError(t, err)
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close(), "integrity must hold for blob %d", i)
		assert.Equal(t, want, got, "blob %d roundtrip mismatch — pooled encoders leaked state across goroutines", i)
	}
}

// slowReader emits its source bytes only after a fixed delay on the first Read.
// Models a slow upstream body (e.g. an R2 GetStream that meters bytes) without
// fighting the io.CopyBuffer chunk size.
type slowReader struct {
	src   io.Reader
	delay time.Duration
	done  bool
}

func (s *slowReader) Read(p []byte) (int, error) {
	if !s.done {
		time.Sleep(s.delay)
		s.done = true
	}
	return s.src.Read(p)
}

func TestCompressingStorage_RandomPayloadRoundtrip(t *testing.T) {
	dec, _ := newCompressingOnFS(t)
	payload := make([]byte, 128*1024)
	_, err := rand.Read(payload)
	require.NoError(t, err)
	key := keyFor(payload)

	require.NoError(t, dec.PutStream(t.Context(), key, bytes.NewReader(payload)))
	rc, err := dec.GetStream(t.Context(), key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "random 128 KB payload survives zstd roundtrip byte-equal")
}
