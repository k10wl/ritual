# Remove v1 Publishing/Backup/Terminal-Retention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the v1 post-run tail (`publishing` → `backup` → terminal `retaining`) from the state machine and replace it with the v2.1 spec chain `committing → pruning(local) → pushing → pruning(remote) → unlocking`.

**Architecture:** The existing `committing` stage (already wired as code but not in the chain) stays. A new `pushing` stage wraps the existing `refs.Pusher`. The existing `retaining.Strategy` is already chainable; the two instances move from tail position to pair with `committing` and `pushing` per spec §2297–2309. The `publishing` and `backup` stages, their helpers (`UpdaterService` port, `RitualUpdater`, `SyncDownloadUpdater`, `SyncUploader`, `services.CreateBackup`, `config.BackupsDir` consumers, `migrateLegacyBackups`) are deleted. Composition roots (`cmd/cli`, `cmd/gui`, integration harness) update to pass `Committer`, `Pusher`, `CommitOptsResolver` to `app.New` instead of `exitUpdaters`.

**Manifest + SyncService + heartbeat scope (expanded):** The lock is already a separate object (`lock.Locker` against remote storage directly, spec §1613) — it does NOT read or write `domain.Manifest`. That means `heartbeat.Supervisor` needs nothing more than the `HeartbeatFn` (which closes over `lock.Locker`). Everything else in the supervisor — `localStore`, `remoteStore`, `syncer`, `syncTick`, sync-readiness atomics, `SaveRequested`/`SaveCompleted` wait loop — is v1 manifest plumbing and is deleted in one cut. `services.SyncService` and its port drop; no live-tick commit replaces them in this plan (spec §1917 flags that design as TBD — future work). With SyncService gone, `ports.ManifestStore`, `adapters.ManifestStore`, `domain.Manifest`, `domain.WorldsManifest`, `domain.SyncState`, and the orphaned `ValidatorService` lose every non-test caller and are deleted together. `RunState.LocalBefore` / `RemoteBefore`, `acquiring.SnapshotLocalFn`, `app.Ritual.localManifests` / `remoteManifests` constructor params, and `Kit.WorldSync` all drop out of the composition graph in the same sweep.

**Tech Stack:** Go, `ports.Committer` / `ports.Pusher` (existing), `refs.NewCommitter` / `refs.NewPusher` (existing), `adapters.NewParallelRunner`.

**Spec reference:** `docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md` §2264–2320.

---

## File Structure

### Create

- `internal/core/stages/pushing/strategy.go` — `pushing.Strategy` (mirrors `pulling.Strategy` shape; reads `rs.RefID`, calls `Pusher.Push`, routes onOK/onFail).
- `internal/core/stages/pushing/strategy_test.go` — unit tests.
- `internal/app/commitopts.go` — composition helper: `NewCommitOptsResolver(parentFn func(*ritual.RunState) domain.RefID, targets []string) committing.OptsResolver`.

### Modify

- `internal/app/ritual.go` — new constructor params (`committer`, `pusher`, `commitTargets`); new chain wiring; drop `exitUpdaters`, `localManifests`, `remoteManifests`.
- `internal/core/ritual/stages.go` — delete `StagePublishing`, `StageBackup`; add `StagePushing`.
- `internal/core/ritual/runstate.go` — delete `LocalBefore`, `RemoteBefore` fields.
- `internal/core/stages/acquiring/strategy.go` — delete `SnapshotLocalFn` param and its internal call site.
- `internal/gui/projection/projection.go` — drop `StagePublishing`/`StageBackup` cases; add `StageCommitting`, `StagePushing`.
- `internal/gui/projection/projection_test.go` — rewrite `StageBackup` test to assert `StagePushing`/`StageCommitting` mappings.
- `cmd/gui/main.go` — build `Committer`, `Pusher`, `commitTargets`; drop `SyncUploader` + `[]UpdaterService`; drop manifest-store construction (except what lock/heartbeat still needs — see Task 18).
- `cmd/cli/main.go` — same treatment; drop `localManifests`/`remoteManifests` from `app.New`; update `heartbeat.Attach` to the reduced signature.
- `internal/subsystems/sync/kit.go` — drop `ExitUpdaters` + `WorldSync` fields and their builder lines; rename or delete file if only `puller`/`applier`/`headResolver`/`committer`/`pusher` remain.
- `internal/subsystems/heartbeat/supervisor.go` — delete `localStore`/`remoteStore`/`syncer` fields, the `syncTick` method, and related sync-readiness plumbing. Keep only the lease-refresh tick.
- `internal/subsystems/heartbeat/supervisor_test.go` — delete sync-tick tests; keep lease-refresh tests.
- `internal/app/ritual_integration_test.go` — drop backup/publishing stories; drop `localManifests`/`remoteManifests` from the harness; add commit/push stories.
- `internal/app/ritual_test.go` — update constructor calls.

### Delete

- `internal/core/stages/publishing/` (whole dir).
- `internal/core/stages/backup/` (whole dir, including `strategy_test.go`).
- `internal/core/ports/ports.go` — `UpdaterService`, `SyncService`, `ValidatorService`, `ManifestStore` interface blocks.
- `internal/core/ports/manifest_store.go` — the ManifestStore interface file (if separate).
- `internal/core/ports/mocks/updater.go` + `updater_test.go`.
- `internal/core/ports/mocks/manifest_store.go`.
- `internal/core/services/sync_updater.go`.
- `internal/core/services/updater_ritual.go` + `updater_ritual_test.go` (if present).
- `internal/core/services/sync.go` + `sync_test.go` + `sync_integration_test.go`.
- `internal/core/services/validator.go` + `validator_test.go`.
- `internal/core/services/backup.go` + `backup_integration_test.go`.
- `internal/core/services/migration.go` — `migrateLegacyBackups` function + caller line in `migrateV2` + `domain.Manifest` / `ManifestVersion` parameters of `RunMigrations` / `RunMigrationsWithList`. Callers must pass a version string directly, not a manifest. (If `services.Migration` has no surviving non-test callers after `domain.Manifest` deletion, delete `migration.go` + `migration_test.go` entirely — verify first.)
- `internal/adapters/manifest_store.go` + tests.
- `internal/core/domain/manifest.go` + `manifest_test.go`. Supporting types (`WorldsManifest`, `SyncState`, `FileEntry`) go with it unless grep finds non-test survivors outside the deleted files.
- `internal/config/config.go` — `BackupsDir` constant; `ManifestFilename` if no non-test callers remain.
- `internal/subsystems/sync/kit.go` + the whole `internal/subsystems/sync/` directory once `WorldSync` and `ExitUpdaters` are dropped — the Kit shrinks to `{Puller, Applier, HeadResolver, Committer, Pusher, CommitTargets}`, at which point it's a one-function wrapper around five `refs.New*` calls and is clearer inlined into `cmd/cli`. Confirm no other importers via `grep -rn "subsystems/sync"`.
- `internal/core/sync/` — the whole v1 sync state-machine engine (engine.go, planning.go, staging.go, committing.go, orphancleanup.go, ghostcleanup.go, stagedirinit.go, stagingdircleanup.go, failed.go, done.go, events.go, runstate.go + tests). After `services.SyncService` dies (Task 17), its only consumers are four projection cases (rewritten in Task 21 below). No production caller remains.
- `internal/adapters/ritualsync.go` + test — `.ritualsync` allowlist files. Spec §645 "Allowlist targets, not denylist" moves target globs onto `CommitOpts.Targets` (manifest-embedded, travels with history). On-disk `.ritualsync` files go away entirely.
- `internal/core/services/migration.go` + `migration_test.go` — whole package. `RunMigrations` has zero non-test callers; `migrateV2` targets v1 layout end-to-end (deletes `instance/`, creates `worlds/.ritualsync`, calls `migrateLegacyBackups`).
- `internal/config/config.go` — `ServerDir`, `SyncStagingPattern`, `SyncStagingGlob` constants (all consumed only by `subsystems/sync/kit.go`, deleted in Task 5).

### Keep (cross-checked against spec)

- `internal/adapters/mtimescanner.go` + test — spec §1164 names this verbatim as the incremental-walk optimizer for refs Commit on large worlds. Stays.
- `internal/adapters/fullscanner.go` + test — used by `refs.Committer` (cmd/gui + integration harness + spec §1149 walk path). Stays.
- `config.WorldsDir` — workdir subpath for refs Apply/Commit. Stays.
- `domain.FileEntry` — consumed by both scanners as their output type. Stays (moves to its own file `domain/fileentry.go` when `domain.Manifest` is deleted in Task 19).

---

## Task 1: Add `pushing` stage skeleton

**Files:**
- Create: `internal/core/stages/pushing/strategy.go`
- Create: `internal/core/stages/pushing/strategy_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/core/stages/pushing/strategy_test.go`:

```go
package pushing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pushing"
)

type sentinelStrategy struct{ name string; called bool }

func (s *sentinelStrategy) Name() string { return s.name }
func (s *sentinelStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	s.called = true
	return nil, nil
}

type recordingPusher struct {
	calls []domain.RefID
	err   error
}

func (p *recordingPusher) Push(_ context.Context, id domain.RefID) error {
	p.calls = append(p.calls, id)
	return p.err
}

func newRunState() *ritual.RunState {
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func TestPushing_PushesRSRefIDAndRoutesToOnOKOnSuccess(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "2026-04-25T10-00-00.000Z"
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Pushing stage must never return a Go error — failures travel via onFail")
	assert.Equal(t, []domain.RefID{"2026-04-25T10-00-00.000Z"}, pusher.calls,
		"Pushing stage must call Pusher.Push exactly once with rs.RefID — the committing stage wrote that id and pushing is the commit point that makes it visible on remote")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pushing stage must route to onOK after Push succeeds so remote retention runs next and the chain advances toward unlocking")
}

func TestPushing_RecordsPusherErrorAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("push: upload objects/abc: 500 internal")
	pusher := &recordingPusher{err: boom}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "id"
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Pushing stage must record Pusher.Push's error verbatim on rs.Err so the operator sees which blob or ref upload failed")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pushing stage must route to onFail when Push errors — the failure path skips remote retention so a half-uploaded ref is not swept")
}

func TestPushing_RoutesToOnFailWhenContextAlreadyCancelledBeforePush(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	rs.RefID = "id"
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err)
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Pushing stage must record context.Canceled when entered with a cancelled ctx so cancellation surfaces as the failure cause")
	assert.Empty(t, pusher.calls,
		"Pushing stage must not invoke Pusher.Push when ctx is cancelled on entry — no partial upload IO")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Pushing stage must route to onFail on entry-time cancellation")
}

func TestPushing_SkipsPushWhenRefIDEmpty(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	pusher := &recordingPusher{}

	stage := pushing.New(pusher, onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Empty(t, pusher.calls,
		"Pushing stage must skip Pusher.Push when rs.RefID is empty — committing produced no new ref (e.g., nothing changed) so there is nothing to upload")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Pushing stage must route to onOK when rs.RefID is empty so remote retention still runs (idempotent GC over whatever prior sessions left)")
}

func TestPushing_PublishesBatchLifecycleEventsOnTheBus(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	stage := pushing.New(&recordingPusher{}, onOK, onFail)
	rs := &ritual.RunState{Bus: bus, RefID: "id"}

	_, err := stage.Run(t.Context(), rs)
	require.NoError(t, err)

	deadline := time.After(time.Second)
	var sawStart, sawFinish bool
	for !(sawStart && sawFinish) {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for batch lifecycle events: start=%v finish=%v", sawStart, sawFinish)
		case e := <-ch:
			if s, ok := e.(ritual.StartInfo); ok && s.Operation == "push" {
				sawStart = true
			}
			if f, ok := e.(ritual.FinishInfo); ok && f.Operation == "push" {
				sawFinish = true
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -timeout 10s ./internal/core/stages/pushing/...`

Expected: FAIL — package `pushing` does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/stages/pushing/strategy.go`:

```go
// Package pushing uploads rs.RefID and every referenced blob to remote.
// On any failure it records rs.Err and routes to onFail, skipping remote
// retention so a half-uploaded ref is not swept. Mirrors pulling.Strategy.
package pushing

import (
	"context"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// Strategy implements the Pushing stage.
type Strategy struct {
	pusher ports.Pusher
	onOK   machine.Strategy[ritual.RunState]
	onFail machine.Strategy[ritual.RunState]
}

// New builds a Pushing Strategy.
func New(pusher ports.Pusher, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{pusher: pusher, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StagePushing }

// Run pushes rs.RefID. Empty rs.RefID is a no-op success — committing
// produced no new ref so there is nothing to upload. On entry-time ctx
// cancellation the stage short-circuits to onFail.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "push"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	if rs.RefID == "" {
		publish(rs.Bus, ritual.FinishInfo{Operation: "push"})
		return s.onOK, nil
	}
	if err := s.pusher.Push(ctx, rs.RefID); err != nil {
		rs.Err = err
		return s.onFail, nil
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "push"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
```

- [ ] **Step 4: Add `StagePushing` constant**

Edit `internal/core/ritual/stages.go`. Add `StagePushing = "Pushing"` to the const block (keep `StagePublishing`/`StageBackup` — they are removed in a later task).

```go
const (
	StageChecking   = "Checking"
	StagePulling    = "Pulling"
	StageAcquiring  = "Acquiring"
	StageRunning    = "Running"
	StageCommitting = "Committing"
	StagePushing    = "Pushing"
	StagePublishing = "Publishing"
	StageBackup     = "Backup"
	StageUnlocking  = "Unlocking"
	StageRetaining  = "Retaining"
	StageFailed     = "Failed"
	StageDone       = "Done"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -timeout 10s ./internal/core/stages/pushing/...`

Expected: PASS (five tests).

- [ ] **Step 6: Commit**

```bash
git add internal/core/stages/pushing/ internal/core/ritual/stages.go
git commit -m "feat(stages): add pushing stage wrapping ports.Pusher"
```

---

## Task 2: Add `CommitOptsResolver` helper in app package

**Files:**
- Create: `internal/app/commitopts.go`
- Create: `internal/app/commitopts_test.go`

The resolver encodes the policy documented in `committing/strategy.go` doc (amend a live-ticker draft when `rs.RefID != ""`, else fresh commit parented on pulled HEAD from `rs.ParentRefID`). Kept in the `app` package because it is a composition-root concern, not a stage-level concern.

- [ ] **Step 1: Write the failing test**

Create `internal/app/commitopts_test.go`:

```go
package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ritual/internal/app"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

func TestNewCommitOptsResolver_FreshCommitUsesParentRefIDAsParent(t *testing.T) {
	resolver := app.NewCommitOptsResolver([]string{"world/**"})
	rs := &ritual.RunState{ParentRefID: "pulled-head"}

	got := resolver(rs)

	assert.Equal(t, ports.CommitOpts{
		Parent:  domain.RefID("pulled-head"),
		Targets: []string{"world/**"},
	}, got,
		"CommitOptsResolver with empty rs.RefID must build a fresh-commit opts set with Parent=rs.ParentRefID — the pulled HEAD becomes the new ref's predecessor so history is linear")
}

func TestNewCommitOptsResolver_AmendCollapsesLiveTickerDraft(t *testing.T) {
	resolver := app.NewCommitOptsResolver([]string{"world/**"})
	rs := &ritual.RunState{RefID: "draft-from-ticker", ParentRefID: "pulled-head"}

	got := resolver(rs)

	assert.Equal(t, ports.CommitOpts{
		Amend:   domain.RefID("draft-from-ticker"),
		Targets: []string{"world/**"},
	}, got,
		"CommitOptsResolver with non-empty rs.RefID must build an amend opts set (Amend=rs.RefID, Parent unset) so the post-session commit collapses into the live-ticker draft instead of forking a sibling ref (spec §1435)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -timeout 10s ./internal/app/... -run TestNewCommitOptsResolver`

Expected: FAIL — `app.NewCommitOptsResolver` is undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/app/commitopts.go`:

```go
package app

import (
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/committing"
)

// NewCommitOptsResolver builds the composition-root resolver for the
// committing stage. Policy (spec §1435, committing doc §amend-push
// collapse):
//   - rs.RefID != ""  → Amend=rs.RefID. A live-ticker draft exists for
//     this session; the post-session commit collapses into it.
//   - rs.RefID == ""  → fresh commit parented on rs.ParentRefID (the
//     pulled HEAD). No ticker ever ran, so no draft exists.
func NewCommitOptsResolver(targets []string) committing.OptsResolver {
	return func(rs *ritual.RunState) ports.CommitOpts {
		if rs.RefID != "" {
			return ports.CommitOpts{Amend: rs.RefID, Targets: targets}
		}
		return ports.CommitOpts{Parent: rs.ParentRefID, Targets: targets}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -timeout 10s ./internal/app/... -run TestNewCommitOptsResolver`

Expected: PASS (two tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/commitopts.go internal/app/commitopts_test.go
git commit -m "feat(app): NewCommitOptsResolver encodes amend-vs-fresh policy"
```

---

## Task 3: Extend `app.New` signature; drop `exitUpdaters`; rewire chain to spec

**Files:**
- Modify: `internal/app/ritual.go`

Target chain per spec §2267 and §2297–2309:

```
checking → pulling → acquiring → running → committing → pruning(local) → pushing → pruning(remote) → unlocking → ∎
```

`failed` back-edges per origin stage.

- [ ] **Step 1: Write the failing test — chain order assertion**

Edit `internal/app/ritual_test.go`. Locate the test that asserts stage ordering (grep for `stageSequence` or `StagePulling`; adjust if absent). Add or replace with:

```go
func TestRitual_BuildChain_EmitsCheckPullAcquireRunCommitRetainPushRetainUnlockInOrder(t *testing.T) {
	// Builds the Ritual with no-op ports and drives a clean run, asserting
	// the stage-order contract from spec §2267 is preserved by buildChain.
	// Load-bearing assertion: committing must run before pushing, and each
	// retention instance must pair with its triggering verb.
	t.Skip("pending: drive Ritual with fakes — see ritual_integration_test.go for pattern; assertion: stageSequence == [Checking Pulling Acquiring Running Committing Retaining Pushing Retaining Unlocking]")
}
```

(The skipped placeholder reserves the assertion. The real coverage lives in the integration tests rewritten in Task 8.)

- [ ] **Step 2: Rewrite `app.Ritual` fields and constructor**

Edit `internal/app/ritual.go`. Replace the `Ritual` struct body:

```go
type Ritual struct {
	bus              ports.EventBus
	localStorage     ports.StorageRepository
	remoteStorage    ports.StorageRepository
	localManifests   ports.ManifestStore
	remoteManifests  ports.ManifestStore
	checks           []checks.Check
	puller           ports.Puller
	applier          ports.Applier
	headResolver     pulling.HeadResolver
	committer        ports.Committer
	pusher           ports.Pusher
	commitTargets    []string
	localRetentions  []retaining.Job
	remoteRetentions []retaining.Job
	cmdBuilder       ports.CmdBuilder
	readiness        ports.ReadinessCheck
	locker           *observed.Locker

	entry    machine.Strategy[ritual.RunState]
	runner   *ritual.Runner
	status   Outcome
	cancel   context.CancelFunc
	userStop atomic.Bool
}
```

Replace `New` signature — drop `exitUpdaters`, insert `committer`, `pusher`, `commitTargets`:

```go
func New(
	bus ports.EventBus,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	localManifests ports.ManifestStore,
	remoteManifests ports.ManifestStore,
	preflightChecks []checks.Check,
	puller ports.Puller,
	applier ports.Applier,
	headResolver pulling.HeadResolver,
	committer ports.Committer,
	pusher ports.Pusher,
	commitTargets []string,
	localRetentions, remoteRetentions []retaining.Job,
	cmdBuilder ports.CmdBuilder,
	readiness ports.ReadinessCheck,
) *Ritual {
	host, _ := os.Hostname()
	r := &Ritual{
		bus:              bus,
		localStorage:     localStorage,
		remoteStorage:    remoteStorage,
		localManifests:   localManifests,
		remoteManifests:  remoteManifests,
		checks:           preflightChecks,
		puller:           puller,
		applier:          applier,
		headResolver:     headResolver,
		committer:        committer,
		pusher:           pusher,
		commitTargets:    commitTargets,
		localRetentions:  localRetentions,
		remoteRetentions: remoteRetentions,
		cmdBuilder:       cmdBuilder,
		readiness:        readiness,
		locker:           observed.NewLocker(lock.New(remoteStorage, host), bus),
		status:           Idle,
	}
	r.entry = r.buildChain()
	return r
}
```

- [ ] **Step 3: Rewrite `buildChain` to spec order**

Replace the body of `buildChain`:

```go
func (r *Ritual) buildChain() machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failPull := failed.New(ritual.StagePulling)
	failAcq := failed.New(ritual.StageAcquiring)
	failCommit := failed.New(ritual.StageCommitting)
	failPush := failed.New(ritual.StagePushing)
	failRet := failed.New(ritual.StageRetaining)

	unlock := unlocking.New(r.locker.Release, nil)
	pruneRemote := retaining.New(r.remoteRetentions, r.bus, failRet, unlock)
	push := pushing.New(r.pusher, pruneRemote, failPush)
	pruneLocal := retaining.New(r.localRetentions, r.bus, failRet, push)
	commit := committing.New(r.committer, NewCommitOptsResolver(r.commitTargets), pruneLocal, failCommit)
	run := running.New(r.cmdBuilder, r.readiness, commit, unlock)
	acquire := acquiring.New(r.locker.Acquire, r.locker.Inspect, r.localManifests.Get, r.locker.HeartbeatInterval(), run, failAcq)
	pull := pulling.New(r.puller, r.applier, r.headResolver, acquire, failPull)
	check := checking.New(r.checks, pull, failCheck)

	failCheck.SetRetry(check)
	failPull.SetRetry(pull)
	failAcq.SetRetry(acquire)
	failCommit.SetRetry(commit)
	failPush.SetRetry(push)
	failRet.SetRetry(pruneLocal)

	return check
}
```

Update the imports block of `internal/app/ritual.go`:
- Remove: `"ritual/internal/core/stages/backup"`, `"ritual/internal/core/stages/publishing"`.
- Add: `"ritual/internal/core/stages/committing"`, `"ritual/internal/core/stages/pushing"`.

- [ ] **Step 4: Build the package**

Run: `go build ./internal/app/...`

Expected: FAIL in composition roots (`cmd/cli/main.go`, `cmd/gui/main.go`, `ritual_integration_test.go`) because signatures changed. That is the next tasks' work. For this task, `go build ./internal/app` alone must succeed.

Run: `go build ./internal/app`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/ritual.go internal/app/ritual_test.go
git commit -m "refactor(app): wire committing→prune→push→prune→unlock chain per spec §2267"
```

---

## Task 4: Update `cmd/gui` composition root

**Files:**
- Modify: `cmd/gui/main.go`

- [ ] **Step 1: Remove the `syncSvc`/`uploader` block**

Edit `cmd/gui/main.go`. Delete lines 233–241 (the `stagingDir`/`syncSvc`/`uploader` block). The new chain no longer consumes `UpdaterService`.

- [ ] **Step 2: Add Committer + Pusher construction**

After the existing `puller`/`applier`/`headResolver` construction (around line 231), add:

```go
committer := refs.NewCommitter(scanner, workdirStorage, localStorage, runner)
pusher := refs.NewPusher(localStorage, remoteStorage, runner)
commitTargets := []string{"**"}
```

- [ ] **Step 3: Update `app.New` call**

Replace the existing `app.New(...)` call with the new signature:

```go
r := app.New(
	bus,
	localStorage, remoteStorage,
	localManifests, remoteManifests,
	nil, // no conditions for POC
	puller, applier, headResolver,
	committer, pusher, commitTargets,
	localRets, remoteRets,
	cmdBuilder,
	readiness,
)
```

- [ ] **Step 4: Drop unused imports**

Remove `"ritual/internal/core/services"` if no longer referenced in `cmd/gui/main.go` (grep the file first). Leave `"ritual/internal/subsystems/retention"` — still used.

- [ ] **Step 5: Verify build**

Run: `go build ./cmd/gui`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/gui/main.go
git commit -m "refactor(gui): wire Committer+Pusher, drop exitUpdaters"
```

---

## Task 5: Update `cmd/cli` composition root

**Files:**
- Modify: `cmd/cli/main.go`
- Modify: `internal/subsystems/sync/kit.go`

The CLI reads its deps from `subsystems/sync.Build` (the Kit). Two steps: shrink the Kit (drop `ExitUpdaters`, add `Committer`/`Pusher`/`CommitTargets`), then update the CLI.

- [ ] **Step 1: Modify `Kit` struct**

Edit `internal/subsystems/sync/kit.go`. Replace the `Kit` struct:

```go
type Kit struct {
	Puller        ports.Puller
	Applier       ports.Applier
	HeadResolver  pulling.HeadResolver
	Committer     ports.Committer
	Pusher        ports.Pusher
	CommitTargets []string
	WorldSync     ports.SyncService
}
```

- [ ] **Step 2: Populate the new `Kit` fields in `Build`**

In `sync.Build`, after `applier := refs.NewApplier(...)`, add:

```go
committer := refs.NewCommitter(worldScanner, workdirStorage, localStorage, runner)
pusher := refs.NewPusher(localStorage, remoteStorage, runner)
```

Replace the `return Kit{...}` block:

```go
return Kit{
	Puller:        puller,
	Applier:       applier,
	HeadResolver:  headResolver,
	Committer:     committer,
	Pusher:        pusher,
	CommitTargets: []string{"**"},
	WorldSync:     worldSync,
}, nil
```

Delete the lines that built `worldUp` / `worldUploader`. `services.NewSyncUploader` is going away in Task 10 — no new callers.

- [ ] **Step 3: Update CLI `app.New` call**

Edit `cmd/cli/main.go`. Around line 140–144:

```go
r := app.New(
	bus,
	localStorage, remoteStorage,
	localManifests, remoteManifests,
	preflightChecks,
	sk.Puller, sk.Applier, sk.HeadResolver,
	sk.Committer, sk.Pusher, sk.CommitTargets,
	localRets, remoteRets,
	cmdBuilder,
	readiness,
)
```

- [ ] **Step 4: Verify build**

Run: `go build ./cmd/cli ./internal/subsystems/sync`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/main.go internal/subsystems/sync/kit.go
git commit -m "refactor(cli): thread Committer+Pusher through sync.Kit, drop ExitUpdaters"
```

---

## Task 6: Update integration harness to provide Committer/Pusher

**Files:**
- Modify: `internal/app/ritual_integration_test.go` (harness only — story rewrites in Task 8)

- [ ] **Step 1: Extend `testRitual` fields**

Edit `internal/app/ritual_integration_test.go`. Remove the comment about "Optional prune jobs" that references `retaining`. Locate the `startRitualFull` method.

- [ ] **Step 2: Replace the `app.New` call**

Replace the body of `startRitualFull` after `puller, applier, headResolver := r.buildPullingVerbs(worldsPath, scanner)` with:

```go
runner := adapters.NewSerialRunner()
worldsRoot, err := os.OpenRoot(worldsPath)
require.NoError(t, err, "open worlds root for committer")
t.Cleanup(func() { worldsRoot.Close() })
workdirStorage, err := adapters.NewFSRepository(worldsRoot, "workdir-commit")
require.NoError(t, err, "workdir storage for committer")
committer := refs.NewCommitter(scanner, workdirStorage, r.local, runner)
pusher := refs.NewPusher(r.local, r.remote, runner)
commitTargets := []string{"**"}

cmdBuilder := &fakeServerCmdBuilder{server: server}

ritual := app.New(
	r.bus,
	r.local, r.remote,
	r.localManifests, r.remoteManifests,
	preflightChecks,
	puller, applier, headResolver,
	committer, pusher, commitTargets,
	r.localRetentions, r.remoteRetentions,
	cmdBuilder,
	immediateReady{},
)

ritual.Listen(r.ctx)
r.bus.Publish(app.StartRequested{})
return server
```

Delete the `syncSvc` / `downloader` / `uploader` / `getState` block directly above — it was only feeding `[]UpdaterService{uploader}`.

- [ ] **Step 3: Verify the harness compiles**

Run: `go vet ./internal/app/...`

Expected: vet errors only for tests that assert backup/publishing behaviour (cleaned in Task 8). No errors in harness itself.

- [ ] **Step 4: Commit (WIP)**

```bash
git add internal/app/ritual_integration_test.go
git commit -m "refactor(integration-harness): wire Committer+Pusher in testRitual"
```

---

## Task 7: Delete publishing stage package

**Files:**
- Delete: `internal/core/stages/publishing/`
- Modify: `internal/core/ritual/stages.go`

- [ ] **Step 1: Verify no remaining callers**

Run: `grep -rn "stages/publishing\|StagePublishing" --include="*.go"`

Expected output lists only `internal/core/stages/publishing/strategy.go`, `internal/core/ritual/stages.go`, and the places cleaned in Tasks 8/9. If anything outside those scopes appears, stop and investigate.

- [ ] **Step 2: Delete the package**

Run: `git rm -r internal/core/stages/publishing`

- [ ] **Step 3: Delete the constant**

Edit `internal/core/ritual/stages.go`. Remove the line `StagePublishing = "Publishing"`.

- [ ] **Step 4: Verify build**

Run: `go build ./...`

Expected: errors only in backup stage and projection — cleaned next tasks.

- [ ] **Step 5: Commit**

```bash
git add internal/core/ritual/stages.go
git rm -r internal/core/stages/publishing
git commit -m "chore(stages): delete publishing stage — replaced by pushing"
```

---

## Task 8: Delete backup stage package

**Files:**
- Delete: `internal/core/stages/backup/`
- Modify: `internal/core/ritual/stages.go`

- [ ] **Step 1: Delete the package**

Run: `git rm -r internal/core/stages/backup`

- [ ] **Step 2: Delete the constant**

Edit `internal/core/ritual/stages.go`. Remove the line `StageBackup = "Backup"`. The block should now read:

```go
const (
	StageChecking   = "Checking"
	StagePulling    = "Pulling"
	StageAcquiring  = "Acquiring"
	StageRunning    = "Running"
	StageCommitting = "Committing"
	StagePushing    = "Pushing"
	StageUnlocking  = "Unlocking"
	StageRetaining  = "Retaining"
	StageFailed     = "Failed"
	StageDone       = "Done"
)
```

- [ ] **Step 3: Verify build (excluding tests)**

Run: `go build ./...`

Expected: PASS. Test files still fail — addressed in Task 9.

- [ ] **Step 4: Commit**

```bash
git add internal/core/ritual/stages.go
git rm -r internal/core/stages/backup
git commit -m "chore(stages): delete backup stage — history lives in refs/ per spec §413"
```

---

## Task 9: Rewrite integration tests — delete backup stories, add push stories

**Files:**
- Modify: `internal/app/ritual_integration_test.go`
- Modify: `internal/gui/projection/projection.go`
- Modify: `internal/gui/projection/projection_test.go`

- [ ] **Step 1: Delete backup-specific tests and helpers**

Edit `internal/app/ritual_integration_test.go`. Delete the following (function names, grep to locate):

- `TestIntegration_BackupCreated_ContainsPostRunState`
- `TestIntegration_NothingChanged_NoBackupCreated`
- `TestIntegration_BackupUsesSameStorageOnly_NoRemoteReadDuringBackup`
- `TestIntegration_BackupEmitsStorageCopyEventsWithBackupsPrefix`
- `TestIntegration_ServerCrash_SkipsPublishAndBackup`
- `TestIntegration_PipelineOrder_MatchesCheckFetchAcquireRunPublishBackupUnlockRetain`
- `TestIntegration_BackupCopyError_EmitsErrorInfo_LockStillReleased`
- Helpers: `assertBackupExists`, `assertNoBackup`, `assertBackupFileContent`, `assertBackupHasManifest`, `assertBackupCount`, `latestBackupName`, `filterCopyInfoWithDstPrefix`, `hasBackupErrorInfo`, `scriptedStorage` (only if no other test uses it — grep first).

- [ ] **Step 2: Add the pipeline-order story covering the new chain**

Append to `internal/app/ritual_integration_test.go`:

```go
func TestIntegration_PipelineOrder_MatchesCheckPullAcquireRunCommitRetainPushRetainUnlock(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(r.bus)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	events := drain()

	want := []string{
		stagenames.StageChecking,
		stagenames.StagePulling,
		stagenames.StageAcquiring,
		stagenames.StageRunning,
		stagenames.StageCommitting,
		stagenames.StageRetaining,
		stagenames.StagePushing,
		stagenames.StageRetaining,
		stagenames.StageUnlocking,
	}
	assert.Equal(t, want, stageSequence(events),
		"post-session chain per spec §2267: commit writes local ref, local prune sweeps orphan blobs before they escape, push uploads ref+blobs, remote prune reaps once remote is authoritative, unlock last")
}

func TestIntegration_ChangesUploaded_RefAppearsOnRemote(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("after run"))
	server.exit(0)
	server.stdin.Close()
	r.waitDone(t)

	keys, err := r.remote.List(r.ctx, "refs/")
	require.NoError(t, err, "list remote refs after session")
	assert.GreaterOrEqual(t, len(keys), 2,
		"remote must carry at least the seeded ref and the session's newly pushed ref — otherwise pushing stage dropped the commit")
}

func TestIntegration_ServerCrash_SkipsCommitAndPush(t *testing.T) {
	r := newRitual(t)

	seedRemoteWorld(t, r,
		file("world/level.dat", []byte("before run")),
	)

	drain := collectBusEvents(r.bus)

	server := r.startRitual(t)
	server.waitReady(t)
	server.write("worlds/world/level.dat", []byte("mid crash"))
	server.exit(1)
	server.stdin.Close()
	r.waitFailed(t)

	events := drain()

	stages := stageSequence(events)
	assert.NotContains(t, stages, stagenames.StageCommitting,
		"server crash (exit code != 0) must skip Committing — mid-mutation workdir is not a safe snapshot source")
	assert.NotContains(t, stages, stagenames.StagePushing,
		"server crash must skip Pushing — nothing was committed, nothing to push")
	r.assertManifestUnlocked(t,
		"crash path still must release the lock via Unlocking")
}
```

- [ ] **Step 3: Update `TestIntegration_Prune_BothInstancesExecute`**

The existing test at line 1303 still passes conceptually (two `retain` StartInfo events) but the chain order changed. Update the assertion message:

```go
assert.Equal(t, 2, starts,
	"retain StartInfo must fire twice per run — once paired with committing (local prune), once paired with pushing (remote prune) per spec §2297. starts!=2 means one retaining.Strategy instance was dropped from buildChain")
```

- [ ] **Step 4: Update projection mapping**

Edit `internal/gui/projection/projection.go`. Replace the post-run case block in `StateChanged`:

```go
case ritual.StageCommitting, ritual.StagePushing, ritual.StageUnlocking, ritual.StageRetaining:
	p.state.Stage = StageUploading
	p.state.Label = uploadLabel(to)
	p.state.ReadyLight = false
```

Replace `uploadLabel`:

```go
func uploadLabel(stage string) string {
	switch stage {
	case ritual.StageCommitting:
		return "Snapshotting…"
	case ritual.StagePushing:
		return "Uploading…"
	case ritual.StageUnlocking:
		return "Releasing lock…"
	case ritual.StageRetaining:
		return "Pruning old refs…"
	}
	return "Finishing…"
}
```

- [ ] **Step 5: Update projection test**

Edit `internal/gui/projection/projection_test.go`. Locate `TestProjection_StateChangedToBackup_FlipsStageToUploading` and replace with two tests:

```go
func TestProjection_StateChangedToCommitting_FlipsStageToUploadingWithSnapshotLabel(t *testing.T) {
	final := lastView(runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StageCommitting})
	}))
	assert.Equal(t, projection.StageUploading, final.Stage,
		"Committing state must map to Uploading UI stage — user sees one 'uploading' screen for the whole post-game persistence phase")
	assert.Equal(t, "Snapshotting…", final.Label,
		"Committing stage must carry a 'Snapshotting…' label so the user understands which post-game step is running (local ref creation, not upload)")
}

func TestProjection_StateChangedToPushing_FlipsStageToUploadingWithUploadingLabel(t *testing.T) {
	final := lastView(runProjection(t, nil, func(bus ports.EventBus) {
		bus.Publish(ritual.StateChangedInfo{To: ritual.StagePushing})
	}))
	assert.Equal(t, projection.StageUploading, final.Stage,
		"Pushing state must map to Uploading UI stage")
	assert.Equal(t, "Uploading…", final.Label,
		"Pushing stage must carry an 'Uploading…' label — this is the network-upload step")
}
```

(If `lastView` does not already exist in the test file, reuse whatever the existing test used — they call it `final`, not `lastView`. Adjust to the file's convention.)

- [ ] **Step 6: Run all affected tests**

Run: `go test -timeout 30s ./internal/app/... ./internal/gui/projection/...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/ritual_integration_test.go internal/gui/projection/
git commit -m "test(pipeline): rewrite backup/publish stories as commit/push stories"
```

---

## Task 10: Delete `UpdaterService` port and its implementations

**Files:**
- Modify: `internal/core/ports/ports.go`
- Delete: `internal/core/ports/mocks/updater.go`, `internal/core/ports/mocks/updater_test.go`
- Delete: `internal/core/services/updater_ritual.go`, `internal/core/services/updater_ritual_test.go` (if present)
- Modify: `internal/core/services/sync_updater.go` (delete whole file's `SyncDownloadUpdater` and `SyncUploader` types)

- [ ] **Step 1: Verify `UpdaterService` has no remaining callers**

Run: `grep -rn "UpdaterService\|SyncUploader\|SyncDownloadUpdater\|RitualUpdater" --include="*.go"`

Expected: matches only inside the files listed above. If anything else appears, stop and investigate.

- [ ] **Step 2: Delete the mock package**

Run: `git rm internal/core/ports/mocks/updater.go internal/core/ports/mocks/updater_test.go`

- [ ] **Step 3: Delete `updater_ritual.go` + test**

Run:

```bash
git rm internal/core/services/updater_ritual.go
test -f internal/core/services/updater_ritual_test.go && git rm internal/core/services/updater_ritual_test.go
```

- [ ] **Step 4: Delete `sync_updater.go` entirely**

`SyncDownloadUpdater` and `SyncUploader` are the only exported symbols in the file, and both exist solely to wrap `SyncService` into `UpdaterService`. With the port gone, the whole file is dead.

Run: `git rm internal/core/services/sync_updater.go`

Also remove any tests tied to those types:

Run: `grep -l "SyncUploader\|SyncDownloadUpdater" internal/core/services/*_test.go` — delete each file returned (they are story-free tests of a dead wrapper).

- [ ] **Step 5: Delete the port interface**

Edit `internal/core/ports/ports.go`. Delete the `UpdaterService` block (the interface and its doc comment above it). The block begins with `// UpdaterService defines the interface for update operations` around line 114.

- [ ] **Step 6: Verify build**

Run: `go build ./... && go vet ./...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/ports/ports.go
git rm internal/core/ports/mocks/updater*.go internal/core/services/updater_ritual*.go internal/core/services/sync_updater.go
git commit -m "chore(ports): delete UpdaterService port and v1 updater impls"
```

---

## Task 11: Delete `services.CreateBackup` family and `config.BackupsDir`

**Files:**
- Delete: `internal/core/services/backup.go`, `internal/core/services/backup_integration_test.go`
- Modify: `internal/core/services/migration.go` (remove `migrateLegacyBackups` + its caller)
- Modify: `internal/core/services/migration_test.go` (remove `migrateLegacyBackups` test)
- Modify: `internal/config/config.go` (remove `BackupsDir`)

- [ ] **Step 1: Delete `backup.go` + its integration test**

Run: `git rm internal/core/services/backup.go internal/core/services/backup_integration_test.go`

- [ ] **Step 2: Remove `migrateLegacyBackups` from migration.go**

Edit `internal/core/services/migration.go`. Delete the `migrateLegacyBackups` function (lines 70–94 in the current file). Find and delete the call site within the `Migrate` top-level function in the same file (grep for `migrateLegacyBackups(`). The removed call did on-first-run `world_backups/` → `backups/` renames; with backup stage gone, the target dir has no writer, so this migration is a no-op that silently bloats user data directories.

- [ ] **Step 3: Remove corresponding test**

Edit `internal/core/services/migration_test.go`. Delete the test that exercises `migrateLegacyBackups` (grep for `world_backups` or `BackupsDir`).

- [ ] **Step 4: Remove `BackupsDir` constant**

Edit `internal/config/config.go`. Delete the line `BackupsDir = "backups" // unified local/R2 backup prefix`.

- [ ] **Step 5: Verify build**

Run: `grep -rn "config.BackupsDir\|services.CreateBackup\|migrateLegacyBackups" --include="*.go"`

Expected: empty output. If matches remain, each one is a dead reference — delete it.

Run: `go build ./... && go vet ./... && go test -timeout 30s ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/services/migration.go internal/core/services/migration_test.go internal/config/config.go
git rm internal/core/services/backup.go internal/core/services/backup_integration_test.go
git commit -m "chore(services): delete CreateBackup, legacy-backup migration, BackupsDir — refs are the history"
```

---

## Task 12: Gut `heartbeat.Supervisor` — delete sync-tick + manifest plumbing

**Files:**
- Modify: `internal/subsystems/heartbeat/supervisor.go`
- Modify: `internal/subsystems/heartbeat/supervisor_test.go`

The supervisor needs only the bus, the `HeartbeatFn` (already a closure over `lock.Locker` via `r.Heartbeat`), and the per-run cancellation map. All manifest-store + SyncService reads/writes + sync-readiness atomics go away.

- [ ] **Step 1: Delete sync-tick tests**

Edit `internal/subsystems/heartbeat/supervisor_test.go`. Delete every test that asserts on `SyncService.Upload`, `SaveRequested`/`SaveCompleted` handshake, or manifest reads/writes. Grep: `SyncService`, `SaveRequested`, `SaveCompleted`, `Upload`, `syncer`, `localStore`, `remoteStore`. Delete the related `emptyStore()`, `noopSyncer()`, `localStore/remoteStore` fixtures and any `syncSvc` helpers too. Keep tests that cover lease refresh + cancellation via `LockAcquiredInfo` / `LockReleasedInfo` / server lifecycle events.

- [ ] **Step 2: Delete sync-tick tests — verify they fail at compile, not at assertion**

Run: `go test -timeout 10s ./internal/subsystems/heartbeat/...`

Expected: build errors because `noopSyncer`/`emptyStore` helpers were deleted and the old tests referenced them. That is what the next step fixes.

- [ ] **Step 3: Shrink `Supervisor` struct + `Attach` signature**

Edit `internal/subsystems/heartbeat/supervisor.go`. Replace the `Supervisor` struct:

```go
type Supervisor struct {
	heartbeat HeartbeatFn
	bus       ports.EventBus

	mu         sync.Mutex
	active     map[string]context.CancelFunc
	sessionIDs map[string]string
}
```

Replace `Attach`:

```go
func Attach(bus ports.EventBus, heartbeat HeartbeatFn) (*Supervisor, func()) {
	s := &Supervisor{
		heartbeat:  heartbeat,
		bus:        bus,
		active:     map[string]context.CancelFunc{},
		sessionIDs: map[string]string{},
	}

	ch, cancelSub := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			s.handle(e)
		}
	}()
	cancel := func() {
		cancelSub()
		<-done
		s.stopAll()
	}
	return s, cancel
}
```

- [ ] **Step 4: Strip `handle` to lease-refresh only**

Replace `handle`:

```go
func (s *Supervisor) handle(e ports.Event) {
	switch ev := e.(type) {
	case ritual.LockAcquiredInfo:
		s.start(ev.RunID, ev.SessionID, ev.Interval)
	case ritual.LockReleasedInfo:
		s.stop(ev.RunID)
	case running.ServerStoppedInfo, running.ServerCrashedInfo:
		s.stopAll()
	}
}
```

Delete `ServerReadyInfo`, `ServerOutputInfo` branches (they existed only to drive `syncCtx`).

- [ ] **Step 5: Strip `tick` to lease refresh only**

Replace `tick`:

```go
func (s *Supervisor) tick(ctx context.Context, runID string) {
	s.mu.Lock()
	sessionID := s.sessionIDs[runID]
	s.mu.Unlock()
	if sessionID == "" {
		return
	}

	if err := s.heartbeat(ctx, sessionID); err != nil {
		switch {
		case errors.Is(err, lock.ErrLeaseTakenOver):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "taken_over"})
			s.stop(runID)
		case errors.Is(err, lock.ErrLeaseVanished):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "vanished"})
			s.stop(runID)
		case errors.Is(err, lock.ErrLeaseLost):
			s.bus.Publish(ritual.LockLostInfo{RunID: runID, Reason: "lease_lost"})
			s.stop(runID)
		default:
			s.bus.Publish(ritual.ErrorInfo{Operation: "heartbeat", Err: err})
		}
	}
}
```

Delete `syncTick`, `cancelSync`, `syncReady`, `syncCtx`, `syncCancel`, `saveWaitTimeout`, `SaveRequested`/`SaveCompleted` handling.

- [ ] **Step 6: Drop imports no longer used**

Remove: `"ritual/internal/core/stages/running"` if the only remaining reference was `ServerReadyInfo`/`ServerOutputInfo`. Keep it if the remaining `ServerStoppedInfo`/`ServerCrashedInfo` branches still reference types from that package (most likely yes — leave it).

Remove: `"sync/atomic"`, `"strings"`, `"time"` if `saveWaitTimeout` was the only consumer. Keep `"time"` if `beat` / `tick` still use `time.NewTicker`.

- [ ] **Step 7: Run heartbeat tests**

Run: `go test -timeout 10s ./internal/subsystems/heartbeat/...`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/subsystems/heartbeat/
git commit -m "refactor(heartbeat): drop v1 sync-tick + manifest plumbing — lock lives outside manifest"
```

---

## Task 13: Update `cmd/cli` heartbeat.Attach call and drop manifests from app.New

**Files:**
- Modify: `cmd/cli/main.go`

- [ ] **Step 1: Update `heartbeat.Attach` call**

Replace:

```go
_, stopHeartbeat = heartbeat.Attach(bus, r.Heartbeat, localManifests, remoteManifests, sk.WorldSync)
```

With:

```go
_, stopHeartbeat = heartbeat.Attach(bus, r.Heartbeat)
```

Update the nearby comment referencing "needs WorldSync from kit" — delete it.

- [ ] **Step 2: Drop manifest construction**

Grep for `localManifests` / `remoteManifests` in `cmd/cli/main.go`. The construction (`adapters.NewManifestStore(...)`) and any `ensureManifest` seed call should be deleted if nothing references them after Step 1. Run `grep -n "manifest\|Manifest" cmd/cli/main.go` — everything should be deletable except log/error strings that mention the word in passing.

- [ ] **Step 3: Update `app.New` call**

Remove `localManifests, remoteManifests` from the argument list (Task 3 already removed them from the signature, but the CLI still passes them).

- [ ] **Step 4: Build**

Run: `go build ./cmd/cli`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/main.go
git commit -m "refactor(cli): drop manifest stores + WorldSync from app.New and heartbeat.Attach"
```

---

## Task 14: Drop manifests from cmd/gui composition root

**Files:**
- Modify: `cmd/gui/main.go`

- [ ] **Step 1: Delete manifest construction + seed**

Delete the block that constructs `localManifests` / `remoteManifests` via `adapters.NewManifestStore` (around lines 193–201) and the `ensureManifest` function at the bottom. Grep for any remaining `Manifests` / `ManifestStore` / `ensureManifest` references and delete them.

- [ ] **Step 2: Delete the retention dep on `remoteManifest`**

The `retention.Build` call currently passes a `remoteManifest *domain.Manifest`. Task 15 rewrites `retention.Build` to stop requiring a manifest. For this task, provide a no-op placeholder or hold the build break until Task 15 lands — easier to batch: skip Step 2 here and come back after Task 15.

- [ ] **Step 3: Strip `app.New` manifest arguments**

Reduce:

```go
r := app.New(
	bus,
	localStorage, remoteStorage,
	localManifests, remoteManifests,
	nil,
	puller, applier, headResolver,
	committer, pusher, commitTargets,
	localRets, remoteRets,
	cmdBuilder,
	readiness,
)
```

To:

```go
r := app.New(
	bus,
	localStorage, remoteStorage,
	nil,
	puller, applier, headResolver,
	committer, pusher, commitTargets,
	localRets, remoteRets,
	cmdBuilder,
	readiness,
)
```

- [ ] **Step 4: Build**

Run: `go build ./cmd/gui`

Expected: FAIL on retention call (deferred to Task 15). Ignore this until Task 15.

- [ ] **Step 5: Commit (WIP — will green up after Task 15)**

```bash
git add cmd/gui/main.go
git commit -m "refactor(gui): drop manifest stores from composition root"
```

---

## Task 15: Remove manifest dep from retention subsystem

**Files:**
- Modify: `internal/subsystems/retention/retention.go`

- [ ] **Step 1: Read current `Build` signature**

`retention.Build(localStorage, remoteStorage, bus, remoteManifest)` currently accepts a `*domain.Manifest` for reasons that must be one of (a) retention rules live on the manifest, or (b) retention keys derive from `XXHashMap`. With refs-based history, rules should come from settings (spec §497) and keys live under `refs/` and `objects/`. Open the file and confirm what `remoteManifest` is actually consulted for.

- [ ] **Step 2: Replace the manifest dep with a settings-shaped value**

If `remoteManifest` was only used to pull retention rules: replace with `domain.RetentionRules` passed directly from the caller (cmd/cli, cmd/gui both already load `settings := domain.LoadSettings()`). The new signature:

```go
func Build(
	localStorage, remoteStorage ports.StorageRepository,
	bus ports.EventBus,
	rules domain.RetentionRules,
) (local, remote []retaining.Job, err error) { ... }
```

If `remoteManifest` was used for `XXHashMap`: that path is v1 and irrelevant — delete the branch.

- [ ] **Step 3: Update callers**

`cmd/cli/main.go` + `cmd/gui/main.go` — pass `settings.Retention` (or whatever field holds rules) instead of `remoteManifest`.

- [ ] **Step 4: Build everything**

Run: `go build ./...`

Expected: PASS. This closes the loop left open by Task 14.

- [ ] **Step 5: Commit**

```bash
git add internal/subsystems/retention/ cmd/cli/main.go cmd/gui/main.go
git commit -m "refactor(retention): take RetentionRules directly, drop *domain.Manifest"
```

---

## Task 16: Drop `localManifests` / `remoteManifests` from `app.Ritual` and `acquiring`

**Files:**
- Modify: `internal/app/ritual.go`
- Modify: `internal/core/stages/acquiring/strategy.go`
- Modify: `internal/core/stages/acquiring/strategy_test.go`
- Modify: `internal/core/ritual/runstate.go`
- Modify: `internal/app/ritual_integration_test.go`

- [ ] **Step 1: Delete `SnapshotLocalFn` from acquiring**

Edit `internal/core/stages/acquiring/strategy.go`. Delete the `SnapshotLocalFn` type and the `snapshotLocal` field. Update `New` to drop that parameter:

```go
func New(
	acquire AcquireFn,
	inspect InspectFn,
	interval time.Duration,
	onOK, onFail machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{acquire: acquire, inspect: inspect, interval: interval, onOK: onOK, onFail: onFail}
}
```

Delete the snapshot block from `Run` (the `if s.snapshotLocal != nil {...}` branch).

- [ ] **Step 2: Drop domain import**

Remove `"ritual/internal/core/domain"` from `acquiring/strategy.go` if no longer referenced.

- [ ] **Step 3: Update acquiring tests**

Edit `internal/core/stages/acquiring/strategy_test.go`. Delete `SnapshotLocalFn` fixtures and any assertions on `rs.LocalBefore`. Callsites drop the arg.

- [ ] **Step 4: Delete `LocalBefore` / `RemoteBefore` from RunState**

Edit `internal/core/ritual/runstate.go`. Delete both fields and the related doc comment. Drop the `"ritual/internal/core/domain"` import if unused.

- [ ] **Step 5: Drop manifest params from `app.Ritual`**

Edit `internal/app/ritual.go`:

Remove `localManifests` + `remoteManifests` from the struct and `New` signature. Remove the `r.localManifests.Get` arg from the `acquiring.New(...)` call inside `buildChain`.

- [ ] **Step 6: Update integration harness**

Edit `internal/app/ritual_integration_test.go`. Drop `localManifests` / `remoteManifests` fields from `testRitual`. Drop the `Save(ctx, &domain.Manifest{})` seeds. Drop the args from the `app.New` call. Drop the `adapters.NewManifestStore` construction.

- [ ] **Step 7: Drop domain import if unused**

Check each file touched above — remove `"ritual/internal/core/domain"` if no non-test reference remains.

- [ ] **Step 8: Build + test**

Run: `go build ./... && go test -timeout 30s ./internal/core/stages/acquiring/... ./internal/app/...`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/app/ritual.go internal/core/stages/acquiring/ internal/core/ritual/runstate.go internal/app/ritual_integration_test.go
git commit -m "refactor(chain): drop manifest stores from app.Ritual, acquiring.SnapshotLocalFn, RunState.LocalBefore"
```

---

## Task 17: Delete `services.SyncService` + port + tests + subsystems/sync

**Files:**
- Delete: `internal/core/services/sync.go`
- Delete: `internal/core/services/sync_test.go`
- Delete: `internal/core/services/sync_integration_test.go`
- Delete: `internal/subsystems/sync/` (whole dir) — unless Task 5 already removed it.
- Modify: `internal/core/ports/ports.go` — delete `SyncService` interface block.

- [ ] **Step 1: Verify no remaining callers**

Run: `grep -rn "services.SyncService\|services.NewSyncService\|ports.SyncService\|WorldSync" --include="*.go"`

Expected: only matches inside files scheduled for deletion in this task. If any match outside, investigate before deleting — it may be a test that needs rewriting or a stale reference.

- [ ] **Step 2: Delete the files**

```bash
git rm internal/core/services/sync.go internal/core/services/sync_test.go internal/core/services/sync_integration_test.go
git rm -r internal/subsystems/sync
```

- [ ] **Step 3: Inline `subsystems/sync/kit.go` logic into `cmd/cli`**

The Kit's current job after Task 5 is wiring `Puller` + `Applier` + `HeadResolver` + `Committer` + `Pusher` from raw primitives. In `cmd/cli/main.go`, replace the `sk := sync.Build(...)` call site with direct construction mirroring `cmd/gui/main.go`. Keep it flat — this is composition-root plumbing, not library code.

- [ ] **Step 4: Delete the port**

Edit `internal/core/ports/ports.go`. Delete the `SyncService` interface block (around line 147).

- [ ] **Step 5: Build + test**

Run: `go build ./... && go vet ./...`

Expected: PASS.

Run: `go test -timeout 60s ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/cli/main.go internal/core/ports/ports.go
git rm internal/core/services/sync*.go
git rm -r internal/subsystems/sync
git commit -m "chore(sync): delete services.SyncService + ports.SyncService + subsystems/sync — refs pipeline replaces v1 sync"
```

---

## Task 18: Delete `ValidatorService` and `ports.ManifestStore`

**Files:**
- Delete: `internal/core/services/validator.go` + `validator_test.go`
- Delete: `internal/core/ports/manifest_store.go` (if separate) or the `ManifestStore` block in `ports.go`
- Delete: `internal/core/ports/mocks/manifest_store.go`
- Delete: `internal/adapters/manifest_store.go` + any adapter test

- [ ] **Step 1: Verify no callers**

Run: `grep -rn "ValidatorService\|ports.ManifestStore\|adapters.NewManifestStore\|CheckLock\|CheckManifestVersion" --include="*.go"`

Expected: only matches inside files scheduled for deletion in this task.

- [ ] **Step 2: Delete**

```bash
git rm internal/core/services/validator.go internal/core/services/validator_test.go
git rm internal/core/ports/manifest_store.go 2>/dev/null || true
git rm internal/core/ports/mocks/manifest_store.go
git rm internal/adapters/manifest_store.go
# Delete adapter test if present:
git ls-files internal/adapters/manifest_store_test.go | xargs -r git rm
```

If `ports.ManifestStore` lived inline in `ports.go`, edit that file to delete the interface block + the `ValidatorService` block.

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test -timeout 60s ./...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/core/ports/ports.go
git commit -m "chore(ports): delete ManifestStore + ValidatorService — lock lives outside manifest"
```

---

## Task 19: Delete `domain.Manifest` and friends

**Files:**
- Delete: `internal/core/domain/manifest.go` + `manifest_test.go`
- Modify: any `domain/` file that referenced `Manifest`, `WorldsManifest`, `SyncState`, `FileEntry`

- [ ] **Step 1: Verify no callers**

Run: `grep -rn "domain.Manifest\|domain.WorldsManifest\|domain.SyncState\|domain.FileEntry" --include="*.go"`

Expected: only matches inside files scheduled for deletion in this task (`manifest.go` + `manifest_test.go`).

- [ ] **Step 2: Check dependent types**

`FileEntry` may still have test callers outside `manifest_test.go` (scanner tests etc.). Grep:

```bash
grep -rn "domain.FileEntry\|FileEntry{" --include="*.go"
```

If `FileEntry` persists in scanner / commit code paths (non-v1), keep it — move it to its own file (e.g., `domain/file_entry.go`) before deleting `manifest.go`. Likewise for `WorldsManifest` and `SyncState`: if they have no non-v1 home, they die with `manifest.go`.

- [ ] **Step 3: Delete**

```bash
git rm internal/core/domain/manifest.go internal/core/domain/manifest_test.go
```

- [ ] **Step 4: Delete `services.migration` package outright**

No backwards compatibility, no migration scripts retained — v1 data is not upgraded, users start fresh on v2.1. `RunMigrations` has zero non-test callers; every migration in the list (`migrateV2`, `migrateLegacyBackups`) targets v1 layout.

```bash
git rm internal/core/services/migration.go internal/core/services/migration_test.go
```

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test -timeout 60s ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/domain/ cmd/cli/main.go cmd/gui/main.go
git rm internal/core/domain/manifest.go internal/core/domain/manifest_test.go internal/core/services/migration.go internal/core/services/migration_test.go
git commit -m "chore(domain): delete Manifest + WorldsManifest + SyncState + migrations — refs + settings replace v1 manifest; no backwards compat"
```

---

## Task 20: Delete `internal/core/sync/` engine package

**Files:**
- Delete: `internal/core/sync/` (whole directory — engine.go, planning.go, staging.go, committing.go, orphancleanup.go, ghostcleanup.go, stagedirinit.go, stagingdircleanup.go, failed.go, done.go, events.go, runstate.go + tests)
- Modify: `internal/gui/projection/projection.go` — drop the four `sync.Sync*Info` fold cases (`SyncStageStartedInfo`, `SyncStageProgressInfo`, `SyncCommitStartedInfo`, `SyncCommitProgressInfo`). Refs pipeline progress reaches the GUI via decorator events (`PullStartedInfo`, `PutStreamStarted`, etc.) per spec §2404.
- Modify: `internal/gui/projection/projection_test.go` — drop tests that publish those event types; add at most one replacement test covering the refs decorator progress path if coverage drops materially.

- [ ] **Step 1: Verify no remaining callers**

Run: `grep -rn "core/sync\"\|sync.Sync[SC]\|sync.Engine\|sync.Run\b" --include="*.go"` (after Tasks 17 + 9 land, only projection + tests should reference `core/sync`).

Expected: matches only in `internal/gui/projection/` and `internal/core/sync/`. If anything else appears, investigate.

- [ ] **Step 2: Drop projection fold cases**

Edit `internal/gui/projection/projection.go`. Remove the four `case sync.SyncStage*Info` / `case sync.SyncCommit*Info` branches from the `fold` switch. Remove the `"ritual/internal/core/sync"` import.

- [ ] **Step 3: Drop projection tests for those events**

Edit `internal/gui/projection/projection_test.go`. Delete tests that publish `sync.SyncStageStartedInfo` / `SyncStageProgressInfo` / `SyncCommitStartedInfo` / `SyncCommitProgressInfo`.

- [ ] **Step 4: Delete the engine package**

Run: `git rm -r internal/core/sync`

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test -timeout 60s ./internal/gui/projection/...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/gui/projection/
git rm -r internal/core/sync
git commit -m "chore(sync): delete core/sync engine + projection fold cases — refs pipeline emits decorator progress"
```

---

## Task 21: Delete `internal/adapters/ritualsync.go` (+ test)

**Files:**
- Delete: `internal/adapters/ritualsync.go`, `internal/adapters/ritualsync_test.go`

`.ritualsync` allowlist files are a v1 concept. Spec §645 puts target globs on `CommitOpts.Targets` (manifest-embedded, travels with ref history). On-disk filter files + `ParseRitualSync` + `FilteredScanner` go away.

- [ ] **Step 1: Verify no remaining callers**

Run: `grep -rn "ParseRitualSync\|FilteredScanner\|\\.ritualsync" --include="*.go"`

Expected: matches only inside files scheduled for deletion in this task (after Task 5's `subsystems/sync/kit.go` deletion + Task 19's `services/migration.go` deletion).

- [ ] **Step 2: Delete**

```bash
git rm internal/adapters/ritualsync.go internal/adapters/ritualsync_test.go
```

- [ ] **Step 3: Build + test**

Run: `go build ./... && go test -timeout 60s ./...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git rm internal/adapters/ritualsync.go internal/adapters/ritualsync_test.go
git commit -m "chore(adapters): delete .ritualsync filter — CommitOpts.Targets replaces it per spec §645"
```

---

## Task 22: Final sweep — run full test suite

- [ ] **Step 1: Full build + vet**

Run: `go build ./... && go vet ./...`

Expected: PASS.

- [ ] **Step 2: Full test suite with strict timeouts**

Run: `go test -timeout 60s ./...`

Expected: PASS.

- [ ] **Step 3: Grep for stragglers**

Run:

```bash
grep -rn "publishing\|backup\|UpdaterService\|CreateBackup\|BackupsDir\|StagePublishing\|StageBackup\|SyncService\|ManifestStore\|domain.Manifest\|ValidatorService\|SyncDownloadUpdater\|SyncUploader\|RitualUpdater" --include="*.go" | grep -v "_test.go"
```

Every match must be a false positive (e.g., doc comment using the word "backup" in the abstract sense of ref-based history). Anything code-level is an oversight — delete it.

- [ ] **Step 4: Final commit**

```bash
git status
git diff
```

If any cleanup surfaced:

```bash
git add -p
git commit -m "chore: final sweep of v1 publishing/backup references"
```

---

## Self-Review Notes

- Spec §2264 chain order: covered by Task 3 (buildChain) + Task 9 (integration test).
- Spec §2297 retention pairing: Task 3 places `retaining.New` between committing→pushing and pushing→unlocking.
- Spec §2285 single stage package / two instances: preserved (existing `retaining.Strategy` reused).
- Spec §1435 amend-vs-fresh: `NewCommitOptsResolver` (Task 2) implements both branches based on `rs.RefID` / `rs.ParentRefID`.
- No step contains TBD / fill-in / similar-to placeholders.
- Method signatures cross-check: `pushing.New(pusher, onOK, onFail)` — used identically in Task 1, 3. `NewCommitOptsResolver(targets []string)` — used identically in Task 2, 3. `retaining.New(jobs, bus, onFail, onOK)` — matches existing signature (already chainable per commit 8ee3188).
- Known limitation: `internal/app/ritual_test.go` placeholder in Task 3 is a `t.Skip` marker; the real coverage is the integration test in Task 9. This is intentional — driving a full chain from `app.Ritual` without fakes duplicates integration-test plumbing.
