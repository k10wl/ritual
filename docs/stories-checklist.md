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

- [~] **#13 Archiving stage actually saves upstream** — local+remote write coded, **remote write not observable**
  `internal/core/stages/archiving/strategy.go:63-64` calls `CreateBackupFrom(remoteStore, localStore)` + `CreateBackup(remoteStore)`, but errors swallowed by `_ = errors.Wrap(...)` and stage bypasses the observed storage decorator. Needs: propagate errors + route remote writes through `observed` so `StoragePutInfo` fires.
  _Classification: integration._

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

- [ ] **#6 UI reflects lifecycle**
  States DOWN / STARTING / STARTED / STOPPING visible, driven by backend events. No-payload events don't panic emitter. STOPPING is optimistic on Stop click.
  _Classification: unit (state machine transitions) + integration (GUI subscription)._

- [ ] **#7 Restart after completed run**
  Start rejected only while Running. Terminal states (Done, Failed, Idle) all restartable.
  _Classification: unit (state machine)._

- [ ] **#8 Graceful Stop ≠ Failed**
  User-requested stop → Done, not Failed. Only stage-returned errors propagate Failed.
  _Classification: unit (state machine)._
