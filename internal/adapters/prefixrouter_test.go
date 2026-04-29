package adapters

import (
	"bytes"
	"context"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mocks "ritual/internal/core/ports/mocks"
)

func TestPrefixRouter_PutStreamRoutesByPrefix(t *testing.T) {
	routedHits, fallbackHits := []string{}, []string{}
	routed := &mocks.MockStorageRepository{
		PutStreamFunc: func(_ context.Context, key string, _ io.Reader) error {
			routedHits = append(routedHits, key)
			return nil
		},
	}
	fallback := &mocks.MockStorageRepository{
		PutStreamFunc: func(_ context.Context, key string, _ io.Reader) error {
			fallbackHits = append(fallbackHits, key)
			return nil
		},
	}
	r := NewPrefixRouter("objects/", routed, fallback)

	require.NoError(t, r.PutStream(t.Context(), "objects/abc", bytes.NewReader(nil)))
	require.NoError(t, r.PutStream(t.Context(), "refs/2026.json", bytes.NewReader(nil)))
	require.NoError(t, r.PutStream(t.Context(), "lock", bytes.NewReader(nil)))

	assert.Equal(t, []string{"objects/abc"}, routedHits, "objects/* hits routed only")
	assert.Equal(t, []string{"refs/2026.json", "lock"}, fallbackHits, "non-prefix keys hit fallback only")
}

func TestPrefixRouter_DeleteBatchSplits(t *testing.T) {
	var routedKeys, fallbackKeys []string
	routed := &mocks.MockStorageRepository{
		DeleteBatchFunc: func(_ context.Context, keys []string) error {
			routedKeys = append(routedKeys, keys...)
			return nil
		},
	}
	fallback := &mocks.MockStorageRepository{
		DeleteBatchFunc: func(_ context.Context, keys []string) error {
			fallbackKeys = append(fallbackKeys, keys...)
			return nil
		},
	}
	r := NewPrefixRouter("objects/", routed, fallback)

	require.NoError(t, r.DeleteBatch(t.Context(), []string{"objects/a", "refs/x.json", "objects/b", "lock"}))

	assert.Equal(t, []string{"objects/a", "objects/b"}, routedKeys, "routed gets only prefix-matching keys")
	assert.Equal(t, []string{"refs/x.json", "lock"}, fallbackKeys, "fallback gets only non-prefix keys")
}

func TestPrefixRouter_ListMergesWhenPrefixOverlapsGate(t *testing.T) {
	routed := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, prefix string) ([]string, error) {
			assert.Equal(t, "", prefix, "routed listed with caller prefix")
			return []string{"objects/a", "objects/b"}, nil
		},
	}
	fallback := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, prefix string) ([]string, error) {
			assert.Equal(t, "", prefix, "fallback listed with caller prefix")
			return []string{"refs/x.json", "lock"}, nil
		},
	}
	r := NewPrefixRouter("objects/", routed, fallback)

	keys, err := r.List(t.Context(), "")
	require.NoError(t, err)
	sort.Strings(keys)
	assert.Equal(t, []string{"lock", "objects/a", "objects/b", "refs/x.json"}, keys, "root list merges both stores")
}

func TestPrefixRouter_ListInsidePrefixHitsRoutedOnly(t *testing.T) {
	fallbackCalls := 0
	routed := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, prefix string) ([]string, error) {
			assert.Equal(t, "objects/abc", prefix)
			return []string{"objects/abc"}, nil
		},
	}
	fallback := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, _ string) ([]string, error) {
			fallbackCalls++
			return nil, nil
		},
	}
	r := NewPrefixRouter("objects/", routed, fallback)

	keys, err := r.List(t.Context(), "objects/abc")
	require.NoError(t, err)
	assert.Equal(t, []string{"objects/abc"}, keys)
	assert.Equal(t, 0, fallbackCalls, "list under prefix never queries fallback")
}

func TestPrefixRouter_CopyAcrossGateUsesGetPut(t *testing.T) {
	payload := []byte("ref bytes that must not be re-encoded by the destination decorator")
	src := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, key string) (io.ReadCloser, error) {
			assert.Equal(t, "refs/source.json", key, "source GetStream uses fallback (raw) for refs/")
			return io.NopCloser(bytes.NewReader(payload)), nil
		},
	}
	var written []byte
	dst := &mocks.MockStorageRepository{
		PutStreamFunc: func(_ context.Context, key string, body io.Reader) error {
			assert.Equal(t, "objects/dest", key, "destination PutStream uses routed for objects/")
			b, err := io.ReadAll(body)
			require.NoError(t, err)
			written = b
			return nil
		},
		CopyFunc: func(_ context.Context, _, _ string) error {
			t.Fatal("cross-gate Copy must not delegate to inner Copy — codecs differ")
			return nil
		},
	}
	r := NewPrefixRouter("objects/", dst, src)

	require.NoError(t, r.Copy(t.Context(), "refs/source.json", "objects/dest"))

	assert.Equal(t, payload, written, "cross-gate Copy streams source bytes to destination")
}

// End-to-end disk-level proof of audit fix #5. Wires the production gate
// (compressing routed onto objects/, raw fallback for refs/) over a real
// FSRepository, then reads bytes back through a sibling raw handle so the
// assertions reflect what an operator would see with `cat`.
func TestPrefixRouter_RefsOnDiskHumanReadable_ObjectsOnDiskCompressed(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	rawFS, err := NewFSRepository(root, "raw")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawFS.Close() })

	compressed, err := NewCompressingStorage(rawFS)
	require.NoError(t, err)

	gate := NewPrefixRouter("objects/", compressed, rawFS)

	refKey := "refs/2026-04-29T12-00-00-000Z.json"
	refBody := []byte(`{"id":"2026-04-29T12-00-00-000Z","objects":{}}`)
	require.NoError(t, gate.PutStream(t.Context(), refKey, bytes.NewReader(refBody)))

	rawDiskRef, err := rawFS.GetStream(t.Context(), refKey)
	require.NoError(t, err)
	rawRefBytes, err := io.ReadAll(rawDiskRef)
	require.NoError(t, err)
	require.NoError(t, rawDiskRef.Close())

	assert.Equal(t, refBody, rawRefBytes,
		"audit fix #5 regression: refs/* written through the gate must land on disk byte-for-byte — compression decorator must not see this keyspace, otherwise `cat refs/<id>.json` gives an operator unreadable zstd output")

	objBody := []byte("blob payload that should be zstd-compressed on disk under the gate")
	objKey := "objects/" + hexHash(objBody)
	require.NoError(t, gate.PutStream(t.Context(), objKey, bytes.NewReader(objBody)))

	rawDiskObj, err := rawFS.GetStream(t.Context(), objKey)
	require.NoError(t, err)
	rawObjBytes, err := io.ReadAll(rawDiskObj)
	require.NoError(t, err)
	require.NoError(t, rawDiskObj.Close())

	assert.NotEqual(t, objBody, rawObjBytes,
		"audit fix #5 regression: objects/* written through the gate must hit compressing — raw bytes on disk must differ from the input payload, otherwise the gate has bypassed compression and storage costs blow up on real remotes")
}

func hexHash(payload []byte) string {
	const hex = "0123456789abcdef"
	h := xxhash.Sum64(payload)
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[h&0xF]
		h >>= 4
	}
	return string(out)
}

func TestPrefixRouter_CopySameGateDelegatesToInner(t *testing.T) {
	innerCopyHits := 0
	routed := &mocks.MockStorageRepository{
		CopyFunc: func(_ context.Context, sourceKey, destKey string) error {
			innerCopyHits++
			assert.Equal(t, "objects/a", sourceKey)
			assert.Equal(t, "objects/b", destKey)
			return nil
		},
	}
	fallback := &mocks.MockStorageRepository{}
	r := NewPrefixRouter("objects/", routed, fallback)

	require.NoError(t, r.Copy(t.Context(), "objects/a", "objects/b"))

	assert.Equal(t, 1, innerCopyHits, "same-gate Copy delegates to inner so adapter optimisations (server-side copy) survive")
}
