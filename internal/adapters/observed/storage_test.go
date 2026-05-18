package observed_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/ports"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mocks "ritual/internal/core/ports/mocks"
)

func setup(t *testing.T) (*mocks.MockStorageRepository, ports.EventBus, <-chan ports.Event, func()) {
	t.Helper()
	inner := &mocks.MockStorageRepository{Label: "mock::test"}
	bus := adapters.NewEventBus(16)
	ch, cancel := bus.Subscribe()
	return inner, bus, ch, cancel
}

func recvOne(t *testing.T, ch <-chan ports.Event) ports.Event {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(1 * time.Second):
		t.Fatal("expected event, got none")
	}
	return nil
}

func TestObservedStorage_Copy(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)
	require.NoError(t, s.Copy(t.Context(), "src", "dst"))

	evt := recvOne(t, ch).(observed.StorageCopyInfo)
	assert.Equal(t, "src", evt.SrcKey)
	assert.Equal(t, "dst", evt.DstKey)
}

func TestObservedStorage_Delete(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)
	require.NoError(t, s.Delete(t.Context(), "k"))

	evt := recvOne(t, ch).(observed.StorageDeleteInfo)
	assert.Equal(t, "k", evt.Key)
}

func TestObservedStorage_DeleteBatch(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)
	require.NoError(t, s.DeleteBatch(t.Context(), []string{"a", "b"}))

	evt := recvOne(t, ch).(observed.StorageDeleteBatchInfo)
	assert.Equal(t, []string{"a", "b"}, evt.Keys)
}

func TestObservedStorage_List(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	inner.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return []string{"x", "y", "z"}, nil
	}
	s := observed.NewStorage(inner, bus)

	keys, err := s.List(t.Context(), "p")
	require.NoError(t, err)
	require.Len(t, keys, 3)

	evt := recvOne(t, ch).(observed.StorageListInfo)
	assert.Equal(t, "p", evt.Prefix)
	assert.Equal(t, 3, evt.Count)
}

func TestObservedStorage_String(t *testing.T) {
	inner := &mocks.MockStorageRepository{Label: "mock::abc"}
	s := observed.NewStorage(inner, nil)
	assert.Equal(t, "mock::abc", s.(interface{ String() string }).String())
}

func TestObservedStorage_NilBus_DoesNotPanic(t *testing.T) {
	inner := &mocks.MockStorageRepository{Label: "mock::nil"}
	s := observed.NewStorage(inner, nil)

	require.NotPanics(t, func() {
		_ = s.Delete(t.Context(), "k")
	})
}

func TestObservedStorage_AllEventTypes(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)

	_ = s.Copy(t.Context(), "a", "b")
	_ = s.Delete(t.Context(), "k")
	_ = s.DeleteBatch(t.Context(), []string{"a"})
	_, _ = s.List(t.Context(), "p")

	want := []reflect.Type{
		reflect.TypeOf(observed.StorageCopyInfo{}),
		reflect.TypeOf(observed.StorageDeleteInfo{}),
		reflect.TypeOf(observed.StorageDeleteBatchInfo{}),
		reflect.TypeOf(observed.StorageListInfo{}),
	}
	for i, w := range want {
		evt := recvOne(t, ch)
		require.Equalf(t, w, reflect.TypeOf(evt), "event %d (got %T)", i, evt)
	}
}

func TestObservedStorage_GetStream_PublishesOnClose(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	payload := []byte("streamed-bytes")
	inner.GetStreamFunc = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	s := observed.NewStorage(inner, bus)

	rc, err := s.GetStream(t.Context(), "k")
	require.NoError(t, err)

	select {
	case evt := <-ch:
		t.Fatalf("event published before Close: %T", evt)
	case <-time.After(20 * time.Millisecond):
	}

	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, read)
	require.NoError(t, rc.Close())

	evt := recvOne(t, ch).(observed.StorageGetStreamInfo)
	assert.Equal(t, "k", evt.Key)
	assert.Equal(t, int64(len(payload)), evt.Bytes, "counting reader tallies full body")
	assert.NoError(t, evt.Err)
}

func TestObservedStorage_GetStream_BodyShortReadSurfacesOnEvent(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	bodyErr := io.ErrUnexpectedEOF
	inner.GetStreamFunc = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(&shortBody{tail: bodyErr}), nil
	}
	s := observed.NewStorage(inner, bus)

	rc, err := s.GetStream(t.Context(), "k")
	require.NoError(t, err, "open succeeds; the failure is mid-stream")

	_, _ = io.ReadAll(rc)
	require.NoError(t, rc.Close())

	evt := recvOne(t, ch).(observed.StorageGetStreamInfo)
	assert.ErrorIs(t, evt.Err, bodyErr,
		"body-phase short read must surface on the storage.getstream row — without this, log mis-attributes to the layer above")
}

// shortBody returns one chunk then a non-EOF error. Models the R2 mid-stream
// EOF shape — caller sees bytes then a transient terminator.
type shortBody struct {
	tail error
	done bool
}

func (s *shortBody) Read(p []byte) (int, error) {
	if s.done {
		return 0, s.tail
	}
	s.done = true
	n := copy(p, []byte("partial"))
	return n, nil
}

func TestObservedStorage_GetStream_FailureBeforeBody(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	sentinel := errors.New("open failed")
	inner.GetStreamFunc = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return nil, sentinel
	}
	s := observed.NewStorage(inner, bus)

	_, err := s.GetStream(t.Context(), "k")
	require.ErrorIs(t, err, sentinel)

	evt := recvOne(t, ch).(observed.StorageGetStreamInfo)
	assert.ErrorIs(t, evt.Err, sentinel)
	assert.Equal(t, int64(0), evt.Bytes, "no bytes on open-error")
}

func TestObservedStorage_PutStream_Success(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	inner.PutStreamFunc = func(_ context.Context, _ string, body io.Reader) error {
		_, _ = io.Copy(io.Discard, body)
		return nil
	}
	s := observed.NewStorage(inner, bus)

	payload := []byte("hello body")
	require.NoError(t, s.PutStream(t.Context(), "k", bytes.NewReader(payload)))

	evt := recvOne(t, ch).(observed.StoragePutStreamInfo)
	assert.Equal(t, "k", evt.Key)
	assert.Equal(t, int64(len(payload)), evt.Bytes, "Bytes reflects body size discovered via Seek")
	assert.NoError(t, evt.Err)
}

func TestObservedStorage_PutStream_InnerError(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	sentinel := errors.New("upload failed")
	inner.PutStreamFunc = func(_ context.Context, _ string, _ io.Reader) error { return sentinel }
	s := observed.NewStorage(inner, bus)

	err := s.PutStream(t.Context(), "k", bytes.NewReader([]byte("x")))
	require.ErrorIs(t, err, sentinel)

	evt := recvOne(t, ch).(observed.StoragePutStreamInfo)
	assert.ErrorIs(t, evt.Err, sentinel)
	assert.Equal(t, int64(1), evt.Bytes, "intended size recorded even on failure")
}

func TestObservedStorage_V2EventStrings(t *testing.T) {
	gs := observed.StorageGetStreamInfo{Store: "s", Key: "k", Bytes: 42, DurationMs: 3}.String()
	assert.Contains(t, gs, "storage.getstream")
	assert.Contains(t, gs, "k")
	assert.Contains(t, gs, "42")

	gsErr := observed.StorageGetStreamInfo{Store: "s", Key: "k", Err: errors.New("nope")}.String()
	assert.Contains(t, gsErr, "err=")
	assert.NotContains(t, gsErr, "bytes=", "error format must not emit bytes= alongside err=")

	ps := observed.StoragePutStreamInfo{Store: "s", Key: "k", Bytes: 99}.String()
	assert.Contains(t, ps, "storage.putstream")
	assert.Contains(t, ps, "99")

	ex := observed.StorageExistsInfo{Store: "s", Key: "k", Hit: true}.String()
	assert.Contains(t, ex, "storage.exists")
	assert.Contains(t, ex, "hit=true")
}

func TestObservedStorage_Exists(t *testing.T) {
	t.Run("hit", func(t *testing.T) {
		inner, bus, ch, cancel := setup(t)
		defer cancel()
		inner.ExistsFunc = func(_ context.Context, _ string) (bool, error) { return true, nil }
		s := observed.NewStorage(inner, bus)

		got, err := s.Exists(t.Context(), "k")
		require.NoError(t, err)
		assert.True(t, got)

		evt := recvOne(t, ch).(observed.StorageExistsInfo)
		assert.True(t, evt.Hit)
		assert.NoError(t, evt.Err)
	})

	t.Run("miss", func(t *testing.T) {
		inner, bus, ch, cancel := setup(t)
		defer cancel()
		inner.ExistsFunc = func(_ context.Context, _ string) (bool, error) { return false, nil }
		s := observed.NewStorage(inner, bus)

		got, err := s.Exists(t.Context(), "k")
		require.NoError(t, err)
		assert.False(t, got)

		evt := recvOne(t, ch).(observed.StorageExistsInfo)
		assert.False(t, evt.Hit)
	})

	t.Run("err", func(t *testing.T) {
		inner, bus, ch, cancel := setup(t)
		defer cancel()
		sentinel := errors.New("exists blew up")
		inner.ExistsFunc = func(_ context.Context, _ string) (bool, error) { return false, sentinel }
		s := observed.NewStorage(inner, bus)

		_, err := s.Exists(t.Context(), "k")
		require.ErrorIs(t, err, sentinel)

		evt := recvOne(t, ch).(observed.StorageExistsInfo)
		assert.ErrorIs(t, evt.Err, sentinel)
	})
}
