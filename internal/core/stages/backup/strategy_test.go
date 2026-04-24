package backup_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/backup"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrategy_Name_IsBackup(t *testing.T) {
	s := backup.New(nil, nil, nil, nil)
	assert.Equal(t, "Backup", s.Name(), "stage name must be Backup — appears in StateChangedInfo and logs")
}

func TestStrategy_NoSessionID_Skips(t *testing.T) {
	local, remote := countingStore(), countingStore()
	s := backup.New(local, remote, nilManifestStore(), nil)

	rs := &ritual.RunState{Bus: adapters.NewEventBus(8)}
	next, err := s.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Nil(t, next, "no lock → onNext (nil) forward without work")
	assert.Equal(t, 0, local.CopyCalls, "no lock → local store must not receive any Copy calls")
	assert.Equal(t, 0, remote.CopyCalls, "no lock → remote store must not receive any Copy calls")
}

func TestStrategy_NoMutation_Skips(t *testing.T) {
	entry := domain.FileEntry{Hash: "h1", Size: 1}
	pre := manifestWith(map[string]domain.FileEntry{"world/a.dat": entry})
	post := manifestWith(map[string]domain.FileEntry{"world/a.dat": entry})

	local, remote := countingStore(), countingStore()
	s := backup.New(local, remote, &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) { return post, nil },
	}, nil)

	rs := &ritual.RunState{Bus: adapters.NewEventBus(8), SessionID: "lock-1", LocalBefore: pre}
	_, err := s.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, 0, local.CopyCalls, "XXHashMap unchanged → no local Copy — retention policy requires distinct snapshots, not duplicates")
	assert.Equal(t, 0, remote.CopyCalls, "XXHashMap unchanged → no remote Copy — same reason as local")
}

func TestStrategy_Mutated_CopiesOnBothSides(t *testing.T) {
	pre := manifestWith(map[string]domain.FileEntry{"world/a.dat": {Hash: "h1", Size: 1}})
	post := manifestWith(map[string]domain.FileEntry{"world/a.dat": {Hash: "h2", Size: 1}})

	local, remote := countingStore(), countingStore()
	local.ListFunc = listKeys("worlds/world/a.dat")
	remote.ListFunc = listKeys("worlds/world/a.dat")

	s := backup.New(local, remote, &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) { return post, nil },
	}, nil)

	rs := &ritual.RunState{Bus: adapters.NewEventBus(8), SessionID: "lock-1", LocalBefore: pre}
	_, err := s.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, 1, local.CopyCalls, "mutation detected → one local Copy per file")
	assert.Equal(t, 1, remote.CopyCalls, "mutation detected → one remote Copy per file (server-side, zero egress)")
	assert.Equal(t, 0, local.GetCalls+remote.GetCalls, "same-storage backup must not Get — Copy is server-side; Get implies cross-storage download")
	assert.Equal(t, 2, local.PutCalls+remote.PutCalls, "each side writes exactly one manifest.json alongside the snapshot")
}

func TestStrategy_CopyError_EmitsErrorInfo_ContinuesToOnNext(t *testing.T) {
	pre := manifestWith(map[string]domain.FileEntry{"world/a.dat": {Hash: "h1", Size: 1}})
	post := manifestWith(map[string]domain.FileEntry{"world/a.dat": {Hash: "h2", Size: 1}})

	bus := adapters.NewEventBus(32)
	ch, unsub := bus.Subscribe()
	defer unsub()

	local := countingStore()
	local.ListFunc = listKeys("worlds/world/a.dat")
	local.CopyFunc = func(context.Context, string, string) error { return errors.New("disk full") }
	remote := countingStore()
	remote.ListFunc = listKeys("worlds/world/a.dat")

	s := backup.New(local, remote, &mocks.MockManifestStore{
		GetFunc: func(context.Context) (*domain.Manifest, error) { return post, nil },
	}, nil)

	rs := &ritual.RunState{Bus: bus, SessionID: "lock-1", LocalBefore: pre}
	_, err := s.Run(t.Context(), rs)

	require.NoError(t, err, "backup failure must not bubble up — lock release depends on pipeline continuing past this stage")
	assert.True(t, hasErrorInfo(ch, "backup", 500*time.Millisecond),
		"CreateBackup failure must publish ritual.ErrorInfo{Operation: backup} so failures appear in logs and GUI")
	assert.Equal(t, 1, remote.CopyCalls, "remote side runs even after local side errored — both snapshots are independent")
}

// ---------- helpers ----------

func countingStore() *countingStorage {
	return &countingStorage{MockStorageRepository: &mocks.MockStorageRepository{}}
}

type countingStorage struct {
	*mocks.MockStorageRepository
	GetCalls  int
	PutCalls  int
	CopyCalls int
}

func (c *countingStorage) Get(ctx context.Context, key string) ([]byte, error) {
	c.GetCalls++
	return c.MockStorageRepository.Get(ctx, key)
}

func (c *countingStorage) Put(ctx context.Context, key string, data []byte) error {
	c.PutCalls++
	return c.MockStorageRepository.Put(ctx, key, data)
}

func (c *countingStorage) Copy(ctx context.Context, src, dst string) error {
	c.CopyCalls++
	return c.MockStorageRepository.Copy(ctx, src, dst)
}

func listKeys(keys ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return keys, nil }
}

func nilManifestStore() ports.ManifestStore {
	return &mocks.MockManifestStore{GetFunc: func(context.Context) (*domain.Manifest, error) { return nil, nil }}
}

func manifestWith(xxhash map[string]domain.FileEntry) *domain.Manifest {
	m := &domain.Manifest{}
	m.Worlds.XXHashMap = xxhash
	return m
}

func hasErrorInfo(ch <-chan ports.Event, op string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return false
		case e, ok := <-ch:
			if !ok {
				return false
			}
			if ei, match := e.(ritual.ErrorInfo); match && ei.Operation == op {
				return true
			}
		}
	}
}
