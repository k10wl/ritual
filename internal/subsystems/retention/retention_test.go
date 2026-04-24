package retention_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/subsystems/retention"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuild_PrunesRefsAndSweepsOrphanBlobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	seedRef(t, localDir, "20260420100000", "blob-alive-1", "blob-alive-2")
	seedRef(t, localDir, "20260419100000", "blob-shared")
	seedRef(t, localDir, "20260418100000", "blob-old-only")
	seedBlob(t, localDir, "blob-alive-1")
	seedBlob(t, localDir, "blob-alive-2")
	seedBlob(t, localDir, "blob-shared")
	seedBlob(t, localDir, "blob-old-only")
	seedBlob(t, localDir, "blob-orphan-never-referenced")

	seedRef(t, remoteDir, "20260420100000", "blob-alive-1")
	seedBlob(t, remoteDir, "blob-alive-1")

	localStorage := newFSRepo(t, localDir)
	remoteStorage := newFSRepo(t, remoteDir)

	rulesLocal := domain.RetentionRules{KeepLast: 1}
	rulesRemote := domain.RetentionRules{KeepLast: 1}
	manifest := &domain.Manifest{RemoteRetention: rulesRemote}
	t.Setenv("HOME", t.TempDir())

	writeSettings(t, rulesLocal)

	localJobs, remoteJobs, err := retention.Build(localStorage, remoteStorage, nil, manifest)
	require.NoError(t, err, "Build must wire jobs without error when storages are valid")

	for _, job := range localJobs {
		require.NoError(t, job(ctx), "each local retention job must complete cleanly")
	}
	for _, job := range remoteJobs {
		require.NoError(t, job(ctx), "each remote retention job must complete cleanly")
	}

	assertExists(t, localDir, "refs/20260420100000.json", "newest ref must survive KeepLast:1")
	assertMissing(t, localDir, "refs/20260419100000.json", "older ref must be pruned under KeepLast:1")
	assertMissing(t, localDir, "refs/20260418100000.json", "oldest ref must be pruned under KeepLast:1")

	assertExists(t, localDir, "objects/blob-alive-1", "live blob referenced by surviving ref must remain")
	assertExists(t, localDir, "objects/blob-alive-2", "live blob referenced by surviving ref must remain")
	assertMissing(t, localDir, "objects/blob-shared", "blob referenced only by pruned ref must be swept")
	assertMissing(t, localDir, "objects/blob-old-only", "blob referenced only by pruned ref must be swept")
	assertMissing(t, localDir, "objects/blob-orphan-never-referenced", "unreferenced blob must be swept on every cycle")
}

func newFSRepo(t *testing.T, dir string) *adapters.FSRepository {
	t.Helper()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err, "open root for fs repo")
	repo, err := adapters.NewFSRepository(root)
	require.NoError(t, err, "construct fs repo")
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func seedRef(t *testing.T, dir, ts string, blobHashes ...string) {
	t.Helper()
	objects := make(map[string]domain.Object, len(blobHashes))
	for _, h := range blobHashes {
		objects[h] = domain.Object{Hash: h, Size: 1}
	}
	ref := domain.Ref{
		Timestamp:     domain.RefID(ts),
		RitualVersion: "test",
		Targets:       []string{"worlds/**"},
		Objects:       objects,
	}
	data, err := json.Marshal(ref)
	require.NoError(t, err, "marshal ref")
	refsDir := filepath.Join(dir, "refs")
	require.NoError(t, os.MkdirAll(refsDir, 0o755), "create refs dir")
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, ts+".json"), data, 0o644), "write ref")
}

func seedBlob(t *testing.T, dir, hash string) {
	t.Helper()
	objectsDir := filepath.Join(dir, "objects")
	require.NoError(t, os.MkdirAll(objectsDir, 0o755), "create objects dir")
	require.NoError(t, os.WriteFile(filepath.Join(objectsDir, hash), []byte("blob-"+hash), 0o644), "write blob")
}

func writeSettings(t *testing.T, rules domain.RetentionRules) {
	t.Helper()
	s := &domain.Settings{Port: 25565, Memory: 4096, LocalRetention: rules}
	require.NoError(t, s.Save(), "save settings for retention build")
}

func assertExists(t *testing.T, dir, rel, msg string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.NoError(t, err, msg)
}

func assertMissing(t *testing.T, dir, rel, msg string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.Error(t, err, msg)
}
