# 057 — Parallelize GC orphan-blob deletes in `refs.Collector`

**Date:** 2026-08-28
**Status:** Implemented
**Related:** [[025-drop-redundant-exists-gate]] (same class of fix — N serial remote round-trips found via live session log), spec §Retention and GC in `docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md` (defines the fail-continue contract this change had to preserve).

## Background

`refs.Collector.Collect` implements the GC half of §Retention and GC: after retention drops manifests, Collect mark-sweeps `objects/{hash}` — building a live-hash set from surviving `refs/*.json`, then deleting every `objects/` key whose hash isn't live. It runs after every push/retention cycle and after every amend (local-only).

Since its original implementation (c65f06b), the delete step has been a plain loop calling `c.store.Delete(ctx, key)` once per orphan, on the calling goroutine, fully serial.

## Problem

User-reported from a live session log (`C:\Users\Owl\k10wl\ritual\logs\20260828144352.log`, 14:50:22–14:50:31):

```
[14:50:22] storage.delete store=router{...retrying::r2::ritual} key=objects/1b739ec20270cd70 dur=252ms
[14:50:22] storage.delete store=router{...retrying::r2::ritual} key=objects/23c3b94b55ac0474 dur=319ms
... (30 lines total, one every ~250-400ms, fully sequential)
```

30 sequential remote deletes × ~250-400ms each ≈ 9-13s of wall clock spent one-at-a-time on independent operations. A background sweep of all three sizable session logs on disk (`20260828144352.log`, `20260727174741.log`, `20260726192450.log`) found the same 32-38-call serial pattern in every session and confirmed it's the only load-bearing win in the logs — Push/Pull blob transfer already saturates the existing `ParallelRunner(10)`, `storage.exists` calls are cheap local-FS stats, and `refs/` listings are scattered (~8/session, not a tight loop).

## Questions and Answers

**Q1.** Parallelize with a one-off goroutine pool, or batch via R2's `DeleteObjects` (already exposed as `StorageRepository.DeleteBatch`, used by the retention-refs Job for manifest keys)?
**A.** Parallelize via the existing `ports.BlobRunner`/`adapters.ParallelRunner` — the same primitive Pull/Push/Commit/Apply already use for blob fan-out. Batching was considered and rejected: `DeleteBatch` collapses fail-continue-per-key into fail-continue-per-batch (a partial-batch S3 error isn't even surfaced by the current `R2Repository.DeleteBatch`, which only returns an error for whole-request failures), and the spec explicitly chose per-key fail-continue for GC ("one flaky delete cannot block the rest of the sweep") — a documented, tested contract (`TestCollector_ContinuesSweepWhenAnIndividualDeleteFails`). Reusing `BlobRunner` gets the concurrency win without touching that contract.

**Q2.** How is per-key fail-continue preserved through a runner whose contract is "first error cancels the rest"?
**A.** The `fn` passed to `runner.Run` swallows the delete's error and always returns `nil` — identical to the old loop's `_ = c.store.Delete(ctx, key)`. The runner's cancel-on-first-error path never trips because it never sees an error.

**Q3.** What concurrency limit?
**A.** No new tunable. `Collector` now takes an injected `runner ports.BlobRunner`; production wiring (`cmd/gui/main.go`) passes the same `ParallelRunner(10)` instance already shared by Pull/Apply/Commit/Push, so the whole app runs one concurrency budget.

**Q4.** Behavior change?
**A.** One: if the `ctx` passed to `Collect` is cancelled mid-sweep, `ParallelRunner.Run` now returns `ctx.Err()` (it checks after every dispatch), whereas the old loop swallowed cancellation identically to any other delete error. No test exercises this path; treated as a minor, arguably-more-honest deviation rather than a design decision requiring its own Q.

## Design

`internal/core/refs/collect.go`:

```go
type Collector struct {
	store  ports.StorageRepository
	runner ports.BlobRunner
}

func NewCollector(store ports.StorageRepository, runner ports.BlobRunner) *Collector {
	return &Collector{store: store, runner: runner}
}

func (c *Collector) Collect(ctx context.Context) error {
	live, err := c.buildLiveSet(ctx)
	if err != nil {
		return err
	}
	blobKeys, err := c.store.List(ctx, "objects/")
	if err != nil {
		return fmt.Errorf("refs.Collector.Collect: list objects: %w", err)
	}
	var orphans []ports.BlobItem
	for _, key := range blobKeys {
		hash := path.Base(key)
		if _, isLive := live[hash]; isLive {
			continue
		}
		orphans = append(orphans, ports.BlobItem{Key: key})
	}
	return c.runner.Run(ctx, orphans, func(ctx context.Context, key string) error {
		_ = c.store.Delete(ctx, key)
		return nil
	})
}
```

`NewCollector`'s new `runner` parameter ripples through every call site (production + tests) since it's a required constructor arg, not an optional decorator — matches `NewPuller`/`NewPusher`/`NewCommitter`/`NewApplier`'s existing shape.

## Implementation Plan

**Phase A — code.**
1. `internal/core/refs/collect.go` — per §Design.
2. `internal/subsystems/retention/retention.go` — `Build(...)` gains a `runner ports.BlobRunner` param, threaded into both `refs.NewCollector` calls (local GC job, remote GC job).
3. `cmd/gui/main.go` — pass the existing `runner` (the `ParallelRunner(10)` built at line 539 for pull/apply/commit/push) into both direct `refs.NewCollector` calls (per-version-delete local/remote collectors) and into `retention.Build`.

**Phase B — tests.**
1. `internal/core/refs/collect_test.go` — all 11 `refs.NewCollector(...)` call sites pass `adapters.NewSerialRunner()` (deterministic, input-order, matches the pre-existing test convention used for Pull/Push in `ritual_integration_test.go`).
2. `internal/subsystems/retention/retention_test.go`, `internal/integration/ritual_integration_test.go` — same treatment for `retention.Build`/`subretention.Build` calls and the two direct `refs.NewCollector` calls in the prune-instances test.

**Phase C — verify.**
1. `go build ./...`, `go vet ./...` clean.
2. Full `go test ./...` — every package green, including the fail-continue test (`TestCollector_ContinuesSweepWhenAnIndividualDeleteFails`) and idempotency test (`TestCollector_IsIdempotentAcrossReruns`) unchanged.
3. `golangci-lint run` on touched packages — 0 issues.

## Verification

- `TestCollector_ContinuesSweepWhenAnIndividualDeleteFails` still passes unmodified — one delete failing does not stop the sweep, confirming per-key fail-continue survived the move to `BlobRunner`.
- All other `collect_test.go`, `retention_test.go`, and the two `ritual_integration_test.go` GC-related tests pass unmodified.
- `go test ./...` (full repo, 40+ packages) green.
- No behavior change observable to a user beyond wall-clock: same orphans deleted, same fail-continue guarantee, ~30 deletes now bounded by `⌈N/10⌉` round-trips instead of `N`.

## Trade-offs

- **Lost.** Nothing functional — ctx-cancellation-mid-sweep now surfaces as an error instead of being silently swallowed (§Q4), which is a minor honesty improvement, not a loss.
- **Gained.** A GC pass with ~30 remote orphans drops from ~9-13s serial to bounded by `⌈30/10⌉ = 3` round-trips' worth of latency (~1-2s), scaling with orphan count instead of session length. Zero new tunables — rides the app's existing single concurrency budget.

## Implementation Results

- `internal/core/refs/collect.go` — `Collector` gains `runner ports.BlobRunner`; `Collect` builds a `[]ports.BlobItem` of orphans and fans deletes through `runner.Run` with an error-swallowing `fn`.
- `internal/subsystems/retention/retention.go` — `Build` signature: `Build(localStorage, remoteStorage ports.StorageRepository, bus ports.EventBus, runner ports.BlobRunner)`.
- `cmd/gui/main.go` — both `refs.NewCollector` call sites (per-version delete, `localCollector`/`remoteCollector`) and the `retention.Build` call now pass the pre-existing `runner` (`ParallelRunner(10)`, shared with pull/apply/commit/push).
- Test call sites updated in `collect_test.go` (11 sites), `retention_test.go` (1 site), `ritual_integration_test.go` (3 sites) — all pass `adapters.NewSerialRunner()`.
- `go build ./...`, `go vet ./...`, `go test ./...` (full repo), `golangci-lint run` on touched packages all clean — no deviations from plan.
- Version bump: `config.VersionMajor/Minor/Patch` 2.2.0 → 2.2.1 (internal perf fix, no API/behavior change — patch bump per semver).

No deviations from the original design.
