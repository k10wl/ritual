package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classifyAll treats every non-EOF error as transient. Useful for tests
// where the classifier is not the unit under test.
func classifyAll(err error) bool { return err != nil && !errors.Is(err, io.EOF) }

// classifyEOFOnly retries only on io.ErrUnexpectedEOF — the production
// shape for the R2 mid-stream-blip class.
func classifyEOFOnly(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }

// noSleep skips backoff entirely so tests stay subsecond.
func noSleep(ctx context.Context, _ time.Duration) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// tightPolicy is the canonical small budget used across most stories.
func tightPolicy(classify func(error) bool) RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
		Classify:    classify,
		Sleep:       noSleep,
	}
}

// flakyOpener replays a fixed payload but injects err once at a configured
// byte offset on the *initial* body. Subsequent re-opens (at offset > 0)
// deliver clean data. Models the R2 mid-stream-EOF failure shape.
type flakyOpener struct {
	payload   []byte
	failAt    int64
	failErr   error
	opens     int
	delivered int64
}

func (f *flakyOpener) open(_ context.Context, offset int64) (io.ReadCloser, error) {
	f.opens++
	if offset == 0 && f.failErr != nil {
		// first attempt: short read that EOFs at failAt
		return io.NopCloser(&truncatingReader{
			src:    bytes.NewReader(f.payload),
			budget: f.failAt,
			err:    f.failErr,
		}), nil
	}
	return io.NopCloser(bytes.NewReader(f.payload[offset:])), nil
}

// truncatingReader delivers src bytes up to budget then returns err.
type truncatingReader struct {
	src    io.Reader
	budget int64
	err    error
	served int64
}

func (t *truncatingReader) Read(p []byte) (int, error) {
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

func TestResumable_ResumesFromOffsetOnTransientShortRead(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefgh"), 1024)
	f := &flakyOpener{payload: payload, failAt: 100, failErr: io.ErrUnexpectedEOF}
	r, err := NewResumable(t.Context(), tightPolicy(classifyEOFOnly), f.open)
	require.NoError(t, err, "initial open must succeed")

	got, err := io.ReadAll(r)
	require.NoError(t, err, "transient mid-stream EOF must be hidden from caller")
	require.NoError(t, r.Close())

	assert.Equal(t, payload, got, "resumed stream must reconstruct the full payload byte-equal")
	assert.GreaterOrEqual(t, f.opens, 2, "opener must be invoked again after the short read")
}

func TestResumable_PassesTerminalErrorThrough(t *testing.T) {
	terminal := errors.New("permission denied")
	payload := bytes.Repeat([]byte("x"), 256)
	f := &flakyOpener{payload: payload, failAt: 50, failErr: terminal}
	r, err := NewResumable(t.Context(), tightPolicy(classifyEOFOnly), f.open)
	require.NoError(t, err)

	_, readErr := io.ReadAll(r)
	require.ErrorIs(t, readErr, terminal, "non-classified error surfaces unchanged")
	require.NoError(t, r.Close())

	assert.Equal(t, 1, f.opens, "no retry attempts may be spent on terminal errors")
}

func TestResumable_HonorsMaxAttempts(t *testing.T) {
	// Source always EOFs at byte 10, regardless of offset.
	opens := 0
	open := Opener(func(_ context.Context, _ int64) (io.ReadCloser, error) {
		opens++
		return io.NopCloser(&truncatingReader{src: bytes.NewReader(bytes.Repeat([]byte("y"), 100)), budget: 10, err: io.ErrUnexpectedEOF}), nil
	})

	r, err := NewResumable(t.Context(), tightPolicy(classifyEOFOnly), open)
	require.NoError(t, err)

	_, readErr := io.ReadAll(r)
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF, "exhausted budget surfaces last classified error")
	require.NoError(t, r.Close())

	assert.Equal(t, 3, opens, "MaxAttempts=3 ⇒ exactly 3 opens before surfacing the final error")
}

func TestResumable_RespectsContextCancellationBetweenAttempts(t *testing.T) {
	payload := bytes.Repeat([]byte("z"), 256)
	f := &flakyOpener{payload: payload, failAt: 50, failErr: io.ErrUnexpectedEOF}
	ctx, cancel := context.WithCancel(t.Context())
	policy := tightPolicy(classifyEOFOnly)
	policy.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	r, err := NewResumable(ctx, policy, f.open)
	require.NoError(t, err)

	_, readErr := io.ReadAll(r)
	require.ErrorIs(t, readErr, context.Canceled, "ctx cancellation between attempts must surface promptly")
	require.NoError(t, r.Close())
}

func TestResumable_PreservesPartialBytesBeforeRetry(t *testing.T) {
	// Body returns (5 bytes, ErrUnexpectedEOF) atomically — Resumable must
	// deliver the 5 bytes upstream, defer the retry to the next Read.
	payload := []byte("abcdefghij")
	bodyOnce := false
	open := Opener(func(_ context.Context, offset int64) (io.ReadCloser, error) {
		if !bodyOnce {
			bodyOnce = true
			return io.NopCloser(&atomicShortBody{data: payload[:5], tail: io.ErrUnexpectedEOF}), nil
		}
		return io.NopCloser(bytes.NewReader(payload[offset:])), nil
	})

	r, err := NewResumable(t.Context(), tightPolicy(classifyEOFOnly), open)
	require.NoError(t, err)
	got, readErr := io.ReadAll(r)
	require.NoError(t, readErr, "partial bytes + transient error must not surface as error")
	require.NoError(t, r.Close())
	assert.Equal(t, payload, got, "resumed stream must include the 5 partial bytes delivered atomically with the error")
}

// atomicShortBody returns data + a non-nil error on the same Read call,
// then EOF on the next. Models the (n>0, ErrUnexpectedEOF) pattern that
// io.Reader allows but most code paths poorly handle.
type atomicShortBody struct {
	data []byte
	tail error
	done bool
}

func (a *atomicShortBody) Read(p []byte) (int, error) {
	if a.done {
		return 0, io.EOF
	}
	a.done = true
	n := copy(p, a.data)
	return n, a.tail
}

func TestResumable_OnRetryReportsAttemptAndOffset(t *testing.T) {
	payload := bytes.Repeat([]byte("p"), 200)
	f := &flakyOpener{payload: payload, failAt: 80, failErr: io.ErrUnexpectedEOF}
	policy := tightPolicy(classifyEOFOnly)
	var seenAttempts []int
	var seenOffsets []int64
	policy.OnRetry = func(attempt int, offset int64, _ error) {
		seenAttempts = append(seenAttempts, attempt)
		seenOffsets = append(seenOffsets, offset)
	}

	r, err := NewResumable(t.Context(), policy, f.open)
	require.NoError(t, err)
	_, _ = io.ReadAll(r)
	require.NoError(t, r.Close())

	require.Equal(t, []int{1}, seenAttempts, "exactly one retry attempt observed for one transient blip")
	require.Equal(t, []int64{80}, seenOffsets, "OnRetry reports the byte offset where the source died")
}
