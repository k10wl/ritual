# Librarian → ManifestStore Refactor Plan (v1)

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox (`- [ ]`) tracking.

**Goal:** Replace `ports.LibrarianService` (4-method Local/Remote quartet, mythic name, hidden `ApplyDefaults` mutation) with a minimal `ports.ManifestStore` port (2 methods), injected twice. Collapse side-as-method-suffix into side-as-wiring. Move default application into `Manifest` unmarshaling so persistence becomes pure codec+IO.

**Relationship to state-machine plan:** prerequisite. See `docs/superpowers/plans/2026-04-15-state-machine.md` Phase 0. State machine writes its states against `ManifestStore` directly; no Librarian references remain in new code.

**LOC target:** net **≥ −150 LOC** (excluding new test files). Measured `git diff main...HEAD --shortstat` on files in scope.

**Design principles (per user memory):** hexagonal, DI, SOLID/ISP, minimal code, tests as docs, flat code (early returns, no nested ifs, no `else`), patterns are vocabulary not templates.

**Architecture decisions:**

- **Two ports, same interface.** `ManifestStore{Get, Save}` injected twice — once for local, once for remote. Side selected at wiring, not in method names. Future consumers (state machine, GUI, Molfar-era code still alive) depend on the same port and pick a side by field name.
- **No composite `Manifests` port.** Rejected a `Snapshot/Commit` pair-port early: the "atomic commit" it implies doesn't exist across local FS + remote object store. Two `Save` calls in sequence are honest; the caller handles partial failure via `UnlockingState` (already in state-machine plan).
- **No pair value type.** Rejected `ManifestLock`/`ManifestPair`: states pass **scalars** through transitions (`lockID string`, `shouldBackup bool`, `cause error`), never `*Manifest` pointers. Historical state is the responsibility of the producing state (`LockingState` computes `shouldBackup` once; downstream states carry a bool).
- **Defaults applied on decode, not on save.** `Manifest.UnmarshalJSON` calls `ApplyDefaults` as its last step. Save path becomes pure `json.Marshal` + `storage.Put`. No hidden mutation.
- **No runtime nil-guards on ctor-set fields.** `NewManifestStore` validates storage once; methods trust their fields.
- **Errors:** `ErrEmptyData` and `ErrNilManifest` evaluated for deletion. Kept only if a callsite can reach the condition; otherwise unreachable guard code is removed.

**Tech stack:** Go stdlib (`context`, `encoding/json`, `fmt`, `errors`), existing `ports.StorageRepository`, zero new deps.

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `internal/core/ports/manifest_store.go` | `ManifestStore` interface (2 methods) |
| `internal/adapters/manifest_store.go` | adapter: storage + JSON codec |
| `internal/adapters/manifest_store_test.go` | adapter unit tests |
| `internal/core/ports/mocks/manifest_store.go` | handwritten mock matching project convention |
| `internal/core/ports/mocks/manifest_store_test.go` | mock contract smoke |

### Modified files

| Path | Change |
|---|---|
| `internal/core/domain/manifest.go` | Add `UnmarshalJSON` applying defaults as last step. Keep `ApplyDefaults` exported if still needed by tests/construction; otherwise unexport. |
| `internal/core/domain/manifest_test.go` | Add test: `Unmarshal` on `{}` produces manifest with default `MinRAMMB` etc. Keep existing `ApplyDefaults` tests (logic unchanged). |
| `cmd/cli/main.go` / `internal/app/wire.go` | Construct `NewManifestStore(localStorage)` + `NewManifestStore(remoteStorage)`. Keep Librarian construction alive during transition (Molfar still uses it). |
| `docs/structure.md` | Replace Librarian blurb with ManifestStore blurb (deferred to state-machine Phase 26 sweep). |
| `docs/progress.md` | Sprint entry: manifest-store refactor. |

### Deletion Manifest (all in Phase 6)

| Path / Symbol | Reason |
|---|---|
| `internal/core/services/librarian.go` | Replaced by `adapters.manifestStore` + `domain.Manifest.UnmarshalJSON` |
| `internal/core/services/librarian_test.go` | Coverage moved to `adapters/manifest_store_test.go` + domain test |
| `ports.LibrarianService` interface block in `internal/core/ports/ports.go` | Port removed |
| `internal/core/ports/mocks/librarian.go` | Mock removed |
| `internal/core/ports/mocks/librarian_test.go` | Mock test removed |
| `services.ErrEmptyData` | Deleted if unreachable (confirmed via callsite sweep); otherwise migrated to `ports` or `adapters` package |
| `services.ErrNilManifest` | Same — deleted if unreachable |
| `ApplyDefaults()` calls inside `Save*Manifest` paths | No longer needed — defaults apply on decode |

Callsite update only (not deleted): Molfar's construction via `NewLibrarianService` — Molfar itself dies in state-machine Phase 6, taking its Librarian dependency with it. During this plan, Molfar continues to use Librarian unchanged.

---

## Phase 1 — Port + Adapter (TDD)

### Task 1: Define `ports.ManifestStore`

- [ ] Create `internal/core/ports/manifest_store.go`:

```go
package ports

import (
    "context"

    "ritual/internal/core/domain"
)

// ManifestStore persists and retrieves a single Manifest from one side
// (local filesystem or remote object storage). Two instances are wired:
// one for local, one for remote. Side is a wiring concern — method names
// do not encode it.
//
// Get returns a fully-defaulted manifest (defaults applied in UnmarshalJSON).
// Save serializes the manifest as-is; callers apply domain mutations first.
type ManifestStore interface {
    Get(ctx context.Context) (*domain.Manifest, error)
    Save(ctx context.Context, m *domain.Manifest) error
}
```

- [ ] Commit: `feat(ports): add ManifestStore port`

---

### Task 2: Adapter tests (write before implementation)

- [ ] Create `internal/adapters/manifest_store_test.go` with cases:

| Test | Setup | Asserts |
|---|---|---|
| `TestManifestStore_Get_Happy` | storage returns valid manifest JSON | returns non-nil `*Manifest`; defaults applied |
| `TestManifestStore_Get_StorageErr` | storage returns error | error surfaces wrapped |
| `TestManifestStore_Get_BadJSON` | storage returns malformed JSON | unmarshal error surfaces wrapped |
| `TestManifestStore_Get_EmptyBytes` | storage returns `(nil, nil)` | decide: explicit error OR unreachable-and-ignore. Document chosen path. |
| `TestManifestStore_Save_Happy` | valid manifest | `storage.Put` called with `config.ManifestFilename` and JSON bytes matching expected marshal |
| `TestManifestStore_Save_StorageErr` | storage returns error | error surfaces wrapped |
| `TestManifestStore_Save_NilManifest` | `Save(ctx, nil)` | returns a typed error (`ErrNilManifest` kept in `adapters` pkg) |
| `TestManifestStore_Save_JSONIndent` | valid manifest | asserts output is indented (matches Librarian's current "  " indent for diffable manifests) |

- [ ] Use existing in-memory `StorageRepository` double from sync tests (copy pattern from `sync_integration_test.go`).
- [ ] `go test ./internal/adapters/ -run TestManifestStore` → all FAIL (no implementation yet).

---

### Task 3: Adapter implementation

- [ ] Create `internal/adapters/manifest_store.go`:

```go
package adapters

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"

    "ritual/internal/config"
    "ritual/internal/core/domain"
    "ritual/internal/core/ports"
)

var ErrNilManifest = errors.New("nil manifest")

type manifestStore struct {
    storage ports.StorageRepository
}

func NewManifestStore(s ports.StorageRepository) ports.ManifestStore {
    return &manifestStore{storage: s}
}

func (m *manifestStore) Get(ctx context.Context) (*domain.Manifest, error) {
    data, err := m.storage.Get(ctx, config.ManifestFilename)
    if err != nil {
        return nil, fmt.Errorf("manifest get: %w", err)
    }
    var out domain.Manifest
    if err := json.Unmarshal(data, &out); err != nil {
        return nil, fmt.Errorf("manifest unmarshal: %w", err)
    }
    return &out, nil
}

func (m *manifestStore) Save(ctx context.Context, man *domain.Manifest) error {
    if man == nil {
        return ErrNilManifest
    }
    data, err := json.MarshalIndent(man, "", "  ")
    if err != nil {
        return fmt.Errorf("manifest marshal: %w", err)
    }
    if err := m.storage.Put(ctx, config.ManifestFilename, data); err != nil {
        return fmt.Errorf("manifest put: %w", err)
    }
    return nil
}
```

- [ ] `go test ./internal/adapters/ -run TestManifestStore` → all PASS.
- [ ] Commit: `feat(adapters): ManifestStore (storage + JSON codec)`

---

### Task 4: Mock + contract smoke

- [ ] Create `internal/core/ports/mocks/manifest_store.go` matching project convention (pattern from `mocks/librarian.go`, but 2 methods only):

```go
package mocks

import (
    "context"

    "ritual/internal/core/domain"
    "ritual/internal/core/ports"
)

type MockManifestStore struct {
    GetFunc  func(ctx context.Context) (*domain.Manifest, error)
    SaveFunc func(ctx context.Context, m *domain.Manifest) error

    GetCalls  int
    SaveCalls int
}

var _ ports.ManifestStore = (*MockManifestStore)(nil)

func (m *MockManifestStore) Get(ctx context.Context) (*domain.Manifest, error) {
    m.GetCalls++
    if m.GetFunc != nil {
        return m.GetFunc(ctx)
    }
    return nil, nil
}

func (m *MockManifestStore) Save(ctx context.Context, man *domain.Manifest) error {
    m.SaveCalls++
    if m.SaveFunc != nil {
        return m.SaveFunc(ctx, man)
    }
    return nil
}
```

- [ ] Create `internal/core/ports/mocks/manifest_store_test.go` — one smoke test confirming the mock honors its contract (calls counted, funcs invoked, `nil`-func default no-op).
- [ ] Commit: `test(mocks): MockManifestStore`

---

## Phase 2 — Domain: defaults on decode

### Task 5: Add `Manifest.UnmarshalJSON`

- [ ] Edit `internal/core/domain/manifest.go`:

```go
func (m *Manifest) UnmarshalJSON(data []byte) error {
    type alias Manifest
    var a alias
    if err := json.Unmarshal(data, &a); err != nil {
        return err
    }
    *m = Manifest(a)
    m.ApplyDefaults()
    return nil
}
```

- [ ] Keep `ApplyDefaults` exported for now (tests reference it directly). Evaluate unexporting after callsite sweep.
- [ ] Add to `internal/core/domain/manifest_test.go`:

```go
func TestManifest_UnmarshalJSON_AppliesDefaults(t *testing.T) {
    var m Manifest
    if err := json.Unmarshal([]byte(`{}`), &m); err != nil {
        t.Fatal(err)
    }
    if m.MinRAMMB != config.DefaultMinRAMMB {
        t.Fatalf("default MinRAMMB not applied: got %d", m.MinRAMMB)
    }
    // Repeat one assertion per defaulted field touched by ApplyDefaults.
}
```

- [ ] `go test ./internal/core/domain/` → PASS (including pre-existing `ApplyDefaults` tests).
- [ ] Commit: `feat(domain): Manifest.UnmarshalJSON applies defaults on decode`

---

### Task 6: Callsite sweep — remove redundant `ApplyDefaults` calls

- [ ] `rg "ApplyDefaults" --type go` — enumerate every call site.
- [ ] For each call site: classify as (a) pre-save mutation in Librarian → **delete** (redundant with decode-time defaults), (b) test construction → **leave** (tests often build manifests without decoding), (c) explicit domain use → **evaluate**.
- [ ] Librarian `SaveLocalManifest` / `SaveRemoteManifest` drop the `manifest.ApplyDefaults()` line (Librarian still alive during transition; this is a safe edit because decode-time defaults already covered any manifest that came from `Get*Manifest`).
- [ ] `go test ./...` → green.
- [ ] Commit: `refactor(manifest): defaults on decode, drop redundant save-time apply`

---

## Phase 3 — Wiring

### Task 7: Wire `ManifestStore` alongside Librarian

- [ ] In `cmd/cli/main.go` (or `internal/app/wire.go` if composition root has moved per v2-foundation plan), construct:

```go
localManifests  := adapters.NewManifestStore(localStorage)
remoteManifests := adapters.NewManifestStore(remoteStorage)
```

- [ ] Do **not** delete Librarian construction here. Molfar still consumes it.
- [ ] When state-machine `Deps` is introduced (state-machine Phase 2), populate:

```go
Deps{
    ...
    LocalManifests:  localManifests,
    RemoteManifests: remoteManifests,
    ...
}
```

- [ ] `go build ./...` → green.
- [ ] Commit: `feat(wire): construct ManifestStore for local + remote`

---

## Phase 4 — Error unreachability sweep

### Task 8: `ErrEmptyData` and `ErrNilManifest` reachability audit

- [ ] `rg "ErrEmptyData|ErrNilManifest" --type go` — enumerate references.
- [ ] `ErrEmptyData`: confirm whether `ports.StorageRepository.Get` contract ever returns `(nil, nil)` for empty-but-present. Check adapters (`r2.go`, `fs_root.go` or equivalent). If never, delete `ErrEmptyData`.
- [ ] `ErrNilManifest`: confirm whether any current callsite can reach `Save(ctx, nil)`. Grep for `SaveLocal\|SaveRemote\|manifestStore.Save` call sites. If all callers construct non-nil, delete the guard (let Go nil-panic expose the bug).
- [ ] Update `adapters/manifest_store.go` accordingly. Update tests.
- [ ] Commit: `refactor(manifest): drop unreachable nil/empty guards` (or skip commit if both kept).

---

## Phase 5 — State-machine consumption (cross-plan)

State-machine Phase 3 (see its plan) consumes `ports.ManifestStore` directly. This plan does not own that code. Confirmation steps in §Acceptance §B ensure the port shape is ready for that consumption.

No tasks here — state-machine plan owns Phase 3.

---

## Phase 6 — Librarian deletion (post-state-machine)

Do **not** execute until state-machine Phase 6 lands (Molfar dies). At that point:

### Task 9: Delete Librarian

- [ ] Delete `internal/core/services/librarian.go`.
- [ ] Delete `internal/core/services/librarian_test.go`.
- [ ] Delete `internal/core/ports/mocks/librarian.go`.
- [ ] Delete `internal/core/ports/mocks/librarian_test.go`.
- [ ] Remove `ports.LibrarianService` block from `internal/core/ports/ports.go`.
- [ ] `rg "Librarian" --type go` → zero results.
- [ ] `rg "Librarian" docs/` — update any stragglers.
- [ ] `go build ./... && go test ./... -race` → green.
- [ ] Commit: `feat(manifest): remove LibrarianService (replaced by ManifestStore)`

---

## Service Impact

**Runtime behavior:** identical. Same on-disk format, same save order, same lock semantics. Byte-for-byte equivalent `manifest.json`.

**Consumers (internal):**
| Before | After |
|---|---|
| `librarian.GetLocalManifest(ctx)` | `localManifests.Get(ctx)` |
| `librarian.GetRemoteManifest(ctx)` | `remoteManifests.Get(ctx)` |
| `librarian.SaveLocalManifest(ctx, m)` | `localManifests.Save(ctx, m)` |
| `librarian.SaveRemoteManifest(ctx, m)` | `remoteManifests.Save(ctx, m)` |

**External API:** none exists. CLI surface unchanged. No config changes. No on-disk migration.

**Performance:** neutral. Same I/O count per run. State-machine's Running state drops 2 reads vs its old plan (Observation 4 from design discussion).

**Event stream:** unchanged (the port doesn't emit events; existing callers emit the same ones).

---

## Acceptance Criteria

### A. Code-level

- [ ] `ports.ManifestStore` exists with exactly `Get(ctx) (*Manifest, error)` and `Save(ctx, *Manifest) error`. No other methods.
- [ ] Two instances wired (local + remote) at composition root.
- [ ] `Manifest.UnmarshalJSON` calls `ApplyDefaults` as last step.
- [ ] No `Save*Manifest` path calls `ApplyDefaults` (sweep green).
- [ ] `ErrEmptyData` / `ErrNilManifest`: either deleted or justified in adapter godoc.
- [ ] Net LOC delta negative. `git diff main...HEAD --shortstat` on files in scope shows deletions > additions (excluding new tests which may inflate additions — measure non-test files separately).

### B. Port-ready for state-machine

- [ ] `ports.ManifestStore` importable from `internal/core/statemachine/` without cycles.
- [ ] `mocks.MockManifestStore` usable in state tests without adapter imports.
- [ ] State-machine plan's find-replace pass compiles when it lands.

### C. Test-level

- [ ] `go test ./... -race -count=1` green.
- [ ] `internal/adapters/manifest_store_test.go` covers all 8 cases from Task 2.
- [ ] `internal/core/domain/manifest_test.go` asserts defaults-on-decode.
- [ ] Coverage on `internal/adapters/manifest_store.go` ≥ 90%.
- [ ] No test file references `Librarian` after Phase 6.

### D. Behavior-level

- [ ] `sync_integration_test.go` passes with unchanged body (only construction swap at wiring).
- [ ] Manual E2E smoke (§Verification Step 7) produces byte-identical `manifest.json` to pre-refactor baseline.
- [ ] Lock-and-kill scenario: `LockedBy` persists identically; next run reports identical error text (or documented improved text).

### E. Doc-level

- [ ] State-machine plan has Phase 0 stub linking to this plan.
- [ ] State-machine plan's `LibrarianService` references replaced with `ManifestStore` + two-instance wording (may be deferred until Phase 0 merges; document the staging).
- [ ] `docs/progress.md` entry added.
- [ ] `docs/structure.md` updated (may piggy-back on state-machine Phase 26 sweep).

---

## Verification (double-check procedure)

Run after all phases complete. Each step independently re-runnable.

**Step 1 — Static sweep**

```
rg "LibrarianService" --type go          # expect: empty
rg "Librarian" --type go                  # expect: empty
rg "ApplyDefaults" --type go              # expect: only definition + UnmarshalJSON call + legitimate test construction
rg "ManifestStore" --type go              # expect: port, adapter, mock, deps, state files
```

**Step 2 — Build + vet**

```
go build ./...
go vet ./...
```

Both green.

**Step 3 — Full test run**

```
go test ./... -count=1 -race -timeout 120s
```

Green. Elapsed time not worse than baseline.

**Step 4 — LOC verification**

```
git diff main...HEAD --shortstat -- "internal/**/*.go"
```

Deletions must exceed additions on non-test files. Target: −150 LOC or better.

**Step 5 — Interface surface diff**

Document before/after:
- Before: `LibrarianService` — 4 methods, 1 instance, 4 unique callsites per lifecycle.
- After: `ManifestStore` — 2 methods, 2 instances, 2 callsites per side per lifecycle.

Confirm mock surface halved (`mocks/librarian.go` ~120 LOC → `mocks/manifest_store.go` ~60 LOC).

**Step 6 — Integration green**

```
go test ./internal/core/services/ -run TestSync -count=1
```

No body edits in `sync_integration_test.go`. Passes.

**Step 7 — Manual smoke (E2E, Windows)**

Baseline (pre-merge):
1. Fresh clone of `main`.
2. Run CLI. Capture `manifest.json` bytes locally + remote object hash.
3. Capture event stream (stdout log).
4. Repeat with kill mid-run; capture `LockedBy` state.

Refactored:
1. Checkout feat branch.
2. Re-run same steps.

Diff:
- `manifest.json` bytes — identical.
- Event stream — identical in event types, count, order.
- `LockedBy` state after kill — identical.

Any diff outside documented exceptions (e.g. improved error text) blocks merge.

**Step 8 — Boundary probes**

- Corrupt local `manifest.json` → `Get` returns wrapped unmarshal error; same classification as pre-refactor.
- Missing remote manifest → `Get` returns wrapped not-found error; same classification.
- `Save(nil)` if reachable anywhere → returns `ErrNilManifest`; otherwise unreachable (confirmed in Phase 4).

**Step 9 — Plan sync**

Open `docs/superpowers/plans/2026-04-15-state-machine.md`. Verify:
- Phase 0 stub exists and links here.
- No `LibrarianService` or `librarian.Get*/Save*` references remain in phases that describe new code (state files, factory, Deps).

**Step 10 — Completion review**

Per `superpowers:verification-before-completion`:
- Read `adapters/manifest_store.go` — fields used only by methods that exist.
- Read `ports/manifest_store.go` — no extra methods added "for later".
- Read each state file that consumes `ManifestStore` — no `*Manifest` pointers threaded through ctors beyond `Save` arguments.
- `releaseLock` helper (if introduced per state-machine Observation 5β) is the only writer of `LockedBy = ""`.

---

## Rollback

Any verification step fails:
1. `git revert` the offending commit.
2. Because this plan is additive until Phase 6, reverting at any earlier phase leaves the codebase compilable with Librarian still present.
3. Phase 6 (Librarian deletion) is the single irreversible point — and it only runs after state-machine Phase 6 already committed the broader migration. If Phase 6 reveals a problem, revert it together with the state-machine Phase-6 delete-Molfar commit.

---

## Open Questions

1. `ErrEmptyData` reachability — confirm with storage adapters' `Get` contracts during Phase 4. Decision recorded in adapter godoc.
2. Should `ApplyDefaults` be unexported after the refactor? Decision deferred to Phase 6 review; depends on whether any test-only constructor path still calls it explicitly.
3. Adapter error wrapping style (`manifest get:` vs current `failed to get local manifest:`) — decision: shorter wrapping, document in PR description. If reviewers prefer verbatim preservation, adjust.

---

## Sequencing vs state-machine plan

| When | This plan | State-machine plan |
|---|---|---|
| Now | Phases 1–4 (port, adapter, domain, wiring, sweep) | Idle (plan doc only) |
| After Phase 4 merges | Phase 5 no-op (port is ready) | Phase 0 references this plan as done; Phases 1–5 proceed, consuming `ManifestStore` directly |
| State-machine Phase 6 | — | Molfar deleted; Librarian orphaned |
| This plan Phase 6 | Librarian symbol deletion | — |

No overlap in code changes. Two PR chains, serial merges.
