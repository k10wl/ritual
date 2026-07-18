# Retry Coverage — Network Operation Audit

## Status: Plan — prerequisite P-B for [2026-04-15-state-machine.md](./2026-04-15-state-machine.md) (see Phase 0)

> **Ordering with sibling prereq P-A:** [`2026-04-16-manifest-store.md`](./2026-04-16-manifest-store.md) deletes Librarian and introduces `ManifestStore` before the state machine lands. This retry plan is **independent of that refactor** — retry lives at the R2 adapter (see below), so it is correct regardless of which services consume `StorageRepository`. Can ship before, after, or in parallel with P-A.

**Authoritative references:**
- [`docs/event-architecture.md`](../../event-architecture.md) — bus topology; `RetryAttemptInfo` event catalog entry.
- `internal/adapters/retry/retry.go` — generic retry util (commit `b6ed020`). `Do[T]` / `DoVoid` / `Fatal` / `IsFatal`.

## Goal

Every network-touching operation routes through `retry.Do` (see `internal/adapters/retry/retry.go`). Today only `SyncService` retries; every other R2 callsite fails on first transient error. This plan closes the gap.

The state-machine sprint assumes transient network flakes never reach `FailedState`. That assumption is currently false at ~8 network callsites. Fix before state-machine lands.

---

## Design (linked)

- Retry util: `internal/adapters/retry/retry.go` (already merged — commit `b6ed020`).
- `retry.Do[T]` / `DoVoid` wrap `avast/retry-go/v4` with defaults (5 attempts, 1s→15s backoff).
- `testing.Testing()` zeroes delays under `go test`.
- `retry.Fatal` marks logic errors as non-retryable.
- Panics inside `fn` become Fatal automatically.

---

## Principle

**Every operation crossing the wire must retry. Local disk ops must not.**

Classification rule:
- Touches `S3Client` / R2 / HTTP / any `net.*` → retry.
- Touches `os.*` / `*os.Root` / FS-only adapter → no retry (different failure mode; retry masks bugs).
- Crosses a port whose implementation *might* be network-backed → retry at the port boundary (safer default).

---

## Network-Touching Callsites

### Already retried (via `RetryStorageRepository` wrapper)

| File | Line | Op | Wrapped by | Status |
|---|---|---|---|---|
| `cmd/cli/main.go` | 119 | builds `retryRemote` decorator | `NewRetryStorageRepository` | ✅ covered |
| `internal/core/services/sync.go` | 129, 193, 205, 208, 219, 227, 229 | `s.remote.{Get,Put,Copy,Delete,DeleteBatch,List}` | `SyncService` receives `retryRemote` | ✅ covered |

### NOT retried (gaps — must fix)

All gaps route through the same `StorageRepository` interface against the R2 adapter. Retry placed at the R2 adapter closes all of them uniformly, regardless of which service (current or post-refactor) holds the reference.

| # | Callsite (current) | Post-refactor equivalent | Op | Failure mode today |
|---|---|---|---|---|
| G1 | `librarian.go:71` `remoteStorage.Get("manifest.json")` | `adapters/manifest_store.go` Get (per manifest-store plan) | manifest fetch | first R2 blip → "failed to get remote manifest" |
| G2 | `librarian.go:131` `remoteStorage.Put("manifest.json", data)` | `adapters/manifest_store.go` Save | manifest save | first R2 blip → manifest save fails |
| G3 | `updater_ritual.go:95` `storage.Get(RemoteBinaryKey)` | unchanged | self-update binary download | first R2 blip → self-update aborts |
| G4 | `retention.go:41` `storage.List(prefix)` | unchanged (R2 retention) | retention listing | first R2 blip → retention fails |
| G5 | `retention.go:51` `storage.DeleteBatch(toDelete)` | unchanged | retention delete | first R2 blip → orphan backups |
| G6 | `backup.go:27` `storage.List(srcPrefix)` (when called with remote) | unchanged | remote backup listing | first R2 blip → remote backup fails |
| G7 | `backup.go:33` `storage.Copy(key, backup/key)` | unchanged | backup copy | first R2 blip → partial backup |
| G8 | `backup.go:43` `storage.Put(backup/manifest.json, data)` | unchanged | backup manifest write | first R2 blip → backup without manifest |

**Key observation:** all 8 gaps are R2 method calls through `StorageRepository`. Retry inside the R2 adapter covers every one — caller identity (Librarian, ManifestStore, Retention, Molfar, future state-machine states) is irrelevant.

### Local-only (must NOT retry)

| File | Reason |
|---|---|
| `cmd/cli/main.go:88` `localStorage` (FS repo on `workRoot`) | Local disk; retry masks permission/disk-full bugs. |
| `internal/core/services/retention_logs.go` | Log retention is local-only FS. |
| `internal/core/services/backup.go` (local-backup branch, `molfar.go:402`) | `CreateBackup(ctx, m.localStorage, ...)` — local disk. |
| `internal/adapters/fs.go`, `fullscanner.go`, `mtimescanner.go`, `ritualsync.go` | FS. |
| `internal/adapters/commandexecutor.go`, `serverrunner.go` | Process management, not network. |
| `internal/adapters/javainfo.go`, `systeminfo_windows.go` | System introspection. |

---

## Non-Storage Network Ops

### Today — none beyond R2

Scan verified:
- `grep -r "net/http\|http.Client\|websocket\|grpc\|net.Dial\|net.Lookup"` → **zero hits** in `internal/` and `cmd/`.
- Only network dependency in `go.mod`: `github.com/aws/aws-sdk-go-v2/*` (R2 client).
- All network I/O funnels through `S3Client` inside `internal/adapters/r2.go`, exposed as `StorageRepository`.

The audit is exhaustive **as of commit** `b6ed020`.

### Future surfaces — must be wrapped at introduction time

The following are likely to appear in upcoming sprints. Each is a new network surface requiring its own retry coverage:

| Surface | Likely arrival | Retry placement |
|---|---|---|
| HTTP API on engine (GUI sprint, Wails backend calls) | near-term | **local only** — no retry for `localhost` IPC |
| Standalone update checker (outside R2) | possible | wrap HTTP client at adapter boundary |
| OAuth / auth token refresh | possible | wrap at token-exchange adapter |
| Telemetry / crash reporting | possible | wrap at reporter adapter; use `rg.Attempts(2)` or lower (best-effort) |
| DNS lookups (custom) | rare | stdlib retries internally; no action unless proven flaky |
| Any new cloud provider (S3 alt, CDN) | possible | wrap at `StorageRepository` implementation; auto-covered |

### Standing rule

**Any new adapter whose constructor takes credentials, a URL, a `*http.Client`, or any SDK talking to a network service MUST route its network calls through `retry.Do` (or be wrapped in a retrying port decorator).**

Enforcement:
- Code review checklist item.
- `grep` patterns for imports of `net/http`, `net.Dial`, `*grpc.ClientConn` in PR diffs should flag for retry discussion.
- Unit test: if adapter has a "happy path" test, it should also have a "transient failure → retry → success" test. Absence = red flag.

### Explicitly excluded from retry

Even if they appear, these are NOT retry candidates:
- **Local HTTP IPC** (Wails JS↔Go, localhost UI backend). No wire crossed. Retry masks programming bugs.
- **Unix/Windows pipe or socket to local process** (e.g. server stdout pipe). Process crash, not network flake.
- **OS signal handling** (shutdown handler). Not network.
- **File watches** (`fsnotify`, if ever used). FS, not network.

---

## Fix Strategy

**Inline retry at the R2 adapter — zero service-code changes, zero composition-root changes.**

Retry lives inside `internal/adapters/r2.go`. Every `StorageRepository` method on `R2Repository` wraps its `S3Client` call with `retry.Do`. Classifier (`r2Retryable`) lives next to the adapter. Hook (`onAttempt`) set at construction time.

```go
// internal/adapters/r2.go (per-method shape)

func (r *R2Repository) Get(ctx context.Context, key string) ([]byte, error) {
    return retry.Do(ctx, func(ctx context.Context) ([]byte, error) {
        result, err := r.client.GetObject(ctx, &s3.GetObjectInput{
            Bucket: aws.String(r.bucket),
            Key:    aws.String(key),
        })
        if err != nil { return nil, err }
        // ... existing body ...
    }, rg.RetryIf(r2Retryable), rg.OnRetry(r.onAttempt))
}
```

Consumers (Librarian / ManifestStore / Retention / RitualUpdater / Molfar / SyncService / future state machine) are untouched. Every `remoteStorage.X()` call they already make now retries automatically.

### Why inline, not decorator

- Only one network-backed adapter exists (R2). Decorator is ceremony for no gain.
- Classifier is R2-specific — it lives naturally with the adapter that produces the errors.
- Impossible to forget at wiring — R2 always retries, no wrapper to wire.
- Layering stays honest: retry is an infrastructure concern at the adapter boundary, not a service concern.
- LOC: delete `retrystorage.go` entirely.

### Why not service-level (e.g. at ManifestStore)

- Only covers manifest ops; binary download (G3), retention (G4/G5), backup (G6–G8) still uncovered.
- Forces every service to learn about retry → infrastructure leaks into domain.
- Classifier duplicates across services.
- If `StorageRepository` ever gets a non-network implementation (in-memory for tests, FS adapter already exists), service-level retry would incorrectly wrap those too.

Retry at R2 is the only placement that covers every gap with no leaks.

---

## Tasks

### Task 0: Verify audit is still current

- [ ] Re-run network scan before starting implementation:
  ```
  grep -rE "net/http|http\.Client|websocket|grpc\.|net\.Dial|net\.Lookup|tls\." internal/ cmd/
  ```
- [ ] If new hits appear → update the "Non-Storage Network Ops" table and add corresponding tasks.
- [ ] Check `go.mod` for new network deps since commit `b6ed020`.

### Task 1: Add R2 error classifier

- [ ] Create `internal/adapters/r2_classify.go`:
  - Function `r2Retryable(err error) bool` (unexported — only R2 adapter calls it).
  - Permanent (return `false`): `NoSuchKey`, `NoSuchBucket`, `AccessDenied`, `InvalidAccessKeyId`.
  - Retryable (return `true`): `SlowDown`, `RequestTimeout`, `InternalError`, `ServiceUnavailable`, `net.Error`, any 5xx HTTP.
  - Fatal short-circuit: `if retry.IsFatal(err) { return false }`.
  - Unknown: default `true` (optimistic; flip if noisy in prod).
- [ ] Create `internal/adapters/r2_classify_test.go` — table-driven, one row per error type.
- [ ] Commit: `feat(r2): classifier distinguishing transient vs permanent errors`.

### Task 2: Inline retry into `R2Repository`

- [ ] Edit `internal/adapters/r2.go`:
  - Add field `onAttempt func(n uint, err error)` to `R2Repository` (default `nil` = silent).
  - Constructor accepts optional hook (keep existing signature compatible; new overload or variadic option).
  - Wrap every `S3Client` call in `Get`, `Put`, `Delete`, `DeleteBatch`, `List`, `Copy` with `retry.Do` / `retry.DoVoid`.
  - Pass `rg.RetryIf(r2Retryable)` and `rg.OnRetry(r.onAttempt)` (when non-nil) as extra options.
  - Progress events (`UploadProgress`, etc.) stay inside the retried closure — on retry the progress re-emits, UI re-renders from zero for that item (acceptable; progress is per-attempt).
- [ ] Update `internal/adapters/r2_test.go`:
  - Existing unit tests unchanged (test delays zeroed via `testing.Testing()`).
  - Add one test per method: inner `S3Client` fails twice → retry → succeeds. Assert call count on the mock.
- [ ] Commit: `feat(r2): inline retry on all S3Client operations`.

### Task 3: Delete `RetryStorageRepository`

- [ ] Delete `internal/adapters/retrystorage.go`.
- [ ] Delete `internal/adapters/retrystorage_test.go` (if present).
- [ ] Edit `cmd/cli/main.go`:
  - Delete line 119 `retryRemote := adapters.NewRetryStorageRepository(...)`.
  - Rename usages of `retryRemote` back to `remoteStorage` at sync callsites (177, 183).
  - Add `onAttempt` hook at R2 construction (line 92) that publishes retry events to `events` channel.
- [ ] Commit: `refactor(wiring): drop decorator, R2 retries inline`.

### Task 4: Add `RetryAttemptInfo` event

- [ ] Add `ports.RetryAttemptInfo` to `internal/core/ports/events.go` (matches existing event shape — sealed interface today, Stringer post state-machine).
- [ ] Add one smoke test for `String()` (or equivalent for current event shape).
- [ ] Commit: `feat(ports): add RetryAttemptInfo event`.

### Task 5: Verify gap closure

- [ ] Manual test: inject network failure mid-run (unplug, firewall block), assert manifest/binary/backup/retention operations all recover, run completes.
- [ ] All existing tests pass (`go test ./...`). Retry delays are zero under tests — no regressions in test duration.
- [ ] Measure test suite duration before/after; no file should gain > 100ms.

---

## Non-Goals

- **Do not** add retry to local FS adapters. Different failure semantics.
- **Do not** introduce a `Policy` struct. Defaults live in `retry` package; overrides via `rg.Option` at call site.
- **Do not** build per-operation retry inside SyncService loops. Port-level decorator suffices until proven otherwise.
- **Do not** wait for state-machine sprint. This plan is a prerequisite — ships standalone.

---

## Out of Scope

- **Idempotency review** for lock acquire / batch ops under retry — acceptable as-is (R2 object ops are idempotent by key; `DeleteBatch` is idempotent). Revisit if/when non-idempotent remote ops are added.
- **Retry metrics** (counters, latency histograms) — observability bus comes with state-machine sprint; wire then.
- **Per-adapter policy overrides** (e.g. initial connectivity check with `Attempts(3)`) — add inline via `rg.Attempts(3)` only when a real case arises.

---

## Tech Stack

- `github.com/avast/retry-go/v4` — already in `go.mod`.
- `internal/adapters/retry` — merged, ready.
- Stdlib: `errors`, `net`, `github.com/aws/smithy-go` (for R2 error classification).

---

## Prerequisite Relationship

This plan MUST land before `2026-04-15-state-machine.md` Phase 6 begins. State-machine's `FailedState` semantics assume `ErrorInfo` events reflect real permanent failures, not first-try transient flakes. Without retry coverage, every network blip = state machine failure = user-visible error. With retry coverage, only exhausted-retry errors propagate to `FailedState`, matching the intended UX.

---

## Estimated LOC — Inline Approach

| Change | File | Production Δ | Test Δ |
|---|---|---|---|
| Delete `RetryStorageRepository` | `internal/adapters/retrystorage.go` | **−85** | — |
| Delete its tests (if any) | `internal/adapters/retrystorage_test.go` | — | (none exist today) |
| Add classifier | `internal/adapters/r2_classify.go` | **+30** | — |
| Classifier table test | `internal/adapters/r2_classify_test.go` | — | **+40** |
| Inline retry — 6 methods | `internal/adapters/r2.go` | **+20** (each method gains `retry.Do` wrapper; ~3 LOC × 6 = 18 + 2 for `onAttempt` field) | — |
| Retry smoke per method | `internal/adapters/r2_test.go` | — | **+50** (one "fail-twice-then-succeed" test per method, ~8 LOC each) |
| Wiring cleanup in `main.go` | `cmd/cli/main.go` | **−3** (delete line 119, rename `retryRemote` → `remoteStorage` at lines 177/183) | — |
| Build `onAttempt` hook at R2 ctor | `cmd/cli/main.go` | **+5** | — |
| `RetryAttemptInfo` event | `internal/core/ports/events.go` | **+12** | **+10** |
| **Total** | | **−21** production | **+100** tests |

**Net production code: shrinks by ~21 LOC.** Deleting the decorator is a bigger win than adding inline retry + classifier. Ratio stands because:
- Decorator had 6 method-wrapping bodies (6 × ~10 LOC).
- Inline has 6 method-wrapping bodies (6 × ~3 LOC — shorter because `retry.Do` takes a closure, no option struct to build).
- Classifier (+30) offset by decorator deletion (−85) → net −55 from the R2 surface alone.

**Net test code: grows by ~100 LOC** — one retry-behavior test per R2 method + classifier table. All tests run with zero delay (`testing.Testing()`).

---

## What Actually Changes — Concrete Diff Summary

### New files (2)

1. **`internal/adapters/r2_classify.go`** (~30 LOC)
   - `r2Retryable(err error) bool` — unexported classifier.

2. **`internal/adapters/r2_classify_test.go`** (~40 LOC)
   - Table test: one row per error type.

### Modified files (3)

3. **`internal/adapters/r2.go`** (+~20 LOC)
   - Add field `onAttempt func(uint, error)` on `R2Repository`.
   - Wrap each of `Get` / `Put` / `Delete` / `DeleteBatch` / `List` / `Copy` in `retry.Do` or `retry.DoVoid`.

4. **`cmd/cli/main.go`** (+2 LOC net: +5, −3)
   - Add `onAttempt` closure publishing `RetryAttemptInfo` to `events` channel.
   - Pass `onAttempt` into `NewR2Repository` (via new param or option).
   - Delete `retryRemote := adapters.NewRetryStorageRepository(...)` line.
   - Rename two usages `retryRemote` → `remoteStorage`.

5. **`internal/core/ports/events.go`** (+12 LOC)
   - Add `RetryAttemptInfo` struct (matching existing event shape in codebase).

### Deleted files (1)

6. **`internal/adapters/retrystorage.go`** (−85 LOC)
   - Entire file gone.

### Untouched (intentional)

- All service files (`librarian.go`, `updater_ritual.go`, `retention.go`, `backup.go`, `molfar.go`, `sync.go`): **zero changes**. They already call `storage.X()` through the port; retry happens inside the R2 adapter transparently.
- All tests for those services: **zero changes**.
- `internal/adapters/retry/retry.go`: already merged, unchanged.

---

## Commit Sequence (4 commits)

1. `feat(r2): classifier distinguishing transient vs permanent errors` — Task 1.
2. `feat(r2): inline retry on all S3Client operations` — Task 2.
3. `refactor(wiring): drop RetryStorageRepository, R2 retries inline` — Task 3 (includes file delete).
4. `feat(ports): add RetryAttemptInfo event` — Task 4.

Each commit stands alone and keeps the tree green.

Single sprint. No architectural dependencies on unbuilt infrastructure. Ships independently of both the manifest-store refactor and the state-machine migration.
