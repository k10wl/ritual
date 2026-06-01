// Integration coverage for the server-free Download / Upload flows
// (design-log/031). Same rules as ritual_integration_test.go: no body
// comments, meaningful names, verbose assertion messages.
package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/pipeline"
)

func (r *testRitual) attachSyncFlows(t *testing.T) {
	t.Helper()
	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create worlds dir for sync flows")

	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)
	localHeadResolver := pulling.NewHeadResolver(r.local)
	committer, pusher, commitTargets := r.buildCommittingVerbs(t, worldsPath, scanner)

	host, _ := os.Hostname()
	localLocker := observed.NewLocker(lock.New(r.local, host), r.bus)
	remoteLocker := observed.NewLocker(lock.New(r.remote, host), r.bus)
	locker := lock.NewBoth(localLocker, remoteLocker)

	deps := pipeline.Deps{
		Bus:               r.bus,
		Puller:            puller,
		Applier:           applier,
		HeadResolver:      headResolver,
		LocalHeadResolver: localHeadResolver,
		Committer:         committer,
		CommitOpts:        ritual.NewCommitOptsResolver(commitTargets),
		Pusher:            pusher,
		LocalRetentions:   r.localRetentions,
		RemoteRetentions:  r.remoteRetentions,
		AcquireFn:         locker.Acquire,
		InspectFn:         locker.Inspect,
		ReleaseFn:         locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
	}
	entries := lifecycle.Entries{
		Download: pipeline.BuildDownload(deps),
		Upload:   pipeline.BuildUpload(deps),
	}
	stop := lifecycle.Attach(r.ctx, r.bus, entries)
	t.Cleanup(stop)
}

// attachLocalSession wires the skip-sync / local-only pipeline
// (design-log/036, BuildLocalSession) behind a fake server and publishes
// StartRequested{SkipSync:true}. Only Bus + CmdBuilder + Readiness matter to
// the no-save chain (Checking → Running → Done); no pulling/committing verbs,
// no locker — the absence of those nodes is the whole point. Returns the
// fakeServer so the caller drives the run (waitReady → write → exit).
func (r *testRitual) attachLocalSession(t *testing.T) *fakeServer {
	t.Helper()
	worldsPath := filepath.Join(r.localDir, config.WorldsDir)
	require.NoError(t, os.MkdirAll(worldsPath, 0o755), "create worlds dir for local session")

	server := r.fakerun()
	deps := pipeline.Deps{
		Bus:        r.bus,
		CmdBuilder: &fakeServerCmdBuilder{server: server},
		Readiness:  immediateReady{},
	}
	entries := lifecycle.Entries{LocalSession: pipeline.BuildLocalSession(deps)}
	stop := lifecycle.Attach(r.ctx, r.bus, entries)
	t.Cleanup(stop)

	r.bus.Publish(ritual.StartRequested{SkipSync: true})
	return server
}

func readRemoteRef(t *testing.T, r *testRitual, id domain.RefID) domain.Ref {
	t.Helper()
	rc, err := r.remote.GetStream(r.ctx, "refs/"+string(id)+".json")
	require.NoError(t, err, "read remote ref %s", id)
	defer rc.Close()
	var ref domain.Ref
	require.NoError(t, json.NewDecoder(rc).Decode(&ref), "decode remote ref %s", id)
	return ref
}

func (r *testRitual) assertLocalLockAbsent(t *testing.T, msg string) {
	t.Helper()
	exists, err := r.local.Exists(r.ctx, lock.Key)
	require.NoError(t, err, msg)
	assert.False(t, exists, msg)
}

func TestIntegration_Upload_SeedingEmptyRemote_WritesRootRef(t *testing.T) {
	r := newRitual(t)
	seedLocalWorld(t, r, file("world/level.dat", []byte("seed-bytes")))
	r.attachSyncFlows(t)

	r.bus.Publish(ritual.UploadRequested{})
	r.waitDone(t)

	head := maxRefID(r.ctx, r.remote)
	require.NotEmpty(t, head, "Upload against an empty remote must bootstrap the first ref from local worlds (seeding)")
	ref := readRemoteRef(t, r, head)
	assert.Empty(t, ref.Parent, "the seed ref has no parent — Probing found ErrNoHead and left ParentRefID empty")
	assert.Contains(t, ref.Objects, "world/level.dat", "the uploaded ref must capture the local world file")
	r.assertRemoteLockAbsent(t, "Upload must release the remote lock after pushing the seed ref")
}

func TestIntegration_Upload_PopulatedRemote_ParentsOnLocalHead(t *testing.T) {
	r := newRitual(t)
	seedRemoteWorld(t, r, file("world/level.dat", []byte("remote-A")))
	priorHead := maxRefID(r.ctx, r.remote)
	require.NotEmpty(t, priorHead, "seed must establish a remote HEAD so the new ref can be compared against it")

	seedLocalWorld(t, r, file("world/level.dat", []byte("local-B")))
	r.attachSyncFlows(t)

	r.bus.Publish(ritual.UploadRequested{})
	r.waitDone(t)

	newHead := maxRefID(r.ctx, r.remote)
	assert.NotEqual(t, priorHead, newHead, "Publish must write a new ref that becomes the newest remote HEAD by timestamp")
	ref := readRemoteRef(t, r, newHead)
	assert.Empty(t, ref.Parent, "Publish parents on the LOCAL HEAD, not the remote HEAD (design-log/035 §Q3 — lineage follows where the operator stands); seedLocalWorld writes worlds but no local ref, so the local HEAD is empty and Probing leaves ParentRefID empty")
	r.assertRemoteLockAbsent(t, "Publish must release the remote lock after pushing")
}

func TestIntegration_Upload_LockHeldByOther_FailsAcquiringNoRefWritten(t *testing.T) {
	r := newRitual(t)
	seedLocalWorld(t, r, file("world/level.dat", []byte("local-only")))
	seedLiveLocalLock(t, r, "other-host", "other-session")
	r.attachSyncFlows(t)

	r.bus.Publish(ritual.UploadRequested{})
	r.waitFailed(t)

	assert.Empty(t, maxRefID(r.ctx, r.remote),
		"a contended Acquiring must abort the Upload before Committing — the remote must carry no ref and no objects")
}

func TestIntegration_Download_PullsRemoteHead_NoLockTaken(t *testing.T) {
	r := newRitual(t)
	seedRemoteWorld(t, r, file("world/level.dat", []byte("remote-data")))
	priorHead := maxRefID(r.ctx, r.remote)
	r.attachSyncFlows(t)

	r.bus.Publish(ritual.DownloadRequested{})
	r.waitDone(t)

	r.assertLocalFileContent(t, "world/level.dat", []byte("remote-data"),
		"Download must materialise the remote HEAD into the local worlds dir")
	assert.Equal(t, priorHead, maxRefID(r.ctx, r.remote),
		"Download is read-only — it must not write a new remote ref")
	r.assertRemoteLockAbsent(t, "Download has no Acquiring stage — it must never take the remote lock")
	r.assertLocalLockAbsent(t, "Download has no Acquiring stage — it must never take the local lock")
}

func TestIntegration_Download_EmptyRemote_NoOpDone(t *testing.T) {
	r := newRitual(t)
	r.attachSyncFlows(t)

	r.bus.Publish(ritual.DownloadRequested{})
	r.waitDone(t)

	r.assertLocalFileMissing(t, "world/level.dat", "a Download against an empty remote must write nothing locally")
	assert.Empty(t, maxRefID(r.ctx, r.remote), "an empty-remote Download must leave the remote empty")
}

func TestIntegration_SkipSync_RunsServerNoPullNoCommit(t *testing.T) {
	r := newRitual(t)
	seedRemoteWorld(t, r, file("world/level.dat", []byte("remote-canonical")))
	priorHead := maxRefID(r.ctx, r.remote)
	require.NotEmpty(t, priorHead, "seed must establish a remote HEAD so the test can assert skip-sync leaves it untouched")

	drain := collectBusEvents(r.bus)

	server := r.attachLocalSession(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("local-only-session-edit"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	assert.Equal(t, []string{ritual.StageChecking, ritual.StageRunning}, stageSequence(drain()),
		"skip-sync runs the no-save chain (design-log/036 no-save reversal): Checking → Running → Done only. Any Pulling/Acquiring/Committing/Pushing/Retaining/Unlocking stage means a save node leaked back into BuildLocalSession")
	assert.Empty(t, maxRefID(r.ctx, r.local),
		"skip-sync saves nothing — the local-only session must write NO local ref; recovery is the deliberate design-log/035 dirty-Publish afterward, not an auto-commit here")
	assert.Equal(t, priorHead, maxRefID(r.ctx, r.remote),
		"skip-sync must not Pull, Acquire, Commit, or Push — the remote HEAD must be exactly what it was before launch, untouched by the local-only run")
	r.assertRemoteLockAbsent(t, "skip-sync has no Acquiring stage — it must never take the remote lock")
	r.assertLocalLockAbsent(t, "skip-sync has no Acquiring stage — it must never take the local lock")
	r.assertLocalFileContent(t, "world/level.dat", []byte("local-only-session-edit"),
		"the server in a skip-sync session runs on the on-disk worlds and its edits land in the workdir as-is (no Pulling overwrote them with remote-canonical) — that dirty workdir is exactly what design-log/035 Publish later recovers")
}
