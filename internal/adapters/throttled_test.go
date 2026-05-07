package adapters

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	mocks "ritual/internal/core/ports/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThrottledStorage_BurstClampedToMinimum_64KiB(t *testing.T) {
	tr := NewThrottledStorage(&mocks.MockStorageRepository{}, 10_000)
	assert.GreaterOrEqual(t, tr.limiter.Burst(), 64*1024, "burst capacity must be clamped to ≥ 64 KiB so a typical io.Copy 32 KiB chunk never exceeds the bucket and stalls indefinitely under low bytesPerSec settings")
}

func TestThrottledStorage_MetadataOpsPassThrough_NotRateLimited(t *testing.T) {
	inner := &mocks.MockStorageRepository{}
	existsCalled, listCalled, deleteCalled, deleteBatchCalled, copyCalled := 0, 0, 0, 0, 0
	inner.ExistsFunc = func(_ context.Context, _ string) (bool, error) { existsCalled++; return true, nil }
	inner.ListFunc = func(_ context.Context, _ string) ([]string, error) { listCalled++; return nil, nil }
	inner.DeleteFunc = func(_ context.Context, _ string) error { deleteCalled++; return nil }
	inner.DeleteBatchFunc = func(_ context.Context, _ []string) error { deleteBatchCalled++; return nil }
	inner.CopyFunc = func(_ context.Context, _, _ string) error { copyCalled++; return nil }

	tr := NewThrottledStorage(inner, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := tr.Exists(ctx, "k")
	require.NoError(t, err)
	_, err = tr.List(ctx, "")
	require.NoError(t, err)
	require.NoError(t, tr.Delete(ctx, "k"))
	require.NoError(t, tr.DeleteBatch(ctx, []string{"k"}))
	require.NoError(t, tr.Copy(ctx, "a", "b"))

	assert.Equal(t, 1, existsCalled, "Exists must reach the inner storage so HEAD-style probes finish in O(1) regardless of the byte-rate budget — metadata ops are not network-streamed")
	assert.Equal(t, 1, listCalled, "List must reach the inner storage — directory enumeration is metadata, not bytes, and must not consume the throttle bucket")
	assert.Equal(t, 1, deleteCalled, "Delete must reach the inner storage — refs cleanup must not stall waiting for the byte-rate bucket")
	assert.Equal(t, 1, deleteBatchCalled, "DeleteBatch must reach the inner storage — batch tombstoning is a single metadata round-trip, not N bytes")
	assert.Equal(t, 1, copyCalled, "Copy must reach the inner storage — server-side copy moves no bytes through the client and must not be throttled")
}

func TestThrottledStorage_PutStream_LimitsByteThroughputToConfiguredRate(t *testing.T) {
	inner := &mocks.MockStorageRepository{}
	var captured bytes.Buffer
	inner.PutStreamFunc = func(_ context.Context, _ string, body io.Reader) error {
		_, err := io.Copy(&captured, body)
		return err
	}

	const bytesPerSec = 256 * 1024
	const payloadSize = 192 * 1024
	tr := NewThrottledStorage(inner, bytesPerSec)

	ctx, cancel := context.WithTimeout(t.Context(), 950*time.Millisecond)
	defer cancel()

	payload := bytes.Repeat([]byte("x"), payloadSize)
	start := time.Now()
	err := tr.PutStream(ctx, "k", bytes.NewReader(payload))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, payloadSize, captured.Len(), "every byte handed to PutStream must reach the inner storage — throttling shapes timing, never drops bytes; a partial write would corrupt the uploaded object")

	burst := 64 * 1024
	expected := time.Duration(float64(payloadSize-burst) / float64(bytesPerSec) * float64(time.Second))
	assert.GreaterOrEqual(t, elapsed, expected/2, "PutStream must spend at least half the theoretical rate-limited duration draining the bucket so the dev loop reflects realistic upload pacing instead of native disk speed")
}
