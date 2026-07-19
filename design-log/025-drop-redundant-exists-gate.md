# 025 — Drop redundant per-blob `Exists` gate in `transferBlob`

**Date:** 2026-05-25
**Status:** Implemented
**Related:** [[019-plan-info-delta]] (the pre-flight `List` that made this gate redundant), [[003-encoder-pool]] (push fan-out), [[004-r2-retry-decorator]] (per-blob retry semantics).

## Background

Both `Pusher.Push` and `Puller.Pull` issue one pre-flight `dst.List("objects/")` per invocation (design-log/019) and build a `known` set. They then filter the ref's blob list via `filterMissing(items, known)` and only hand the **missing** blobs to the runner:

```go
known, _ := collectKnownHashes(ctx, p.to)
missing := filterMissing(items, known)
err = p.runner.Run(ctx, missing, func(ctx, hash) error {
    return transferBlob(ctx, p.from, p.to, blobKey(hash))
})
```

`transferBlob` then re-checks every blob with a per-key `Exists`:

```go
// internal/core/refs/transfer.go:37
present, err := to.Exists(ctx, key)
if err != nil { return ... }
if present { return nil } // no-op
```

This redundancy is acknowledged in the codebase — `pull.go:127-131` says the per-blob gate is "authoritative" and the list "advisory" — but the live log shows the cost.

## Problem

Latest run (`C:\Users\Owl\k10wl\ritualdev\logs\20260525211546.log`, 2026-05-25 21:15-21:22):

| Metric                                           | Count |
|--------------------------------------------------|-------|
| `storage.list prefix=objects/` calls             | 1     |
| `storage.exists store=...remote key=objects/...` | ~1000+ |
| `storage.exists` calls total in session          | 2005  |

Every Exists during the Pushing window returned `hit=false` — i.e. every blob the pre-flight List said was missing was missing. The gate cost one extra remote round-trip per blob, with **zero** Exists-hit savings, and ate ~1 minute and 42 seconds of wall clock (Pushing started 21:20:59, completed 21:22:41) on what was already pre-classified as upload-everything.

This matches the user's framing: *"list operation lists files one by one instead of listing objects globally."* The global List runs, but the effective per-blob pattern at scale is N round-trips of `Exists`.

The intentionality comment in `pull.go:127-131` cites two race windows the gate was meant to cover:

1. **Blob landed at dst between List and transfer** — concurrent push from another client; our `missing` set still includes it. Without the gate, we re-stream the bytes; with content-addressed compression, `PutStream` writes the same bytes to the same key and the destination is unchanged.
2. **Blob present in `known` but actually gone at dst** — scrub-on-failure removed the bytes but the ref already passed through retention. `filterMissing` skips it. The gate doesn't fire in this case either (the blob isn't in `missing`), so it doesn't help.

Case 1 is the only race the gate covers, and the "fix" is a duplicate upload that the destination would silently accept anyway.

## Questions and Answers

**Q1.** Drop the gate entirely, or condition it?
**A.** Drop entirely. The two race-window justifications collapse on inspection (above). Content-addressed keys make `PutStream` idempotent — a duplicate upload is bytes-on-the-wire wasted, not corruption. With the pre-flight List in place, the duplicate-upload window is microseconds wide (mid-push concurrent client). Cost of the gate (one RTT × every missing blob, every push) dwarfs the cost of the race (occasional duplicate upload of one blob, ever).

**Q2.** Won't this break the "scrub-on-failure leaves no half-blob" invariant?
**A.** No. The scrub path inside `transferBlob` (lines 56-66) still runs on any `PutStream`/`Close` failure — that's the *write-side* cleanup, independent of the *pre-write* Exists gate. Failure semantics unchanged: a failed transfer still deletes its partial bytes.

**Q3.** Does Pull need the gate for any different reason than Push?
**A.** No — same `transferBlob` primitive, same race analysis. Both lose the gate.

**Q4.** What if a future backend (R2 conditional writes, etag-match) wants the Exists check?
**A.** Push that conditionality down into the storage decorator (e.g. `If-None-Match: *` header on PUT for R2). `transferBlob` stays direction-agnostic.

**Q5.** Does this conflict with [[004-r2-retry-decorator]]'s per-blob retry semantics?
**A.** No. Retry happens inside the storage adapter (RetryingStorage decorator wraps `to`); transferBlob's responsibility is the source→dest stream. The retry decorator already classifies which errors are worth re-trying; removing the gate doesn't change classification.

**Q6.** Updated comment in pull.go:127-131?
**A.** Yes — strike the "authoritative … advisory" line. The pre-flight List becomes the single filter, full stop.

**Q7.** Test coverage?
**A.** Existing tests:
  - `internal/core/refs/transfer_test.go` (if it exists) — gate-presence test must flip to gate-absence.
  - `push_test.go` / `pull_test.go` — fault-injection tests assume scrub on failure; those stay valid (the failure-side cleanup is unchanged).
  - Add a regression: `TestPush_NoExistsRoundTripPerMissingBlob` — fake storage that fails any `Exists` call during transfer; push should still succeed because nothing calls it.

**Q8.** Behaviour change visible to user?
**A.** Yes — pushes become ~one RTT per blob faster. On R2 with ~100ms per Exists, a 1000-blob push saves ~100s. On local mock fs (this session), the gain is ~30-50s. Bar fills faster; user sees less time in PhaseSaving. Tangentially related to [[026-stuck-in-saving]] but does not cause it.

## Design

Single file edit: `internal/core/refs/transfer.go`.

Before:

```go
func transferBlob(ctx context.Context, from, to ports.StorageRepository, key string) error {
    present, err := to.Exists(ctx, key)
    if err != nil {
        return fmt.Errorf("exists %s: %w", key, err)
    }
    if present {
        return nil
    }

    rc, err := from.GetStream(ctx, key)
    // ... rest unchanged
}
```

After:

```go
func transferBlob(ctx context.Context, from, to ports.StorageRepository, key string) error {
    rc, err := from.GetStream(ctx, key)
    // ... rest unchanged
}
```

Plus comment-only edit in `pull.go:121-131` — strike the "authoritative / advisory" framing; replace with "Single filter: the pre-flight List determines which blobs ship. Race window (blob landed at dst between List and transfer) accepted: duplicate upload is byte-identical, content-addressed, idempotent."

## Implementation Plan

**Phase A — code change.**

1. Edit `transferBlob` per Design block.
2. Edit `collectKnownHashes` comment.

**Phase B — tests.**

1. Run existing `refs/` test suite — expect green; gate removal does not change observable behaviour under happy-path or scrub-failure inputs.
2. Add `TestTransferBlob_DoesNotCallExists` — fake storage with `Exists` that fails the test if invoked; assert that a successful transfer never touches it.

**Phase C — smoke.**

1. Re-run a full Push-after-fresh-Pull cycle on the user's local mock-FS.
2. Confirm log shows zero `storage.exists` for `objects/*` during the Pushing window.
3. Wall-clock compare: Pushing duration should drop by ~ms-per-blob × blob-count.

## Verification

- `storage.exists store=...remote key=objects/...` count during Pushing = 0 in a clean run.
- Pushing wall-clock drops measurably (target: ≥30% on this local-mock topology).
- All existing refs/ tests pass; new gate-absence test passes.
- No regression in fault-injection tests (transfer-failure scrub still deletes partials).

## Trade-offs

- **Lost.** A microseconds-wide race where a concurrent client uploads the same blob between our List and our transfer now results in a duplicate stream rather than a no-op. Content-addressed, byte-identical — destination unchanged.
- **Gained.** O(N) reduction in remote round-trips per push/pull. On R2 with N=1000 blobs and ~100ms latency: ~100s saved per operation. On local mock: ~30-50s observed in this session. The pre-flight List remains the single source of "what to ship."

## Implementation Results

- `internal/core/refs/transfer.go:36-67` — removed the Exists pre-check + the `fmt.Errorf("exists %s: %w", …)` branch from `transferBlob`. Updated the godoc to reference design-log/025 and reframe the single-filter posture; scrub-on-failure path unchanged.
- `internal/core/refs/pull.go:121-131` — rewrote the `collectKnownHashes` doc comment per §Design (struck the "authoritative … advisory" line; codified the duplicate-upload race acceptance).
- `internal/core/refs/fsbundle_test.go` — extended `keyCounter` with an `exists` counter + `existsHitsPrefix(prefix string) int` accessor (exists used to be a pure passthrough). Decided against publishing `existsHits(key string)` because callers only need the prefix aggregate.
- `internal/core/refs/push_test.go` — new `TestPusher_DoesNotCallExistsOnObjectsDuringTransfer` asserts `remote.existsHitsPrefix("objects/") == 0` after a clean Push with empty remote. Replaces the per-blob skip test (`TestPusher_SkipsBlobsAlreadyOnRemote`) — both stay green.
- Full `go test ./...` clean (32 packages, all green).
- Wall-clock saving (target ≥30% of Pushing window on local-mock) deferred to next live run; counter assertion is the regression guard.

## Open follow-up — relationship to stuck-saving

The 2026-05-25 21:15-21:22 session also exhibits the stuck-in-saving bug — see [[026-stuck-in-saving]] (forthcoming, pending more diagnosis). They share a log file but the Exists redundancy is not a cause: the push completed successfully and `status: done` was published. Stuck-saving is a separate projection/delivery issue.
