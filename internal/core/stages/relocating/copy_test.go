package relocating

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowReadCloser drips fixed-size chunks out one at a time with a sleep
// between each, so a single-key copy can be made to outlast
// relocateHeartbeat without needing an actual multi-hundred-KB fixture file.
type slowReadCloser struct {
	chunks [][]byte
	delay  time.Duration
	i      int
}

func (s *slowReadCloser) Read(p []byte) (int, error) {
	if s.i >= len(s.chunks) {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	n := copy(p, s.chunks[s.i])
	s.i++
	return n, nil
}

func (s *slowReadCloser) Close() error { return nil }

// fakeStorage is a minimal in-memory ports.StorageRepository stand-in.
// copyContent only exercises GetStream/PutStream/List; the rest are
// implemented just to satisfy the interface.
type fakeStorage struct {
	mu   sync.Mutex
	data map[string][]byte
	slow map[string]*slowReadCloser
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: map[string][]byte{}, slow: map[string]*slowReadCloser{}}
}

func (f *fakeStorage) String() string { return "fake" }

func (f *fakeStorage) GetStream(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.slow[key]; ok {
		return s, nil
	}
	b, ok := f.data[key]
	if !ok {
		return nil, errors.New("not found: " + key)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeStorage) PutStream(_ context.Context, key string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.data[key] = b
	f.mu.Unlock()
	return nil
}

func (f *fakeStorage) Exists(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[key]
	return ok, nil
}

func (f *fakeStorage) Delete(context.Context, string) error        { return nil }
func (f *fakeStorage) DeleteBatch(context.Context, []string) error { return nil }

func (f *fakeStorage) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	for k := range f.slow {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeStorage) Copy(context.Context, string, string) error { return nil }

var _ ports.StorageRepository = (*fakeStorage)(nil)

// TestCopyContent_HeartbeatPublishesProgressMidLargeFile regression-guards
// the 2026-08-15 live report ("progress not moving while transferring"):
// copyContent used to publish RelocateProgress only after a whole file
// finished, so a single file that outlasts one publish interval froze
// BytesDone/ETA/arc for its entire transfer. This asserts the heartbeat
// path fires WHILE still mid-copy of the one slow file (FilesDone still 0)
// with a real, partial BytesDone > 0 — not just at the file-boundary.
func TestCopyContent_HeartbeatPublishesProgressMidLargeFile(t *testing.T) {
	orig := relocateHeartbeat
	relocateHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { relocateHeartbeat = orig })

	oldWorkdir := newFakeStorage()
	chunks := make([][]byte, 6)
	for i := range chunks {
		chunks[i] = bytes.Repeat([]byte{'x'}, 1024)
	}
	oldWorkdir.slow["server/big.dat"] = &slowReadCloser{chunks: chunks, delay: 15 * time.Millisecond}

	refs := WorkRootRefs{
		Root:    new(atomic.Pointer[os.Root]),
		Local:   adapters.NewSwappableStorage(),
		Workdir: adapters.NewSwappableStorage(),
	}
	refs.Local.Store(newFakeStorage())
	refs.Workdir.Store(oldWorkdir)

	newLocal := newFakeStorage()
	newWorkdir := newFakeStorage()

	bus := adapters.NewEventBus(64)
	sub, unsub := bus.Subscribe()

	var mu sync.Mutex
	var events []ports.Event
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for e := range sub {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
	}()

	err := copyContent(context.Background(), refs, newLocal, newWorkdir, bus)
	require.NoError(t, err)
	unsub()
	<-collected

	mu.Lock()
	defer mu.Unlock()
	var sawMidFileHeartbeat bool
	for _, e := range events {
		p, ok := e.(RelocateProgress)
		if ok && p.FilesDone == 0 && p.BytesDone > 0 {
			sawMidFileHeartbeat = true
			break
		}
	}
	assert.True(t, sawMidFileHeartbeat, "a heartbeat tick must report partial BytesDone while still mid-copy of the single slow file (FilesDone still 0) — proving progress moves before the file boundary, the exact freeze this test guards against")
}
