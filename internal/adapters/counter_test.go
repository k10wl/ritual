package adapters

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mocks "ritual/internal/core/ports/mocks"
)

func TestCounterStorage_GetStream_BytesAdvanceDuringRead(t *testing.T) {
	inner := &mocks.MockStorageRepository{}
	payload := bytes.Repeat([]byte("A"), 4096)
	inner.GetStreamFunc = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	rc, err := c.GetStream(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, int64(0), counters.BytesIn.Load(), "no bytes counted before Read")

	buf := make([]byte, 512)
	n, err := rc.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 512, n)
	assert.Equal(t, int64(512), counters.BytesIn.Load(), "counter advances mid-stream, not only on Close")

	drained, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, len(payload)-512, len(drained))
	assert.Equal(t, int64(len(payload)), counters.BytesIn.Load(), "counter reflects full body after drain")

	require.NoError(t, rc.Close())
	assert.Equal(t, int64(1), counters.OpsComplete.Load(), "Close finalises op")
	assert.Equal(t, int64(0), counters.OpsFailed.Load())
}

func TestCounterStorage_GetStream_OpenFailure(t *testing.T) {
	inner := &mocks.MockStorageRepository{}
	inner.GetStreamFunc = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return nil, errors.New("boom")
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	_, err := c.GetStream(t.Context(), "k")
	require.Error(t, err)
	assert.Equal(t, int64(0), counters.BytesIn.Load(), "open-error counts no bytes")
	assert.Equal(t, int64(1), counters.OpsComplete.Load())
	assert.Equal(t, int64(1), counters.OpsFailed.Load())
}

func TestCounterStorage_PutStream_BytesAdvanceDuringWrite(t *testing.T) {
	payload := bytes.Repeat([]byte("B"), 2048)
	inner := &mocks.MockStorageRepository{}
	inner.PutStreamFunc = func(_ context.Context, _ string, body io.Reader) error {
		tmp := make([]byte, 256)
		n, err := body.Read(tmp)
		if err != nil {
			return err
		}
		if n != 256 {
			return errors.New("partial read")
		}
		_, err = io.Copy(io.Discard, body)
		return err
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	require.NoError(t, c.PutStream(t.Context(), "k", bytes.NewReader(payload)))
	assert.Equal(t, int64(len(payload)), counters.BytesOut.Load(), "BytesOut equals bytes inner consumed")
	assert.Equal(t, int64(1), counters.OpsComplete.Load())
	assert.Equal(t, int64(0), counters.OpsFailed.Load())
}

func TestCounterStorage_PutStream_RetryDoubleCounts(t *testing.T) {
	payload := []byte("retry-bytes")
	inner := &mocks.MockStorageRepository{}
	attempt := 0
	inner.PutStreamFunc = func(_ context.Context, _ string, body io.Reader) error {
		attempt++
		_, _ = io.Copy(io.Discard, body)
		if attempt == 1 {
			seeker, ok := body.(io.Seeker)
			require.True(t, ok, "counter wrapper must expose Seek when inner body is seekable — retry rewind path must survive the tap")
			_, _ = seeker.Seek(0, io.SeekStart)
			_, _ = io.Copy(io.Discard, body)
		}
		return nil
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	require.NoError(t, c.PutStream(t.Context(), "k", bytes.NewReader(payload)))
	assert.Equal(t, int64(len(payload)*2), counters.BytesOut.Load(),
		"retry-re-read counts again — BytesOut is wire-level, matches POC r2sim convention")
}

func TestCounterStorage_Exists_NoByteCount(t *testing.T) {
	inner := &mocks.MockStorageRepository{
		ExistsFunc: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	hit, err := c.Exists(t.Context(), "k")
	require.NoError(t, err)
	require.True(t, hit)
	assert.Equal(t, int64(0), counters.BytesIn.Load())
	assert.Equal(t, int64(0), counters.BytesOut.Load())
	assert.Equal(t, int64(1), counters.OpsComplete.Load())
}

func TestCounterStorage_Concurrent(t *testing.T) {
	const workers = 8
	const perWorker = 16
	payload := bytes.Repeat([]byte("X"), 1024)
	inner := &mocks.MockStorageRepository{
		PutStreamFunc: func(_ context.Context, _ string, body io.Reader) error {
			_, _ = io.Copy(io.Discard, body)
			return nil
		},
	}
	counters := &StorageCounters{}
	c := NewCounterStorage(inner, counters)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_ = c.PutStream(t.Context(), "k", bytes.NewReader(payload))
			}
		}()
	}
	wg.Wait()

	want := int64(workers * perWorker * len(payload))
	assert.Equal(t, want, counters.BytesOut.Load(), "atomic increments must not race under concurrent writers")
	assert.Equal(t, int64(workers*perWorker), counters.OpsComplete.Load())
}
