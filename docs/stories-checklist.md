# Ritual — user-story checklist

Source: 14 stories distilled from the POC session.
Status legend: `[ ]` pending · `[x]` done · `[~]` in progress · `[-]` skipped (already fixed)

---

## Pipeline / startup

- [-] **#1 Fresh-install pipeline works end-to-end**
  No local state (no manifest, no .ritualsync, empty dirs). Pipeline completes; filters loaded lazily; missing manifest = empty; full remote pull on first fetch.
  _Classification: integration._

- [-] **#2 StartScript resolves relative to work root**
  e.g. `server/.ritual_run.bat`, not bare filename. Run stage must launch it.
  _Classification: unit (cmdbuilder). Already handled in POC._

- [ ] **#3 Server loads the synced world**
  Whatever run-stage CWD is, server must find ritual-downloaded world content. Either configure `level-name` to ritual's worlds dir, or colocate worlds with server CWD at runtime.
  _Classification: integration._

---

## Server lifecycle (was story #5 — expanded to rules 5.1–5.6)

Strategy-level `ErrAlreadyRunning` guard added: second concurrent `Run()` rejected via `atomic.Bool`. Safety net under the orchestrator guard (story #7).


Unified design: **one ctx cascades all stop signals** (UI click, Go shutdown). Subprocess NOT bound to that ctx (plain `exec.Command`). Listener: `ctx.Done()` → `stdin stop\n` → wait (timeout) → `process.Kill()`.

- [x] **5.1 `/stop` from server console**
  User types `/stop`. Server prints "Stopping the server" then exits 0.
  Scanner detects line → emits `ServerStoppingInfo`; `cmd.Wait` returns → emits `ServerStoppedInfo`.
  Full lifecycle STARTING → STARTED → STOPPING → STOPPED asserted.
  _Test: `internal/core/stages/running/strategy_test.go`. Also migrated stdin/stdout to `os.Pipe` so `cmd.Wait` no longer blocks on copier goroutines._

- [x] **5.2 UI Stop button** (covered by 5.3)
  UI Stop click cancels ctx — identical to any outside-initiated cancel.

- [x] **5.3 Outside stop — ctx cancel**
  ctx.Done triggers `cmd.Cancel` (stdin `stop\n` + ServerStoppingInfo). `cmd.WaitDelay` (SetStopGracePeriod on Strategy) force-kills if server ignores stop. Classifier: `ctx.Err() != nil` → always `ServerStoppedInfo` + onNext (not Crashed).
  _Tests: `TestRunning_OutsideStop_Graceful_*` and `TestRunning_OutsideStop_ForceKillFallback_*`._

- [x] **5.4 Cancel before cmd.Start**
  Pre-start cancel: `ctx.Err() != nil` at Run entry → return (onNext, nil) without spawning. No orphan, no damage, no lifecycle events (nothing ran).
  _Test: `TestRunning_CancelledBeforeRun_FastExitNoSpawn`._

- [x] **5.5 Stop queued during STARTING**
  Pre-ready `cmd.Cancel` sets `stopQueued` atomic; readiness path flushes it after `save-off`. `ServerStoppingInfo` is emitted only when `stop\n` is actually written, preserving STARTING → STARTED → STOPPING → STOPPED ordering.
  Readiness now runs on an independent ctx (not user-cancellable), bounded by `WaitDelay`, so queued stops can flush even after outer ctx is cancelled.
  _Test: `TestRunning_StopDuringStarting_QueuedAndFlushedInOrder` with `gatedReadiness` + helper that asserts stdin ordering._

- [~] **5.6 Go crash → no ghost java process** — code complete, **pending Windows runtime verification**
  Subprocess attached to a Windows Job Object via `github.com/kolesnikovae/go-winjob` (`winjob.Start(cmd, winjob.LimitKillOnJobClose)`). When the Go parent handle closes (for any reason — panic, SIGKILL, terminal close), the kernel kills every process in the job.
  Build-tagged: `procguard_windows.go` uses go-winjob; `procguard_other.go` is a noop + plain `cmd.Start`. `GOOS=windows GOARCH=amd64` build verified.
  **Still to do:** runtime verification on Windows (smoke on dev box, scripted test on VM, GitHub Actions windows-latest runner). See verification plan in conversation.

---

## Sync correctness (story #10 family)

- [x] **#10 Commit must complete, override** — post-commit verify explicitly dropped
  1. Atomic override — commit replaces existing remote files. Same for local-FS and R2. ✅ (committing strategy: Copy staging→final, defer Delete staging).
  2. Observable per phase — `stage.put`, `stage.commit`, `stage.cleanup` events with per-file context. ✅ (`internal/core/sync/events.go`).
  3. Fail loud — preserve staging + remote on error; no cleanup racing commit. ✅ (`SyncStagingDirCleanedInfo{Outcome: "failed"}` on error).
  4. ~~Post-commit verify~~ — **dropped**. Re-read + hash sample doubles read volume, and storage backends (local FS, R2) already guarantee durability post-ack. On crash mid-commit we have no restore path anyway, so verify-fail outcome is unactionable. Normal transient failures handled by existing retries.
  5. Orphan cleanup is a separate explicit pass, not a staging side-effect. ✅ (`SyncOrphanCleanupInfo` / `SyncGhostDeletedInfo`).
  _Classification: integration (both backends)._

- [x] **#4 Staging uses UUIDv4, not lock strings**
  `internal/core/sync/stagedirinit.go` uses `uuid.NewString()` → `.staging/<uuid>`. Cleanup unconditional on both success and failure paths.
  _Classification: unit + integration._

- [x] **#13 Backup stage saves upstream + observable** — rewritten as a post-publish, same-storage snapshot on both sides. Replaces the pre-publish cross-storage archive.

  Refactor plan:
  1. Rename stage `Archiving` → `Backup` (Go package + constant + event operation strings).
  2. Pipeline reorder: `Run → Publish → Backup → Unlock → Retain` (was `Run → Archive → Publish → Unlock → Retain`). Crash still skips Publish + Backup.
  3. Backup strategy does two same-storage `CreateBackup` calls: local→local, remote→remote. Drops `CreateBackupFrom` (no cross-storage bytes).
  4. No-op detection by manifest comparison — if `rs.LocalBefore.Worlds.XXHashMap` equals post-publish local manifest's `XXHashMap`, skip backup. Drops scanner dependency.
  5. Errors published as `ritual.ErrorInfo{Operation:"backup"}`, pipeline continues to Unlocking. Lock always releases.
  6. Observability via existing `observed` decorator on both storage repos — `StorageCopyInfo` / `StorageListInfo` / `StoragePutInfo` emit per call, no extra wiring.

  Story → test matrix. Completion rule: every row has a named test function.

  | ID | Story | Layer | Test |
  |---|---|---|---|
  | B1 | Post-publish canonical backup — snapshot contains post-run world state on both sides | integration | `TestIntegration_BackupCreated_ContainsPostRunState` (`internal/app/ritual_integration_test.go`) |
  | B2 | No-op session skip — unchanged `XXHashMap` → no `CreateBackup` calls | unit + integration | `TestStrategy_NoMutation_Skips` (`internal/core/stages/backup/strategy_test.go`) + `TestIntegration_NothingChanged_NoBackupCreated` |
  | B3 | Same-storage only — zero remote Get calls during Backup stage window | unit + integration | `TestStrategy_Mutated_CopiesOnBothSides` (asserts `GetCalls==0`) + `TestIntegration_BackupUsesSameStorageOnly_NoRemoteReadDuringBackup` |
  | B4 | Error non-fatal — `CreateBackup` failure emits `ErrorInfo{Operation:"backup"}`, strategy returns `onNext` | unit | `TestStrategy_CopyError_EmitsErrorInfo_ContinuesToOnNext` |
  | B5 | Observability — Backup emits `StorageCopyInfo` with `DstKey` under `backups/` on both sides | integration | `TestIntegration_BackupEmitsStorageCopyEventsWithBackupsPrefix` |
  | B6 | Crash skips Publish + Backup — `exit(1)` → `StateChangedInfo` sequence omits both stages | integration | `TestIntegration_ServerCrash_SkipsPublishAndBackup` |
  | B7 | Retention post-backup — Backup forwards to Unlock → Retain; count matches `KeepLast` | integration | `TestIntegration_RetentionPrunesOldBackups` |
  | B8 | Stage name is `"Backup"` — `StateChangedInfo` / events use the new label | unit | `TestStrategy_Name_IsBackup` |
  | P1 | Pipeline order — `Checking→Fetching→Acquiring→Running→Publishing→Backup→Unlocking→Retaining` asserted end-to-end | integration | `TestIntegration_PipelineOrder_MatchesCheckFetchAcquireRunPublishBackupUnlockRetain` |
  | R1 | Lock released on backup error — injected `scriptedStorage` copy failure → `ErrorInfo` emitted, `LockedBy` cleared on both manifests, status reaches `Done` | integration | `TestIntegration_BackupCopyError_EmitsErrorInfo_LockStillReleased` |

  _Classification: unit + integration._

- [x] **#14 Sync observability — both directions**
  Full event taxonomy in `internal/core/sync/events.go`: `SyncStartedInfo`, `SyncPlanInfo`, `SyncStagingDirCreatedInfo`, `SyncStage{Started,Progress,Finished,Failed}Info`, `SyncCommit{Started,Progress,Finished,Failed}Info`, `SyncStagingDirCleanedInfo`, `SyncOrphanCleanupInfo`/`SyncGhost{Deleted,CleanupFailed}Info`, `SyncFinishedInfo`/`SyncFailedInfo`. Storage decorator (`internal/adapters/observed/storage.go`) emits per-call `StorageGet/Put/Copy/Rename/Delete/DeleteBatch/ListInfo`. All events implement `String()`; bus publishes to logger + GUI. Parity upstream↔downstream via shared state machine. Ordering enforced by topology, asserted in `engine_test.go`.
  _Classification: unit (ordering) + integration (real emission)._

---

## Readiness / network

- [x] **#9 Readiness probe observable**
  `TCPReadinessCheck` takes bus in `NewTCPReadinessCheck(address, bus)` and emits `ports.ReadinessDialInfo{Address, Attempt, Err}` on every dial — success event has `Err==nil`, failure events carry the dial error. Tests: `internal/adapters/readiness_test.go`.
  _Classification: unit._

- [x] **#11 Probe targets the right IP**
  `cmd/cli/main.go:132` now dials `127.0.0.1:<port>` (was `localhost:<port>`). Dodges Windows `::1`-first resolution since Minecraft is IPv4-only.
  _Classification: integration._

- [-] **#12 Server bind reachable from probe**
  Dismissed: wildcard bind is the contract from the MC side. If user sets `server-ip=<specific>` in `server.properties`, that is an MC-side misconfig, not the probe's concern. No `server.properties` reader added.

---

## UI / state machine

- [x] **#6 UI reflects lifecycle — backend events confirmed**
  Backend emits `ServerStartingInfo → ServerReadyInfo → ServerStoppingInfo → ServerStoppedInfo` in order on a clean run+stop cycle. GUI state machine (future work) subscribes to these to drive DOWN / STARTING / STARTED / STOPPING. Scope here is confirming emission; rendering lands with the GUI.
  _Test: `TestIntegration_ServerLifecycleEventsEmitted_StartingReadyStoppingStopped` (`internal/app/ritual_integration_test.go`)._
  _Classification: integration._

- [x] **#7 Restart after completed run**
  `Ritual.start` now rejects only when `status == Running`. Terminal states (Done, Failed, Idle) all accept a fresh Start.
  _Code: `internal/app/ritual.go:105-108`._
  _Test: `TestRitual_Start_AfterDone_StartsAgain` (`internal/app/ritual_test.go`)._
  _Classification: unit._

- [x] **#8 Graceful Stop ≠ Failed**
  User-requested stop sets `userStop` flag before cancelling ctx. `resolveStatus` treats `userStop=true + (nil err OR context.Canceled)` as Done; other stage errors still propagate Failed.
  _Code: `internal/app/ritual.go:125-149` (`stop`, `resolveStatus`)._
  _Tests: `TestRitual_Stop_CancelsRunning` + `TestIntegration_StopMidGame_LockReleased` (both updated to assert `Done`)._
  _Classification: unit + integration._
