package adapters

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"ritual/internal/adapters/stream"
	mocks "ritual/internal/core/ports/mocks"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPolicy(classify func(error) bool) stream.RetryPolicy {
	return stream.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
		Classify:    classify,
		Sleep:       func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
}

func TestRetryingStorage_GetStream_UsesRangeGetterWhenAvailable(t *testing.T) {
	payload := bytes.Repeat([]byte("range"), 200)
	inner := &fakeRanged{payload: payload, failAt: 120, failErr: io.ErrUnexpectedEOF}
	r := NewRetryingStorage(inner, testPolicy(stream.RetryPolicy{Classify: func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }}.Classify), nil)

	rc, err := r.GetStream(t.Context(), "objects/x")
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err, "transient mid-stream EOF must heal via RangeGetter")
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "ranged resume must reconstruct payload byte-equal")
	assert.GreaterOrEqual(t, inner.rangedOpens, int32(2), "RangeGetter path must be used to resume (≥2 opens)")
	assert.Equal(t, int32(0), inner.plainOpens, "plain GetStream must be bypassed when RangeGetter is available")
}

func TestRetryingStorage_GetStream_FallsBackToReopenAndSkip(t *testing.T) {
	payload := bytes.Repeat([]byte("plain"), 200)
	inner := &fakePlain{payload: payload, failAt: 80, failErr: io.ErrUnexpectedEOF}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	rc, err := r.GetStream(t.Context(), "objects/y")
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err, "range-blind inner must still resume via re-open + skip")
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "fallback path must reconstruct payload byte-equal")
}

func TestRetryingStorage_PutStream_RetriesSeekableBodyOnTransient(t *testing.T) {
	payload := []byte("seekable-body-bytes")
	var attempts int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		PutStreamFunc: func(_ context.Context, _ string, body io.Reader) error {
			n := atomic.AddInt32(&attempts, 1)
			buf, _ := io.ReadAll(body)
			if !bytes.Equal(buf, payload) {
				return errors.New("body was not rewound between attempts — got " + string(buf))
			}
			if n < 3 {
				return io.ErrUnexpectedEOF
			}
			return nil
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	err := r.PutStream(t.Context(), "objects/z", bytes.NewReader(payload))
	require.NoError(t, err, "seekable body must rewind and retry through to success")
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "exhausting two transient failures takes 3 attempts under MaxAttempts=3")
}

func TestRetryingStorage_PutStream_NonSeekableBodyAttemptsOnceOnly(t *testing.T) {
	var attempts int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		PutStreamFunc: func(_ context.Context, _ string, body io.Reader) error {
			atomic.AddInt32(&attempts, 1)
			_, _ = io.Copy(io.Discard, body)
			return io.ErrUnexpectedEOF
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	nonSeekable := io.NopCloser(bytes.NewReader([]byte("pipe")))
	err := r.PutStream(t.Context(), "objects/zz", nonSeekable)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "non-seekable upload must fail honestly without retry")
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "non-seekable body uploads exactly once — no replay possible")
}

func TestRetryingStorage_PutStream_TerminalErrorSurfacesImmediately(t *testing.T) {
	terminal := errors.New("4xx forbidden")
	var attempts int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		PutStreamFunc: func(_ context.Context, _ string, _ io.Reader) error {
			atomic.AddInt32(&attempts, 1)
			return terminal
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	err := r.PutStream(t.Context(), "objects/term", bytes.NewReader([]byte("body")))
	require.ErrorIs(t, err, terminal, "terminal err must not be retried")
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "exactly one attempt for terminal errors")
}

func TestRetryingStorage_Exists_RetriesAndRecovers(t *testing.T) {
	var calls int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		ExistsFunc: func(_ context.Context, _ string) (bool, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 2 {
				return false, io.ErrUnexpectedEOF
			}
			return true, nil
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	hit, err := r.Exists(t.Context(), "objects/e")
	require.NoError(t, err, "Exists must recover after one transient")
	assert.True(t, hit, "second attempt's return value must surface to caller")
}

func TestRetryingStorage_PublishesRetryEvent(t *testing.T) {
	inner := &fakeRanged{payload: []byte("xxxxxxxxxxxxxxxx"), failAt: 4, failErr: io.ErrUnexpectedEOF}
	bus := NewEventBus(8)
	ch, cancel := bus.Subscribe()
	defer cancel()
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), bus)

	rc, err := r.GetStream(t.Context(), "objects/observed")
	require.NoError(t, err)
	_, _ = io.ReadAll(rc)
	require.NoError(t, rc.Close())

	select {
	case evt := <-ch:
		info, ok := evt.(StorageRetryInfo)
		require.True(t, ok, "expected StorageRetryInfo, got %T", evt)
		assert.Equal(t, 1, info.Attempt)
		assert.Equal(t, 3, info.MaxAttempts)
		assert.Equal(t, int64(4), info.Offset, "Offset must point at the byte where the source died")
		assert.ErrorIs(t, info.Err, io.ErrUnexpectedEOF)
	case <-time.After(time.Second):
		t.Fatal("expected one storage.retry event, got none")
	}
}

func TestRetryingStorage_GetStream_RetriesInitialOpenOnTransient(t *testing.T) {
	payload := []byte("recovered-body")
	var opens int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			n := atomic.AddInt32(&opens, 1)
			if n < 2 {
				return nil, io.ErrUnexpectedEOF
			}
			return io.NopCloser(bytes.NewReader(payload)), nil
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	rc, err := r.GetStream(t.Context(), "objects/initial")
	require.NoError(t, err, "initial open must retry on transient — symmetric with PutStream's runWithRetry")
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, payload, got, "recovered body must reach the caller byte-equal")
	assert.Equal(t, int32(2), atomic.LoadInt32(&opens), "inner.GetStream must be invoked twice: once transient-failing, once succeeding")
}

func TestRetryingStorage_GetStream_InitialOpenTerminalSurfacesImmediately(t *testing.T) {
	terminal := errors.New("NoSuchKey")
	var opens int32
	inner := &mocks.MockStorageRepository{
		Label: "fake::backend",
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			atomic.AddInt32(&opens, 1)
			return nil, terminal
		},
	}
	r := NewRetryingStorage(inner, testPolicy(func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }), nil)

	_, err := r.GetStream(t.Context(), "objects/missing")
	require.ErrorIs(t, err, terminal, "terminal open errors must surface unchanged — no retry budget spent on 4xx-class")
	assert.Equal(t, int32(1), atomic.LoadInt32(&opens), "exactly one open attempt for terminal errors")
}

func TestDefaultRetryClassify_RetriesOnECONNRESET(t *testing.T) {
	err := &net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}
	assert.True(t, DefaultRetryClassify(err),
		"connection reset by peer is the canonical idle-edge-drop shape — must classify as transient or production keeps failing on ECONNRESET")
}

func TestDefaultRetryClassify_RetriesOnEPIPE(t *testing.T) {
	err := &net.OpError{Op: "write", Net: "tcp", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}}
	assert.True(t, DefaultRetryClassify(err),
		"broken pipe on write is symmetric with ECONNRESET on read — also retryable")
}

func TestDefaultRetryClassify_TerminalOnDNSError(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "missing.example.com", IsNotFound: true}
	assert.False(t, DefaultRetryClassify(dnsErr),
		"name-resolution failure is permanent — retrying it just wastes MaxAttempts × MaxBackoff and surfaces the same error N times")
}

func TestRetryingStorage_Label(t *testing.T) {
	inner := &mocks.MockStorageRepository{Label: "r2::ritual-dev"}
	r := NewRetryingStorage(inner, DefaultRetryPolicy(), nil)
	assert.Equal(t, "retrying::r2::ritual-dev", r.String(), "decorator must self-describe as retrying::<inner>")
}

// fakeRanged is a StorageRepository that satisfies RangeGetter. Its first
// ranged open at offset=0 returns a short body that EOFs at failAt; later
// opens at offset > 0 deliver clean tail bytes. Exposes counters for asserts.
type fakeRanged struct {
	mocks.MockStorageRepository
	payload     []byte
	failAt      int64
	failErr     error
	rangedOpens int32
	plainOpens  int32
}

func (f *fakeRanged) String() string { return "fake::ranged" }

func (f *fakeRanged) GetStream(_ context.Context, _ string) (io.ReadCloser, error) {
	atomic.AddInt32(&f.plainOpens, 1)
	return io.NopCloser(bytes.NewReader(f.payload)), nil
}

func (f *fakeRanged) GetStreamRange(_ context.Context, _ string, offset int64) (io.ReadCloser, error) {
	atomic.AddInt32(&f.rangedOpens, 1)
	if offset == 0 {
		return io.NopCloser(&truncatedAt{src: bytes.NewReader(f.payload), budget: f.failAt, err: f.failErr}), nil
	}
	return io.NopCloser(bytes.NewReader(f.payload[offset:])), nil
}

// fakePlain is range-blind: only GetStream. Forces the decorator into
// re-open + skip-prefix mode.
type fakePlain struct {
	mocks.MockStorageRepository
	payload []byte
	failAt  int64
	failErr error
	opens   int32
}

func (f *fakePlain) String() string { return "fake::plain" }

func (f *fakePlain) GetStream(_ context.Context, _ string) (io.ReadCloser, error) {
	n := atomic.AddInt32(&f.opens, 1)
	if n == 1 {
		return io.NopCloser(&truncatedAt{src: bytes.NewReader(f.payload), budget: f.failAt, err: f.failErr}), nil
	}
	return io.NopCloser(bytes.NewReader(f.payload)), nil
}

// truncatedAt delivers src bytes up to budget then returns err. Mirrors
// stream/resumable_test.go's truncatingReader; duplicated here to keep the
// stream package's test types unexported.
type truncatedAt struct {
	src    io.Reader
	budget int64
	err    error
	served int64
}

func (t *truncatedAt) Read(p []byte) (int, error) {
	if t.served >= t.budget {
		return 0, t.err
	}
	remain := t.budget - t.served
	if int64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := t.src.Read(p)
	t.served += int64(n)
	if err != nil {
		return n, err
	}
	if t.served >= t.budget {
		return n, t.err
	}
	return n, nil
}
