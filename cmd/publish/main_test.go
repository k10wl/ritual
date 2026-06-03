package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/ports/mocks"
)

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ritual.exe")
	require.NoError(t, os.WriteFile(path, []byte("binary-bytes"), 0o600))

	got, err := sha256File(path)
	require.NoError(t, err)

	want := sha256.Sum256([]byte("binary-bytes"))
	assert.Equal(t, hex.EncodeToString(want[:]), got, "leaf name must be the hex sha256 minio will verify against")
}

func TestSweepStaleSiblings_KeepsTargetDeletesOthers(t *testing.T) {
	const dir = "bin/windows-amd64/2.1.0/"
	keep := dir + "newsha.exe"

	var mu sync.Mutex
	var deleted []string
	storage := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{keep, dir + "oldsha.exe"}, nil // a prior re-publish of the same version
		},
		DeleteFunc: func(_ context.Context, key string) error {
			mu.Lock()
			defer mu.Unlock()
			deleted = append(deleted, key)
			return nil
		},
	}

	require.NoError(t, sweepStaleSiblings(t.Context(), storage, dir, keep))
	assert.Equal(t, []string{dir + "oldsha.exe"}, deleted,
		"sweep deletes only the superseded same-version sibling, never the just-uploaded artifact")
}

func TestSweepStaleSiblings_OnlyTarget_NoDeletes(t *testing.T) {
	const dir = "bin/linux-amd64/2.1.0/"
	keep := dir + "sha"
	deletes := 0
	storage := &mocks.MockStorageRepository{
		ListFunc:   func(_ context.Context, _ string) ([]string, error) { return []string{keep}, nil },
		DeleteFunc: func(_ context.Context, _ string) error { deletes++; return nil },
	}

	require.NoError(t, sweepStaleSiblings(t.Context(), storage, dir, keep))
	assert.Zero(t, deletes, "a clean publish (only our artifact present) deletes nothing")
}
