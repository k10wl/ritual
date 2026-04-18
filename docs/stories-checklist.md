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

- [ ] **#10 Commit must complete, verify, override**
  1. Atomic override — commit replaces existing remote files. Same for local-FS and R2.
  2. Observable per phase — `stage.put`, `stage.commit`, `stage.cleanup` events with per-file context.
  3. Fail loud — preserve staging + remote on error; no cleanup racing commit.
  4. Post-commit verify — re-read remote manifest + sample hashes. "No write error" ≠ "committed".
  5. Orphan cleanup is a separate explicit pass, not a staging side-effect.
  _Classification: integration (both backends)._

- [ ] **#4 Staging uses UUIDv4, not lock strings**
  Drop lock-string-derived paths (contain `:`, Windows-reserved). Use `uuid.NewString()` for staging dir; delete after commit success; preserve on failure (per #10).
  _Classification: unit + integration._

- [ ] **#13 Archiving stage actually saves upstream**
  Pre-run archive persists to local AND remote. Remote write currently silent-fails. Verify write succeeds + observable post-run.
  _Classification: integration._

- [ ] **#14 Sync observability — both directions**
  Full event taxonomy (see `project_upstream_observability.md`):
  - `sync.plan` pre-run (file list)
  - `sync.started`
  - `stage.put.{started,progress,finished,failed}`
  - `stage.commit.{started,progress,finished,failed}`
  - `verify.{started,progress,finished,failed}`
  - `stage.cleanup.{started,finished,failed}`
  - `sync.finished` / `sync.failed`

  Parity upstream ↔ downstream. Every event emits log line. GUI subscribes to bus.
  Ordering invariant (`sync.plan` before any `stage.*`) is **unit-testable** in state machine.
  _Classification: unit (ordering) + integration (real emission)._

---

## Readiness / network

- [ ] **#9 Readiness probe observable**
  Every dial emits progress event: address, attempt#, error on fail. User never left wondering "is it trying?".
  _Classification: unit._

- [ ] **#11 Probe targets the right IP**
  Windows `localhost` → `[::1]` first; Minecraft is IPv4-only. Force `127.0.0.1` or dial actual bind address.
  _Classification: integration._

- [ ] **#12 Server bind reachable from probe**
  `server.properties server-ip=<specific>` excludes loopback. Default empty (bind `0.0.0.0`), or probe reads `server-ip` and dials that.
  _Classification: integration._

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
