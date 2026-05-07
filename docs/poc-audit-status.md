# POC Audit — Fix Status Tracker

Tracks POC session findings against branch state so the audit isn't
re-derived every session.

**Source-of-truth audit:** `docs/dev-session-2026-04-25-poc-setup.md` on
`origin/testing` (commit `2f72cc4`). 12 numbered fixes + 5 open items.
Read it via `git show origin/testing:docs/dev-session-2026-04-25-poc-setup.md`.

**Audited branches:**
- `origin/testing` (commit `c6663b6` + docs `2f72cc4`) — all 12 fixes inline
  in `cmd/gui/main.go`. End-to-end POC verified.
- `feat/delta-sync` — clean-shaped subset; uses shared primitives instead
  of cmd/gui inlines.

## Numbered fixes

| # | Fix | Branch landing | Shape note |
|---|-----|----------------|------------|
| 1 | `wails-api.ts` binding path (`gui/services` → `gui/control`) | `feat/delta-sync` `749e92b` · `testing` `c6663b6` | identical |
| 2 | First-ref bootstrap on empty storage | `feat/delta-sync` `749e92b` · `testing` `c6663b6` | **diverges** — `feat/delta-sync` extracts `pulling.NewHeadResolver` primitive + sentinel `pulling.ErrNoHead`; pulling stage routes ErrNoHead → onOK. `testing` returns `("", nil)` inline in cmd/gui and integration test |
| 3 | Real NeoForge `start.bat` wiring (`adapters.NewServerCmdBuilder` + `NewTCPReadinessCheck`); drops `fakerunCmdBuilder`, `immediateReady`, `locateFakerun`, `fakerunName`, `buildFakerun` and the `errors`/`io`/`os/exec` imports | `feat/delta-sync` `4821fcb` · `testing` `c6663b6` | **diverges** — `feat/delta-sync` lifts the launcher filename to a `Settings.StartScript` field (`json:"start_script"`, `domain.DefaultStartScript = "start.bat"`) backfilled by `applyDefaults` so operators can override the entry point via settings.json without a code change. Server workroot opens `<root>/server/`, so the default resolves to `<root>/server/start.bat`. `cmd/fakerun` binary kept as a running-stage fixture for `internal/integration`; only the cmd/gui inlines were dropped. Two new domain regression tests lock the default + the empty-field backfill |
| 4 | **GUI Stop cancels Committing** (data-loss bug). `lifecycle.controller.stop()` cancelled `runCtx`; commit aborted on first storage call after server exit. Fix: `stop()` only sets `userStop`; running stage's `coordinate()` subscribes to `ritual.StopRequested` and triggers `writeStop()` instead | `feat/delta-sync` (next commit) · `testing` `c6663b6` | **diverges** — `feat/delta-sync` moves the bus subscription up to `strategy.Run` (before `cmd.Build`) so the sub is wired before any external observer can publish StopRequested in response to a Build-triggered ready signal. Closed a race the testing-branch shape inherited. Deletes obsolete `TestRitual_Stop_CancelsRunning` + `blockingCmdBuilder` (asserted the pre-fix contract — Stop forcing ctx cancel through a hung Build is now intentionally a no-op per audit caveat). |
| 5 | `prefixRouter` in `cmd/gui/main.go` — gates `compressing` to `objects/*`; `refs/*` + `lock` go raw → cat-readable refs. `Copy` falls back to GetStream→PutStream across the gate; `List` merges across the gate; `DeleteBatch` splits keys | `feat/delta-sync` (next commit) · `testing` `c6663b6` | **diverges** — `feat/delta-sync` lifts the router from a private `cmd/gui` struct to a reusable `adapters.PrefixRouter` decorator with unit tests + a disk-level regression (`TestPrefixRouter_RefsOnDiskHumanReadable_ObjectsOnDiskCompressed`) so any reversion that re-wraps refs in `compressing` fails loudly. `cmd/gui/main.go` wires `NewPrefixRouter("objects/", counterCompressed, rawFS)` for both local and remote stacks |
| 6 | `logging.Attach` + `logging.CreateLogFile` from `cmd/gui` → `<root>/logs/<ts>.log`; `guiRuntime` gains `logFile *os.File` + `closeLogFile func()` | `feat/delta-sync` (next commit) · `testing` `c6663b6` | **diverges** — `feat/delta-sync` adds `logging.Build(bus, workRoot)` so `cmd/gui/main.go` and `internal/integration/ritual_integration_test.go startRitualFull` share a single wiring call. Integration test `TestIntegration_RunSession_PersistsLogFileWithBusEventsUnderRootLogsDir` enforces the contract end-to-end (refs-first bootstrap path, no raw-world seeding) |
| 7 | `refs/commit.go` — `json.MarshalIndent(ref, "", "  ")` | `feat/delta-sync` `749e92b` · `testing` `c6663b6` | identical; `feat/delta-sync` adds regression test `TestCommitter_RefFileIsHumanReadableJSON_NotMinified` |
| 8 | Workdir = `<root>` (not `worlds/`); explicit `commitTargets` allowlist incl. `server/libraries/**`; excludes `server/logs/**`, `server/usercache.json`, `server/.cache/**`, `refs/`, `objects/`, `remote-mock/`, root `logs/`, `settings.json` | `feat/delta-sync` (next commit) · `testing` `c6663b6` | **diverges** — `feat/delta-sync` lifts the allowlist to `config.DefaultCommitTargets` so `cmd/gui/main.go` and the regression test in `internal/core/refs/commit_test.go` share one source of truth. Migration is a non-issue here (no active clients) |
| 9 | Idle ticker spam — `internal/adapters/progress/ticker.go Run()` skips emit when counters unchanged; one final zero-delta tick after activity stops | `feat/delta-sync` (next commit) · `testing` `c6663b6` | identical shape; `feat/delta-sync` adds two regression tests (`TestTicker_StableCounters_NoTicks`, `TestTicker_OneFinalZeroDeltaAfterActivityStops`) locking both halves of the contract |
| 10 | `projection.fold()` missing `progress.Tick` case → 0% bar all run. Fix: handle `progress.Tick`, update `BytesDone` + label | `feat/delta-sync` `6eab5c7` · `testing` `c6663b6` | **diverges** — `feat/delta-sync` lands the naive shape (Tick → `BytesDone = BytesIn`, no stage gate, no label mutation) so the regression test `TestProjection_TickInPullingStage_UpdatesBytesDone` locks the BytesDone→bar contract in isolation. Stage gate + Mbps label deferred to fix #11 |
| 11 | Stage-aware label gate — `Projection.pipelineStage` field; `onTick` only mutates label in `ritual.StagePulling`/`ritual.StagePushing`; drops `mbps <= 0` fallback | `feat/delta-sync` `8ad2b39` · `testing` `c6663b6` | identical shape; `feat/delta-sync` adds four regression tests locking the gate (Pulling Mbps label, Pushing BytesOut + Mbps label, late Tick during Committing must not flip label or move BytesDone, zero-Mbps Tick keeps stage-entry label) |
| 12 | `ThrottledStorage` decorator (`internal/adapters/throttled.go`) — `golang.org/x/time/rate` token bucket, 12.5 MB/s sustained, 1.25 MB burst (clamped ≥ 64 KiB), metadata ops unthrottled. Adds `golang.org/x/time v0.15.0` to go.mod | `feat/delta-sync` `69b7f65` · `testing` `c6663b6` | identical decorator shape; `feat/delta-sync` adds three regression tests (burst-clamp at low bytesPerSec, metadata-ops passthrough not rate-limited, PutStream throughput limited to configured rate) so any reversion that re-routes refs/objects past the limiter or drops the burst floor fails loudly |

## Open items (from audit; status independent of fixes #1–12)

| # | Item | Status |
|---|------|--------|
| 0 | No local process lock — `cmd/gui/main.go:251` wires `lock.New(remoteStorage, host)` only | **closed on `feat/delta-sync` `a44c476` + `ca3f7b6`** — `lock.Both` stacks local + remote leases at the cmd/gui composition root; reuses the existing `lock.Locker` primitive (no Windows API, no new adapter, no new port). Integration tests lifted to the same `lock.Both` surface (`ca3f7b6`); regression story `TestIntegration_LocalLockHeldBySameHost_BlocksAcquireAndSurfacesLocalHolder` locks the contract that a same-host PID surfaces the local holder, not the remote one |
| 1 | Progress bar reads 0% — push/pull never emit a `PlanInfo{BytesTotal}` event upfront | open both branches |
| 2 | Pre-existing integration timeouts: `TestIntegration_ChangesUploaded_RefAppearsOnRemote`, `TestIntegration_ServerLifecycleEventsEmitted_StartingReadyStoppingStopped` (1s `waitDone` budget) | open both branches; not session-introduced |
| 3 | Stop only graceful in Running stage; Pulling/Pushing aborts wait the 20s window-close budget | open both branches (fix #4 covers Running only) |
| 4 | `cmd/gui/main.go` mock-remote TODO — needs `adapters.NewR2Repository` swap | open both branches |

## Maintenance

- When a fix lands on `feat/delta-sync` (or successor), update its row's
  branch column with the landing commit and any shape divergence note.
- When a new dev-session doc lands on `testing`, append a section here
  pointing to it; do not restate its content.
- Don't paste the audit text — keep this file as a status tracker, not a
  copy.
