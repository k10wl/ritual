package refs_test

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/adapters/stream"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPuller_RecoversFromMidStreamBodyEOFOnSingleBlob is the friction-to-test
// regression for design-log/004. It models the production failure where R2's
// GetStream body returned io.ErrUnexpectedEOF mid-read on one blob inside a
// fan-out — without retry, the entire pull failed and the user had to click
// Retry. With retrying::storage wrapping the remote, the blip heals
// transparently and Pull completes.
func TestPuller_RecoversFromMidStreamBodyEOFOnSingleBlob(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	remote := newFSBundle(t)
	local := newFSBundle(t)

	good := []byte("CLEAN")
	flaky := []byte("FLAKY_PAYLOAD_LONG_ENOUGH_FOR_PARTIAL_READ")
	ref := sampleRef("2026-04-22T10-00-00.000Z", map[string][]byte{
		"worlds/level.dat": good,
		"worlds/region/r": flaky,
	})
	seedRemote(t, remote, ref, map[string][]byte{
		"worlds/level.dat": good,
		"worlds/region/r": flaky,
	})

	flakyKey := "objects/" + hashHex(string(flaky))
	flakyRemote := newOneShotEOFRemote(remote.inner, flakyKey, 8)
	withRetry := adapters.NewRetryingStorage(flakyRemote, retryNowPolicy(), nil)

	puller := refs.NewPuller(withRetry, local.storage, serialRunner)
	err := puller.Pull(ctx, ref.Timestamp)

	require.NoError(t, err,
		"pull must complete despite one transient mid-stream EOF — retrying::storage must resume from the byte offset where the body died")
	assert.Equal(t, good, local.mustGet(t, "objects/"+hashHex(string(good))),
		"clean blob must land verbatim — failure on a sibling cannot poison its result")
	assert.Equal(t, flaky, local.mustGet(t, flakyKey),
		"flaky blob must reconstruct byte-equal — resume preserved the offset, no data drift")
	assert.Equal(t, 1, flakyRemote.failuresInjected(),
		"exactly one EOF must have been injected — fixture preconditions for the regression")
	assert.GreaterOrEqual(t, flakyRemote.opens(flakyKey), 2,
		"flaky blob must have been re-opened after the EOF — proves the retry path ran, not just a coincidence")
}

// retryNowPolicy is the tight in-test policy: 3 attempts, zero backoff, accept
// ErrUnexpectedEOF + io.EOF as transient (the production classifier is
// stricter; the integration test only needs to prove the resume path runs).
func retryNowPolicy() stream.RetryPolicy {
	return stream.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: time.Microsecond,
		MaxBackoff:  time.Microsecond,
		Classify:    adapters.DefaultRetryClassify,
		Sleep:       func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
}

// oneShotEOFRemote decorates a StorageRepository so that the first GetStream
// for failKey delivers `after` bytes then returns io.ErrUnexpectedEOF. All
// subsequent GetStreams for the same key (and every call for any other key)
// pass through unchanged.
type oneShotEOFRemote struct {
	inner    ports.StorageRepository
	failKey  string
	after    int64
	mu       sync.Mutex
	injected int
	openHits map[string]int
}

func newOneShotEOFRemote(inner ports.StorageRepository, failKey string, after int64) *oneShotEOFRemote {
	return &oneShotEOFRemote{inner: inner, failKey: failKey, after: after, openHits: map[string]int{}}
}

func (o *oneShotEOFRemote) failuresInjected() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.injected
}

func (o *oneShotEOFRemote) opens(key string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.openHits[key]
}

func (o *oneShotEOFRemote) String() string { return "oneShotEOF::" + o.inner.String() }

func (o *oneShotEOFRemote) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := o.inner.GetStream(ctx, key)
	if err != nil {
		return rc, err
	}
	o.mu.Lock()
	o.openHits[key]++
	shouldFail := key == o.failKey && o.injected == 0
	if shouldFail {
		o.injected++
	}
	o.mu.Unlock()
	if shouldFail {
		return &truncatingBody{inner: rc, budget: o.after, err: io.ErrUnexpectedEOF}, nil
	}
	return rc, nil
}

func (o *oneShotEOFRemote) PutStream(ctx context.Context, key string, body io.Reader) error {
	return o.inner.PutStream(ctx, key, body)
}

func (o *oneShotEOFRemote) Exists(ctx context.Context, key string) (bool, error) {
	return o.inner.Exists(ctx, key)
}

func (o *oneShotEOFRemote) Delete(ctx context.Context, key string) error {
	return o.inner.Delete(ctx, key)
}

func (o *oneShotEOFRemote) DeleteBatch(ctx context.Context, keys []string) error {
	return o.inner.DeleteBatch(ctx, keys)
}

func (o *oneShotEOFRemote) List(ctx context.Context, prefix string) ([]string, error) {
	return o.inner.List(ctx, prefix)
}

func (o *oneShotEOFRemote) Copy(ctx context.Context, src, dst string) error {
	return o.inner.Copy(ctx, src, dst)
}

// truncatingBody delivers up to budget bytes from inner, then returns err.
// Models an R2 body that EOFs mid-stream. Close forwards to inner.
type truncatingBody struct {
	inner  io.ReadCloser
	budget int64
	err    error
	served int64
}

func (t *truncatingBody) Read(p []byte) (int, error) {
	if t.served >= t.budget {
		return 0, t.err
	}
	remain := t.budget - t.served
	if int64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := t.inner.Read(p)
	t.served += int64(n)
	if err != nil {
		return n, err
	}
	if t.served >= t.budget {
		return n, t.err
	}
	return n, nil
}

func (t *truncatingBody) Close() error { return t.inner.Close() }

// keep bytes import used (silences linter when test grows in size)
var _ = bytes.NewReader
