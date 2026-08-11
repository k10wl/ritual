package adapters

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwappableStorage_ForwardsToCurrentBackingStore(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	fsRepo, err := NewFSRepository(root)
	require.NoError(t, err)

	sw := NewSwappableStorage()
	sw.Store(fsRepo)

	require.NoError(t, sw.PutStream(context.Background(), "objects/abc", bytes.NewReader([]byte("hello"))))
	rc, err := sw.GetStream(context.Background(), "objects/abc")
	require.NoError(t, err, "SwappableStorage.GetStream must forward to the currently stored backing repository")
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, "hello", string(data), "content written through the facade must be readable back through it unchanged")
}

func TestSwappableStorage_SwapMidTest_CallerObservesNewTargetWithoutReconstruction(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	firstRoot, err := os.OpenRoot(firstDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = firstRoot.Close() })
	firstRepo, err := NewFSRepository(firstRoot)
	require.NoError(t, err)

	secondRoot, err := os.OpenRoot(secondDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = secondRoot.Close() })
	secondRepo, err := NewFSRepository(secondRoot)
	require.NoError(t, err)

	sw := NewSwappableStorage()
	sw.Store(firstRepo)
	require.NoError(t, sw.PutStream(context.Background(), "objects/abc", bytes.NewReader([]byte("first"))))

	// A caller holding only the facade — never the concrete FSRepository —
	// must observe the new backing store immediately after Store, with no
	// reconstruction on the caller's side (design-log/055 Q4).
	sw.Store(secondRepo)
	require.NoError(t, sw.PutStream(context.Background(), "objects/xyz", bytes.NewReader([]byte("second"))))

	_, err = sw.GetStream(context.Background(), "objects/abc")
	assert.Error(t, err, "a key written only to the first backing store must not be visible after the facade swaps to the second")

	rc, err := sw.GetStream(context.Background(), "objects/xyz")
	require.NoError(t, err, "a key written to the second backing store must be visible immediately after Store, with the facade never reconstructed")
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, "second", string(data))
}

func TestSwappableStorage_String_ReflectsCurrentBackingStore(t *testing.T) {
	assert.Equal(t, "swappable::unset", NewSwappableStorage().String(), "an unset facade must self-describe distinctly rather than panic")

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	repo, err := NewFSRepository(root, "local")
	require.NoError(t, err)

	sw := NewSwappableStorage()
	sw.Store(repo)
	assert.Equal(t, repo.String(), sw.String(), "String() must delegate to the current backing store's own label")
}
