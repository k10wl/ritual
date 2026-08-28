package retention_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
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

	seedRef(t, localDir, "2026-04-20T10-00-00.000Z", "blob-alive-1", "blob-alive-2")
	seedRef(t, localDir, "2026-04-19T10-00-00.000Z", "blob-shared")
	seedRef(t, localDir, "2026-04-18T10-00-00.000Z", "blob-old-only")
	seedBlob(t, localDir, "blob-alive-1")
	seedBlob(t, localDir, "blob-alive-2")
	seedBlob(t, localDir, "blob-shared")
	seedBlob(t, localDir, "blob-old-only")
	seedBlob(t, localDir, "blob-orphan-never-referenced")

	seedRef(t, remoteDir, "2026-04-20T10-00-00.000Z", "blob-alive-1")
	seedBlob(t, remoteDir, "blob-alive-1")

	localStorage := newFSRepo(t, localDir)
	remoteStorage := newFSRepo(t, remoteDir)

	rulesLocal := domain.RetentionRules{KeepLast: 1}
	rulesRemote := domain.RetentionRules{KeepLast: 1}
	// Isolate the settings file to a temp dir. config.RootPath is cached at init
	// from $HOME, so t.Setenv("HOME",…) alone does NOT redirect it — without this
	// the prune jobs' Select would read (and writeSettings would clobber) the
	// host's real settings.json (design-log/039: rules are read at prune time).
	origRoot := config.RootPath
	config.RootPath = t.TempDir()
	t.Cleanup(func() { config.RootPath = origRoot })

	writeSettings(t, rulesLocal, rulesRemote)

	localJobs, remoteJobs := retention.Build(localStorage, remoteStorage, nil, adapters.NewSerialRunner())

	for _, job := range localJobs {
		require.NoError(t, job.Run(ctx), "each local retention job must complete cleanly")
	}
	for _, job := range remoteJobs {
		require.NoError(t, job.Run(ctx), "each remote retention job must complete cleanly")
	}

	assertExists(t, localDir, "refs/2026-04-20T10-00-00.000Z.json", "newest ref must survive KeepLast:1")
	assertMissing(t, localDir, "refs/2026-04-19T10-00-00.000Z.json", "older ref must be pruned under KeepLast:1")
	assertMissing(t, localDir, "refs/2026-04-18T10-00-00.000Z.json", "oldest ref must be pruned under KeepLast:1")

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
	// Guard against stale fixtures: refsRetention.parseTime only recognises keys
	// in domain.RefIDFormat and silently skips anything else, which would make a
	// compact-format ts produce unprunable refs and quietly defeat the prune
	// assertions below (regression: design-log/045 §Bug3). Fail loud instead.
	if _, err := time.ParseInLocation(domain.RefIDFormat, ts, time.UTC); err != nil {
		t.Fatalf("seedRef ts %q must be domain.RefIDFormat (%s): %v", ts, domain.RefIDFormat, err)
	}
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

func writeSettings(t *testing.T, localRules, remoteRules domain.RetentionRules) {
	t.Helper()
	s := &domain.Settings{
		Port:            25565,
		Memory:          4096,
		LocalRetention:  localRules,
		RemoteRetention: remoteRules,
	}
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
