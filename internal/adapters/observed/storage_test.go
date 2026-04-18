package observed_test

import (
	"context"
	"errors"
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

func TestObservedStorage_Get_Success(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	inner.GetFunc = func(_ context.Context, key string) ([]byte, error) {
		return []byte("hello"), nil
	}
	s := observed.NewStorage(inner, bus)

	data, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)

	evt := recvOne(t, ch)
	got, ok := evt.(observed.StorageGetInfo)
	require.True(t, ok, "expected StorageGetInfo, got %T", evt)
	assert.Equal(t, "mock::test", got.Store)
	assert.Equal(t, "k", got.Key)
	assert.Equal(t, 5, got.Bytes)
	assert.NoError(t, got.Err)
}

func TestObservedStorage_Get_Failure(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	want := errors.New("boom")
	inner.GetFunc = func(_ context.Context, _ string) ([]byte, error) { return nil, want }
	s := observed.NewStorage(inner, bus)

	_, err := s.Get(t.Context(), "k")
	require.ErrorIs(t, err, want)

	evt := recvOne(t, ch).(observed.StorageGetInfo)
	assert.ErrorIs(t, evt.Err, want)
	assert.Equal(t, 0, evt.Bytes)
}

func TestObservedStorage_Put(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)
	require.NoError(t, s.Put(t.Context(), "k", []byte("payload")))

	evt := recvOne(t, ch).(observed.StoragePutInfo)
	assert.Equal(t, "k", evt.Key)
	assert.Equal(t, 7, evt.Bytes)
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

func TestObservedStorage_Rename(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)
	require.NoError(t, s.Rename(t.Context(), "src", "dst"))

	evt := recvOne(t, ch).(observed.StorageRenameInfo)
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
		_, _ = s.Get(t.Context(), "k")
		_ = s.Put(t.Context(), "k", []byte{})
	})
}

func TestObservedStorage_AllEventTypes(t *testing.T) {
	inner, bus, ch, cancel := setup(t)
	defer cancel()
	s := observed.NewStorage(inner, bus)

	_, _ = s.Get(t.Context(), "k")
	_ = s.Put(t.Context(), "k", []byte("x"))
	_ = s.Copy(t.Context(), "a", "b")
	_ = s.Rename(t.Context(), "a", "b")
	_ = s.Delete(t.Context(), "k")
	_ = s.DeleteBatch(t.Context(), []string{"a"})
	_, _ = s.List(t.Context(), "p")

	want := []reflect.Type{
		reflect.TypeOf(observed.StorageGetInfo{}),
		reflect.TypeOf(observed.StoragePutInfo{}),
		reflect.TypeOf(observed.StorageCopyInfo{}),
		reflect.TypeOf(observed.StorageRenameInfo{}),
		reflect.TypeOf(observed.StorageDeleteInfo{}),
		reflect.TypeOf(observed.StorageDeleteBatchInfo{}),
		reflect.TypeOf(observed.StorageListInfo{}),
	}
	for i, w := range want {
		evt := recvOne(t, ch)
		require.Equalf(t, w, reflect.TypeOf(evt), "event %d (got %T)", i, evt)
	}
}
