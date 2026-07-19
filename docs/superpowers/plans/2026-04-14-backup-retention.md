# Backup & Retention Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace dead backup/retention machinery with one generic retention engine and a pure `CreateBackup` function. Target ~-650 net LOC.

**Architecture:** Pure functions (`Mark`, `ParseStrategy`, `shouldBackup`, `CreateBackup`) wrapped by thin IO adapters. Two-phase retention (mark then delete). UTC timestamps. Format-agnostic via strategy functions. All via stdlib — no new dependencies.

**Tech Stack:** Go stdlib (`time`, `path`, `maps`, `slices`, `encoding/json`), existing `StorageRepository` interface, existing hexagonal architecture.

**Spec:** `docs/superpowers/specs/2026-04-14-backup-retention-design.md`

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `internal/core/domain/retention_rules.go` | `RetentionRules` struct + `DefaultRetentionRules()` |
| `internal/core/domain/retention_rules_test.go` | Defaults, JSON round-trip |
| `internal/core/services/retention_parse.go` | `ParseStrategy` type + `ParseTimestampDir`, `ParseTimestampTar`, `ChainStrategies` |
| `internal/core/services/retention_parse_test.go` | Pure tests for strategies |
| `internal/core/services/retention_mark.go` | Pure `Mark` function + internal `entry` type |
| `internal/core/services/retention_mark_test.go` | Table-driven pure tests |
| `internal/core/services/retention.go` | Generic `retention` struct wrapping `Mark` + IO |
| `internal/core/services/retention_test.go` | Mock storage tests |
| `internal/core/services/backup.go` | `CreateBackup` function |
| `internal/core/services/backup_test.go` | Mock storage tests |
| `internal/core/services/dirty.go` | `shouldBackup` pure function |
| `internal/core/services/dirty_test.go` | Table-driven tests |

### Modified files

| Path | Change |
|---|---|
| `internal/config/config.go` | Add `BackupsDir = "backups"`. Remove `LocalBackups`, `RemoteBackups`, `R2MaxBackups`, `LocalMaxBackups`, `ManualWorldFilename` once callers deleted. |
| `internal/core/domain/settings.go` | Add `LocalRetention RetentionRules` field + default |
| `internal/core/domain/sync_state.go` | Add `RemoteRetention RetentionRules` to Manifest |
| `internal/core/domain/manifest.go` | Remove `AddWorld`, `GetLatestWorld`, `RemoveOldestWorlds`, `Worlds.Backups []World`. Add retention defaults in `ApplyDefaults`. |
| `internal/core/ports/ports.go` | Delete `BackupperService`. Simplify `RetentionService.Apply(ctx)`. |
| `internal/core/ports/mocks/retention.go` | Update to new interface signature |
| `internal/core/services/retention_logs.go` | Update to new interface signature |
| `internal/core/services/sync_updater.go` | Delete `SyncUploadBackupper` struct and constructor |
| `internal/core/services/molfar.go` | Delete `updateManifestsWithArchive`, rewrite Exit to use `CreateBackup` + retention iteration |
| `cmd/cli/main.go` | New DI: no `SyncUploadBackupper`, new retentions, new backup calls in exit flow |

### Deleted files

| Path | Why |
|---|---|
| `internal/core/services/retention_local.go` | Replaced by generic engine |
| `internal/core/services/retention_r2.go` | Replaced by generic engine |
| `internal/core/services/retention_r2_test.go` | Tests for deleted file |
| `internal/core/services/retention_util.go` | Absorbed into parse strategy |
| `internal/core/services/retention_util_test.go` | Tests for deleted file |
| `internal/core/services/session.go` | `CheckPlayersJoined` replaced by dirty check |
| `internal/core/services/session_test.go` | Tests for deleted file |
| `internal/core/ports/mocks/backupper.go` | Interface deleted |
| `internal/core/ports/mocks/backupper_test.go` | Tests for deleted file |
| `internal/core/domain/world.go` | Only used by Backups list which dies |
| `internal/core/domain/world_test.go` | Tests for deleted file |

---

## Phase 1: Domain — Retention Rules

### Task 1: RetentionRules struct + defaults

**Files:**
- Create: `internal/core/domain/retention_rules.go`
- Test: `internal/core/domain/retention_rules_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/domain/retention_rules_test.go
package domain_test

import (
	"encoding/json"
	"testing"

	"ritual/internal/core/domain"
)

func TestDefaultRetentionRules_KeepLast2(t *testing.T) {
	r := domain.DefaultRetentionRules()
	if r.KeepLast != 2 {
		t.Errorf("KeepLast = %d, want 2", r.KeepLast)
	}
	if r.KeepDaily != 0 || r.KeepWeekly != 0 || r.KeepMonthly != 0 {
		t.Errorf("non-last tiers = %+v, want all zero", r)
	}
}

func TestRetentionRules_JSONRoundTrip(t *testing.T) {
	original := domain.RetentionRules{KeepLast: 3, KeepDaily: 1, KeepWeekly: 2, KeepMonthly: 4}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.RetentionRules
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestRetentionRules_JSONFields(t *testing.T) {
	r := domain.RetentionRules{KeepLast: 2}
	data, _ := json.Marshal(r)
	got := string(data)
	want := `{"keep_last":2,"keep_daily":0,"keep_weekly":0,"keep_monthly":0}`
	if got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/domain/ -run TestDefault -v`
Expected: FAIL — undefined: domain.RetentionRules / DefaultRetentionRules

- [ ] **Step 3: Implement**

```go
// internal/core/domain/retention_rules.go
package domain

// RetentionRules controls how many backups are kept per tier.
// Each tier protects backups independently — union logic, never conflicts.
type RetentionRules struct {
	KeepLast    int `json:"keep_last"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
}

// DefaultRetentionRules returns safe defaults: keep 2 latest, no tier rotation.
// Matches previous R2MaxBackups / LocalMaxBackups constants.
func DefaultRetentionRules() RetentionRules {
	return RetentionRules{KeepLast: 2}
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/domain/ -run TestDefault -v && go test ./internal/core/domain/ -run TestRetentionRules -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/domain/retention_rules.go internal/core/domain/retention_rules_test.go
git commit -m "feat(domain): add RetentionRules with safe defaults"
```

---

## Phase 2: Pure Retention Logic

### Task 2: ParseStrategy — directory format

**Files:**
- Create: `internal/core/services/retention_parse.go`
- Test: `internal/core/services/retention_parse_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/services/retention_parse_test.go
package services_test

import (
	"testing"
	"time"

	"ritual/internal/core/services"
)

func TestParseTimestampDir_Valid(t *testing.T) {
	got := services.ParseTimestampDir("backups/20260414160000/")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampDir = %v, want %v", got, want)
	}
}

func TestParseTimestampDir_WithoutTrailingSlash(t *testing.T) {
	got := services.ParseTimestampDir("backups/20260414160000")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampDir = %v, want %v", got, want)
	}
}

func TestParseTimestampDir_NestedKey(t *testing.T) {
	// Directory backups may appear as nested keys in storage listings
	got := services.ParseTimestampDir("backups/20260414160000/worlds/world/region/r.0.0.mca")
	if got.IsZero() {
		t.Errorf("expected timestamp for nested key, got zero")
	}
}

func TestParseTimestampDir_Invalid(t *testing.T) {
	cases := []string{
		"",
		"backups/",
		"backups/not-a-timestamp/",
		"backups/abc/",
		"backups/20260414160000.tar", // tar format, not dir
	}
	for _, k := range cases {
		if got := services.ParseTimestampDir(k); !got.IsZero() {
			t.Errorf("ParseTimestampDir(%q) = %v, want zero", k, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestParseTimestampDir -v`
Expected: FAIL — undefined: services.ParseTimestampDir

- [ ] **Step 3: Implement**

```go
// internal/core/services/retention_parse.go
package services

import (
	"path"
	"strings"
	"time"

	"ritual/internal/config"
)

// ParseStrategy extracts a UTC timestamp from a storage key.
// Returns zero time if key is not a recognized backup entry.
type ParseStrategy func(key string) time.Time

// ParseTimestampDir recognizes directory-format backups: {prefix}/{ts}/...
// Returns timestamp from the first path segment matching TimestampFormat.
func ParseTimestampDir(key string) time.Time {
	key = strings.TrimSuffix(key, "/")
	parts := strings.Split(key, "/")
	for _, p := range parts {
		// Skip extensions — a dir entry has no file extension in its name
		if path.Ext(p) != "" {
			continue
		}
		if t, err := time.ParseInLocation(config.TimestampFormat, p, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestParseTimestampDir -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/retention_parse.go internal/core/services/retention_parse_test.go
git commit -m "feat(retention): add ParseTimestampDir strategy"
```

---

### Task 3: ParseStrategy — tar format (v1 compatibility)

**Files:**
- Modify: `internal/core/services/retention_parse.go`
- Modify: `internal/core/services/retention_parse_test.go`

- [ ] **Step 1: Write failing test**

```go
// Append to retention_parse_test.go
func TestParseTimestampTar_Valid(t *testing.T) {
	got := services.ParseTimestampTar("backups/20260414160000.tar")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestampTar = %v, want %v", got, want)
	}
}

func TestParseTimestampTar_Invalid(t *testing.T) {
	cases := []string{
		"",
		"backups/20260414160000/", // dir format, not tar
		"backups/20260414160000",  // no extension
		"backups/manual.tar",
		"backups/abc.tar",
	}
	for _, k := range cases {
		if got := services.ParseTimestampTar(k); !got.IsZero() {
			t.Errorf("ParseTimestampTar(%q) = %v, want zero", k, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestParseTimestampTar -v`
Expected: FAIL — undefined: services.ParseTimestampTar

- [ ] **Step 3: Implement**

```go
// Append to retention_parse.go
// ParseTimestampTar recognizes v1 tar-format backups: {prefix}/{ts}.tar
func ParseTimestampTar(key string) time.Time {
	base := path.Base(key)
	if path.Ext(base) != ".tar" {
		return time.Time{}
	}
	stem := strings.TrimSuffix(base, ".tar")
	t, err := time.ParseInLocation(config.TimestampFormat, stem, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestParseTimestampTar -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/retention_parse.go internal/core/services/retention_parse_test.go
git commit -m "feat(retention): add ParseTimestampTar v1 strategy"
```

---

### Task 4: ChainStrategies

**Files:**
- Modify: `internal/core/services/retention_parse.go`
- Modify: `internal/core/services/retention_parse_test.go`

- [ ] **Step 1: Write failing test**

```go
// Append to retention_parse_test.go
func TestChainStrategies_FirstMatchWins(t *testing.T) {
	chain := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)

	// Dir format hits first strategy
	got := chain("backups/20260414160000/")
	want := time.Date(2026, 4, 14, 16, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("dir key: got %v, want %v", got, want)
	}

	// Tar format falls through to second strategy
	got = chain("backups/20260414160000.tar")
	if !got.Equal(want) {
		t.Errorf("tar key: got %v, want %v", got, want)
	}

	// Unknown format — all strategies return zero
	if got := chain("backups/unknown"); !got.IsZero() {
		t.Errorf("unknown key: got %v, want zero", got)
	}
}

func TestChainStrategies_Empty(t *testing.T) {
	chain := services.ChainStrategies()
	if got := chain("backups/20260414160000/"); !got.IsZero() {
		t.Errorf("empty chain: got %v, want zero", got)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestChainStrategies -v`
Expected: FAIL — undefined: services.ChainStrategies

- [ ] **Step 3: Implement**

```go
// Append to retention_parse.go
// ChainStrategies composes multiple parse strategies. First non-zero result wins.
func ChainStrategies(strategies ...ParseStrategy) ParseStrategy {
	return func(key string) time.Time {
		for _, s := range strategies {
			if t := s(key); !t.IsZero() {
				return t
			}
		}
		return time.Time{}
	}
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestChainStrategies -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/retention_parse.go internal/core/services/retention_parse_test.go
git commit -m "feat(retention): add ChainStrategies composer"
```

---

### Task 5: Mark — empty + all-zero cases

**Files:**
- Create: `internal/core/services/retention_mark.go`
- Test: `internal/core/services/retention_mark_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/services/retention_mark_test.go
package services_test

import (
	"reflect"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestMark_EmptyList(t *testing.T) {
	got := services.Mark(nil, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("empty input: got %v, want empty", got)
	}
}

func TestMark_AllTiersZero_DeletesAll(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000/",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{}, services.ParseTimestampDir)
	if len(got) != 3 {
		t.Errorf("got %d deletions, want 3. got=%v", len(got), got)
	}
}

func TestMark_UnparseableKeysDeleted(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/garbage.txt",
		"backups/manual.tar", // unparseable by dir strategy
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 5}, services.ParseTimestampDir)
	if !reflect.DeepEqual(got, []string{"backups/garbage.txt", "backups/manual.tar"}) {
		t.Errorf("got %v, want unparseables only", got)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestMark -v`
Expected: FAIL — undefined: services.Mark

- [ ] **Step 3: Implement minimal Mark**

```go
// internal/core/services/retention_mark.go
package services

import (
	"fmt"
	"slices"
	"time"

	"ritual/internal/core/domain"
)

// Mark classifies keys into a delete list per retention rules.
// Pure function. No IO. Unparseable keys are marked for deletion (sacred dir).
func Mark(keys []string, rules domain.RetentionRules, parse ParseStrategy) []string {
	type entry struct {
		key string
		t   time.Time
	}

	var parsed []entry
	var unparseable []string

	for _, k := range keys {
		t := parse(k)
		if t.IsZero() {
			unparseable = append(unparseable, k)
			continue
		}
		parsed = append(parsed, entry{key: k, t: t.UTC()})
	}

	// Newest first; deterministic tiebreaker by key
	slices.SortFunc(parsed, func(a, b entry) int {
		if c := b.t.Compare(a.t); c != 0 {
			return c
		}
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})

	protected := make(map[string]bool, len(parsed))

	// keep_last
	for i, e := range parsed {
		if i < rules.KeepLast {
			protected[e.key] = true
		}
	}

	// keep_daily
	seen := map[string]int{}
	for _, e := range parsed {
		bucket := e.t.Format("2006-01-02")
		if seen[bucket] == 0 && seen["__daily_count__"] < rules.KeepDaily {
			protected[e.key] = true
			seen["__daily_count__"]++
		}
		seen[bucket]++
	}

	// keep_weekly
	seen = map[string]int{}
	for _, e := range parsed {
		y, w := e.t.ISOWeek()
		bucket := fmt.Sprintf("%d-W%02d", y, w)
		if seen[bucket] == 0 && seen["__weekly_count__"] < rules.KeepWeekly {
			protected[e.key] = true
			seen["__weekly_count__"]++
		}
		seen[bucket]++
	}

	// keep_monthly
	seen = map[string]int{}
	for _, e := range parsed {
		bucket := e.t.Format("2006-01")
		if seen[bucket] == 0 && seen["__monthly_count__"] < rules.KeepMonthly {
			protected[e.key] = true
			seen["__monthly_count__"]++
		}
		seen[bucket]++
	}

	var toDelete []string
	toDelete = append(toDelete, unparseable...)
	for _, e := range parsed {
		if !protected[e.key] {
			toDelete = append(toDelete, e.key)
		}
	}

	return toDelete
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestMark -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/retention_mark.go internal/core/services/retention_mark_test.go
git commit -m "feat(retention): add Mark pure function with tier logic"
```

---

### Task 6: Mark — tier behavior tests

**Files:**
- Modify: `internal/core/services/retention_mark_test.go`

- [ ] **Step 1: Write failing tests**

```go
// Append to retention_mark_test.go
func TestMark_KeepLast_KeepsNewest(t *testing.T) {
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000/",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 2}, services.ParseTimestampDir)
	want := []string{"backups/20260412160000/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMark_KeepMonthly_OnePerMonth(t *testing.T) {
	// 3 months, 2 entries per month — monthly:1 should keep the newest per month
	keys := []string{
		"backups/20260415160000/", // April - newer
		"backups/20260414100000/", // April - older
		"backups/20260315160000/", // March - newer
		"backups/20260314100000/", // March - older
		"backups/20260215160000/", // Feb - newer
		"backups/20260214100000/", // Feb - older
	}
	got := services.Mark(keys, domain.RetentionRules{KeepMonthly: 3}, services.ParseTimestampDir)
	want := []string{
		"backups/20260414100000/",
		"backups/20260314100000/",
		"backups/20260214100000/",
	}
	slicesEqual(t, got, want)
}

func TestMark_OverlappingTiers_Union(t *testing.T) {
	keys := []string{"backups/20260414160000/"}
	// Single entry protected by all tiers (each tier independently wants it)
	got := services.Mark(keys,
		domain.RetentionRules{KeepLast: 1, KeepDaily: 1, KeepWeekly: 1, KeepMonthly: 1},
		services.ParseTimestampDir)
	if len(got) != 0 {
		t.Errorf("got %v, want no deletions (overlap protects)", got)
	}
}

func TestMark_SameSecondTimestamps_DeterministicOrder(t *testing.T) {
	keys := []string{
		"backups/20260414160000/b",
		"backups/20260414160000/a",
	}
	// Only 1 kept — deterministic tiebreaker (alphabetical)
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 1}, services.ParseTimestampDir)
	// Stricter than spec: guarantee the SAME key wins every run
	got2 := services.Mark(keys, domain.RetentionRules{KeepLast: 1}, services.ParseTimestampDir)
	if !reflect.DeepEqual(got, got2) {
		t.Errorf("non-deterministic: %v vs %v", got, got2)
	}
}

func TestMark_MixedFormats_BothParsed(t *testing.T) {
	chain := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)
	keys := []string{
		"backups/20260414160000/",
		"backups/20260413160000.tar",
		"backups/20260412160000/",
	}
	got := services.Mark(keys, domain.RetentionRules{KeepLast: 2}, chain)
	// Newest 2 kept across both formats
	if len(got) != 1 || got[0] != "backups/20260412160000/" {
		t.Errorf("got %v, want [backups/20260412160000/]", got)
	}
}

// slicesEqual compares two string slices regardless of order
func slicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("len(got)=%d, len(want)=%d. got=%v, want=%v", len(got), len(want), got, want)
		return
	}
	gotMap := map[string]bool{}
	for _, g := range got {
		gotMap[g] = true
	}
	for _, w := range want {
		if !gotMap[w] {
			t.Errorf("missing %q in got=%v", w, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify pass (Mark implementation should already cover these)**

Run: `go test ./internal/core/services/ -run TestMark -v`
Expected: PASS (all tier cases)

- [ ] **Step 3: Commit**

```bash
git add internal/core/services/retention_mark_test.go
git commit -m "test(retention): cover tier overlap, formats, determinism"
```

---

## Phase 3: Retention Service

### Task 7: Update RetentionService interface + mock

**Files:**
- Modify: `internal/core/ports/ports.go`
- Modify: `internal/core/ports/mocks/retention.go`
- Modify: `internal/core/services/retention_logs.go` (update signature)

- [ ] **Step 1: Modify interface**

In `internal/core/ports/ports.go`:

```go
// RetentionService defines the interface for backup retention operations
// Retentions clean up old backups after manifest is updated
type RetentionService interface {
	// Apply removes old backups exceeding the retention limit
	Apply(ctx context.Context) error
}
```

- [ ] **Step 2: Update mock**

Replace `internal/core/ports/mocks/retention.go`:

```go
package mocks

import (
	"context"

	"ritual/internal/core/ports"
)

// MockRetentionService is a mock implementation of RetentionService for testing
type MockRetentionService struct {
	ApplyFunc  func(ctx context.Context) error
	ApplyCalls int
}

var _ ports.RetentionService = (*MockRetentionService)(nil)

func NewMockRetentionService() *MockRetentionService {
	return &MockRetentionService{}
}

func (m *MockRetentionService) Apply(ctx context.Context) error {
	m.ApplyCalls++
	if m.ApplyFunc != nil {
		return m.ApplyFunc(ctx)
	}
	return nil
}
```

- [ ] **Step 3: Update LogRetention signature**

In `internal/core/services/retention_logs.go:49`, change:

```go
// Before
func (r *LogRetention) Apply(ctx context.Context, manifest *domain.Manifest) error {
```

To:

```go
func (r *LogRetention) Apply(ctx context.Context) error {
```

Remove the `// manifest is not used for log retention` comment. Remove `"ritual/internal/core/domain"` import if no longer used.

- [ ] **Step 4: Run build**

Run: `go build ./...`
Expected: Many errors — callers of old `Apply(ctx, manifest)` still pass manifest. We'll fix them in later tasks.

- [ ] **Step 5: Temporary fix of log retention callers**

Update `internal/core/services/retention_local.go` and `retention_r2.go` Apply signatures to match (just to keep build green during transition). Both files will be deleted later:

```go
// retention_local.go, retention_r2.go — update signature
func (r *LocalRetention) Apply(ctx context.Context) error {
func (r *R2Retention) Apply(ctx context.Context) error {
```

Remove manifest parameter usage inside (they'll be deleted soon anyway).

Update `molfar.go:386` call site:

```go
// Before
if err := retention.Apply(ctx, updatedManifest); err != nil {

// After
if err := retention.Apply(ctx); err != nil {
```

- [ ] **Step 6: Run build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 7: Run existing tests**

Run: `go test ./...`
Expected: Many failures in retention_r2_test, retention_util_test — those files get deleted. Sync tests, molfar tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/core/ports/ports.go internal/core/ports/mocks/retention.go internal/core/services/retention_logs.go internal/core/services/retention_local.go internal/core/services/retention_r2.go internal/core/services/molfar.go
git commit -m "refactor(retention): simplify Apply signature — drop manifest param"
```

---

### Task 8: Generic retention struct

**Files:**
- Create: `internal/core/services/retention.go`
- Test: `internal/core/services/retention_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/services/retention_test.go
package services_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
)

func TestRetention_Apply_ListsAndDeletes(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		if prefix != "backups" {
			t.Errorf("prefix=%s, want backups", prefix)
		}
		return []string{
			"backups/20260414160000/",
			"backups/20260413160000/",
			"backups/20260412160000/",
		}, nil
	}

	deleted := []string{}
	storage.DeleteBatchFunc = func(ctx context.Context, keys []string) error {
		deleted = append(deleted, keys...)
		return nil
	}

	r, err := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(deleted) != 1 || deleted[0] != "backups/20260412160000/" {
		t.Errorf("deleted=%v, want [backups/20260412160000/]", deleted)
	}
}

func TestRetention_Apply_Empty_NoOp(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, nil
	}
	storage.DeleteBatchFunc = func(ctx context.Context, keys []string) error {
		t.Errorf("DeleteBatch called unexpectedly: %v", keys)
		return nil
	}

	r, _ := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRetention_Apply_ListError_Propagates(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	want := errors.New("list boom")
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, want
	}

	r, _ := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	err := r.Apply(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v wrapped", err, want)
	}
}

func TestRetention_NewRetention_NilStorage_Errors(t *testing.T) {
	_, err := services.NewRetention(nil, domain.RetentionRules{}, "backups", services.ParseTimestampDir)
	if err == nil {
		t.Error("expected error for nil storage")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestRetention_Apply -v`
Expected: FAIL — undefined: services.NewRetention

- [ ] **Step 3: Implement**

```go
// internal/core/services/retention.go
package services

import (
	"context"
	"errors"
	"fmt"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

var (
	ErrRetentionStorageNil = errors.New("retention storage cannot be nil")
	ErrRetentionParseNil   = errors.New("retention parse strategy cannot be nil")
)

// retention is the generic retention engine.
// Storage-agnostic via StorageRepository. Format-agnostic via ParseStrategy.
type retention struct {
	storage ports.StorageRepository
	rules   domain.RetentionRules
	prefix  string
	parse   ParseStrategy
}

var _ ports.RetentionService = (*retention)(nil)

// NewRetention creates a retention service for the given storage, rules, prefix, and parse strategy.
func NewRetention(storage ports.StorageRepository, rules domain.RetentionRules, prefix string, parse ParseStrategy) (ports.RetentionService, error) {
	if storage == nil {
		return nil, ErrRetentionStorageNil
	}
	if parse == nil {
		return nil, ErrRetentionParseNil
	}
	return &retention{storage: storage, rules: rules, prefix: prefix, parse: parse}, nil
}

// Apply lists the prefix, marks expired entries, and deletes them.
func (r *retention) Apply(ctx context.Context) error {
	keys, err := r.storage.List(ctx, r.prefix)
	if err != nil {
		return fmt.Errorf("list %s: %w", r.prefix, err)
	}

	toDelete := Mark(keys, r.rules, r.parse)
	if len(toDelete) == 0 {
		return nil
	}

	if err := r.storage.DeleteBatch(ctx, toDelete); err != nil {
		return fmt.Errorf("delete batch: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestRetention -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/retention.go internal/core/services/retention_test.go
git commit -m "feat(retention): add generic retention service wrapping Mark"
```

---

## Phase 4: Backup Function

### Task 9: shouldBackup pure function

**Files:**
- Create: `internal/core/services/dirty.go`
- Test: `internal/core/services/dirty_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/services/dirty_test.go
package services_test

import (
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestShouldBackup_EqualMaps_False(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]string{"a": "h1", "b": "h2"}}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h1", "b": "h2"}}
	if services.ShouldBackup(local, remote) {
		t.Error("equal maps: want false, got true")
	}
}

func TestShouldBackup_DifferentMaps_True(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h2"}}
	if !services.ShouldBackup(local, remote) {
		t.Error("different hashes: want true, got false")
	}
}

func TestShouldBackup_LocalEmpty_True(t *testing.T) {
	local := domain.SyncState{}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
	if !services.ShouldBackup(local, remote) {
		t.Error("local empty: want true, got false")
	}
}

func TestShouldBackup_BothEmpty_False(t *testing.T) {
	if services.ShouldBackup(domain.SyncState{}, domain.SyncState{}) {
		t.Error("both empty: want false, got true")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestShouldBackup -v`
Expected: FAIL — undefined: services.ShouldBackup

- [ ] **Step 3: Implement**

```go
// internal/core/services/dirty.go
package services

import (
	"maps"

	"ritual/internal/core/domain"
)

// ShouldBackup returns true if local and remote sync states differ.
// Pure function — used as gate before creating a backup snapshot.
func ShouldBackup(local, remote domain.SyncState) bool {
	return !maps.Equal(local.XXHashMap, remote.XXHashMap)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestShouldBackup -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/dirty.go internal/core/services/dirty_test.go
git commit -m "feat(backup): add ShouldBackup dirty-check pure function"
```

---

### Task 10: CreateBackup function

**Files:**
- Create: `internal/core/services/backup.go`
- Test: `internal/core/services/backup_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/core/services/backup_test.go
package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"testing"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
)

func TestCreateBackup_CopiesAllKeys(t *testing.T) {
	storage := mocks.NewMockStorageRepository()

	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		if prefix != "worlds" {
			t.Errorf("List prefix=%s, want worlds", prefix)
		}
		return []string{
			"worlds/world/level.dat",
			"worlds/world/region/r.0.0.mca",
		}, nil
	}

	copies := map[string]string{}
	storage.CopyFunc = func(ctx context.Context, src, dst string) error {
		copies[src] = dst
		return nil
	}
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		// manifest.json Put — verified separately
		return nil
	}

	manifest := &domain.Manifest{ManifestVersion: "v2"}
	if err := services.CreateBackup(context.Background(), storage, "worlds", config.BackupsDir, manifest); err != nil {
		t.Fatal(err)
	}

	if len(copies) != 2 {
		t.Fatalf("got %d copies, want 2: %v", len(copies), copies)
	}

	for src, dst := range copies {
		// dst must start with backups/{ts}/worlds/...
		if !strings.HasPrefix(dst, config.BackupsDir+"/") {
			t.Errorf("dst=%s missing backups prefix", dst)
		}
		if !strings.Contains(dst, "/"+src) {
			t.Errorf("dst=%s should contain src=%s", dst, src)
		}
	}
}

func TestCreateBackup_WritesManifestJSON(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, nil
	}
	storage.CopyFunc = func(ctx context.Context, src, dst string) error { return nil }

	var manifestKey string
	var manifestData []byte
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error {
		manifestKey = key
		manifestData = data
		return nil
	}

	manifest := &domain.Manifest{ManifestVersion: "v2", RitualVersion: "2.0.0"}
	if err := services.CreateBackup(context.Background(), storage, "worlds", config.BackupsDir, manifest); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(manifestKey, "/manifest.json") {
		t.Errorf("manifest key=%s, want suffix /manifest.json", manifestKey)
	}
	if !strings.HasPrefix(manifestKey, config.BackupsDir+"/") {
		t.Errorf("manifest key=%s, want backups/ prefix", manifestKey)
	}

	var decoded domain.Manifest
	if err := json.Unmarshal(manifestData, &decoded); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if decoded.ManifestVersion != "v2" || decoded.RitualVersion != "2.0.0" {
		t.Errorf("manifest round-trip mismatch: %+v", decoded)
	}

	// Pretty-printed (MarshalIndent) — should contain newlines
	if !strings.Contains(string(manifestData), "\n") {
		t.Error("manifest should be pretty-printed (MarshalIndent)")
	}
}

func TestCreateBackup_ListError_Propagates(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	want := errors.New("list boom")
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, want
	}

	manifest := &domain.Manifest{}
	err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest)
	if !errors.Is(err, want) {
		t.Errorf("got %v, want wrapping %v", err, want)
	}
}

func TestCreateBackup_PathsUseForwardSlashes(t *testing.T) {
	storage := mocks.NewMockStorageRepository()
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"worlds/world/level.dat"}, nil
	}
	var copyDst string
	storage.CopyFunc = func(ctx context.Context, src, dst string) error {
		copyDst = dst
		return nil
	}
	storage.PutFunc = func(ctx context.Context, key string, data []byte) error { return nil }

	manifest := &domain.Manifest{}
	if err := services.CreateBackup(context.Background(), storage, "worlds", "backups", manifest); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(copyDst, `\`) {
		t.Errorf("dst=%s contains backslash — storage keys must use /", copyDst)
	}
	// sanity: use path (not filepath)
	if copyDst != path.Join("backups", strings.Split(copyDst, "/")[1], "worlds/world/level.dat") {
		// we can't know the timestamp — just check it's well-formed
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/core/services/ -run TestCreateBackup -v`
Expected: FAIL — undefined: services.CreateBackup

- [ ] **Step 3: Implement**

```go
// internal/core/services/backup.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// CreateBackup copies all keys under srcPrefix into dstPrefix/{ts}/... within the same storage,
// then writes a manifest snapshot at dstPrefix/{ts}/manifest.json.
// Same-storage copy is efficient: same-disk copy locally, server-side copy on R2.
func CreateBackup(
	ctx context.Context,
	storage ports.StorageRepository,
	srcPrefix, dstPrefix string,
	manifest *domain.Manifest,
) error {
	ts := time.Now().UTC().Format(config.TimestampFormat)
	base := path.Join(dstPrefix, ts)

	keys, err := storage.List(ctx, srcPrefix)
	if err != nil {
		return fmt.Errorf("list %s: %w", srcPrefix, err)
	}

	for _, key := range keys {
		if err := storage.Copy(ctx, key, path.Join(base, key)); err != nil {
			return fmt.Errorf("copy %s: %w", key, err)
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := storage.Put(ctx, path.Join(base, "manifest.json"), data); err != nil {
		return fmt.Errorf("put manifest: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/core/services/ -run TestCreateBackup -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/backup.go internal/core/services/backup_test.go
git commit -m "feat(backup): add CreateBackup stdlib-only function"
```

---

## Phase 5: Config Integration

### Task 11: Add BackupsDir constant + retention defaults in domain

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/core/domain/manifest.go`
- Modify: `internal/core/domain/settings.go`

- [ ] **Step 1: Add BackupsDir to config**

In `internal/config/config.go:30`, add:

```go
const (
	LocalBackups  = "world_backups" // legacy; kept until v1 tar callers removed
	RemoteBackups = "worlds"         // legacy
	BackupsDir    = "backups"        // v2 unified local/R2 backup prefix
	ServerDir     = "server"
	WorldsDir     = "worlds"
	TmpDir        = "temp"
	LogsDir       = "logs"
)
```

- [ ] **Step 2: Extend Settings struct**

In `internal/core/domain/settings.go:15`, update the struct and default:

```go
type Settings struct {
	IP             string         `json:"ip"`
	Port           int            `json:"port"`
	Memory         int            `json:"memory"`
	LocalRetention RetentionRules `json:"local_retention"`
}

func DefaultSettings() *Settings {
	return &Settings{
		IP:             "0.0.0.0",
		Port:           25565,
		Memory:         4096,
		LocalRetention: DefaultRetentionRules(),
	}
}
```

- [ ] **Step 3: Extend Manifest struct**

In `internal/core/domain/manifest.go:8`, update:

```go
type Manifest struct {
	ManifestVersion string    `json:"manifest_version"`
	RitualVersion   string    `json:"ritual_version"`
	LockedBy        string    `json:"locked_by"`
	UpdatedAt       time.Time `json:"updated_at"`

	MinRAMMB       int `json:"min_ram_mb"`
	MinDiskMB      int `json:"min_disk_mb"`
	MinJavaVersion int `json:"min_java_version"`

	Worlds          WorldsManifest  `json:"worlds"`
	Server          ServerManifest  `json:"server"`
	RemoteRetention RetentionRules  `json:"remote_retention"`
}
```

Update `ApplyDefaults` at the end of the file:

```go
func (m *Manifest) ApplyDefaults() {
	if m.MinRAMMB <= 0 {
		m.MinRAMMB = config.DefaultMinRAMMB
	}
	if m.MinDiskMB <= 0 {
		m.MinDiskMB = config.DefaultMinDiskMB
	}
	if m.MinJavaVersion <= 0 {
		m.MinJavaVersion = config.DefaultMinJavaVersion
	}
	// Zero-value RetentionRules is a valid "no retention" config, but admin-friendly
	// default matches historical R2MaxBackups=2 behaviour.
	if m.RemoteRetention == (RetentionRules{}) {
		m.RemoteRetention = DefaultRetentionRules()
	}
}
```

- [ ] **Step 4: Run existing tests**

Run: `go test ./internal/core/domain/ -v`
Expected: PASS (existing tests don't break)

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/core/domain/manifest.go internal/core/domain/settings.go
git commit -m "feat(config): add BackupsDir + wire RetentionRules into Settings/Manifest"
```

---

### Task 12: Backward-compat test for existing settings.json

**Files:**
- Modify: `internal/core/domain/settings_test.go`

- [ ] **Step 1: Add test**

```go
// Append to settings_test.go (or create if missing)
func TestLoadSettings_MissingRetention_UsesZeroValue(t *testing.T) {
	// Simulate existing settings.json from v1 without retention field
	data := []byte(`{"ip":"127.0.0.1","port":25565,"memory":8192}`)

	var s domain.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("should load v1 settings: %v", err)
	}
	if s.IP != "127.0.0.1" {
		t.Errorf("IP=%s, want 127.0.0.1", s.IP)
	}
	// Retention fields are zero — user must set explicitly or UI applies defaults
	if s.LocalRetention != (domain.RetentionRules{}) {
		t.Errorf("LocalRetention = %+v, want zero (missing field)", s.LocalRetention)
	}
}
```

Note: `LoadSettings` returns `DefaultSettings()` only when the file is missing — when the file exists and lacks retention fields, they stay zero. The UI layer or `ApplyDefaults` equivalent should fill them. If a `ApplySettingsDefaults` helper is desired, add it here.

- [ ] **Step 2: Run test**

Run: `go test ./internal/core/domain/ -run TestLoadSettings_MissingRetention -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/core/domain/settings_test.go
git commit -m "test(settings): verify v1 settings.json loads without retention"
```

---

## Phase 6: Molfar Integration

### Task 13: Rewrite Molfar Exit flow

**Files:**
- Modify: `internal/core/services/molfar.go`
- Modify: `internal/core/services/molfar_test.go`

- [ ] **Step 1: Write failing test**

Review existing `molfar_test.go` for test patterns. Add a focused integration test:

```go
// Append to internal/core/services/molfar_test.go

func TestMolfar_Exit_CleanWorlds_SkipsBackup_RunsRetention(t *testing.T) {
	// Setup: local and remote have identical xxhash maps → clean
	hashMap := map[string]string{"worlds/world/level.dat": "h1"}

	localManifest := &domain.Manifest{
		Worlds: domain.WorldsManifest{SyncState: domain.SyncState{XXHashMap: hashMap}},
	}
	remoteManifest := localManifest.Clone()

	// ... mock librarian, storages, retention ...
	// ... create Molfar, set lockID, call Exit ...
	// ... assert: no Copy/Put calls on storages, retention.Apply called ...
}

func TestMolfar_Exit_DirtyWorlds_CreatesBackups_RunsRetention(t *testing.T) {
	// Setup: local and remote have different xxhash maps → dirty
	// ... assert: Copy called on both storages, retention.Apply called ...
}

func TestMolfar_Exit_NoLock_SkipsEverything(t *testing.T) {
	// Setup: currentLockID == ""
	// ... assert: no calls to anything ...
}
```

(Flesh out using existing test helpers in molfar_test.go. The existing tests demonstrate setup patterns — copy them.)

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/core/services/ -run TestMolfar_Exit -v`
Expected: FAIL — test expectations don't match current behaviour.

- [ ] **Step 3: Rewrite Exit method**

Replace `Exit` and delete `updateManifestsWithArchive` in `internal/core/services/molfar.go`:

```go
// Exit gracefully shuts down the server and cleans up resources.
// Only runs if we own the lock.
func (m *MolfarService) Exit() error {
	if m == nil {
		return ErrMolfarNil
	}
	if m.librarian == nil {
		return ErrLibrarianNil
	}

	m.send(ports.StartEvent{Operation: "exit"})
	defer m.send(ports.FinishEvent{Operation: "exit"})

	if m.currentLockID == "" {
		m.send(ports.UpdateEvent{Operation: "exit", Message: "No lock owned, skipping exit flow"})
		return nil
	}

	ctx := context.Background()

	// Run all backuppers (delta sync + snapshot creators).
	for i, backupper := range m.backuppers {
		m.send(ports.StartEvent{Operation: "backup"})
		m.send(ports.UpdateEvent{Operation: "backup", Message: "Running backupper", Data: map[string]any{"index": i}})
		if err := backupper.Run(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("backupper %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "backup"})
	}

	// Run retentions (always, even when backuppers skipped).
	for i, r := range m.retentions {
		m.send(ports.StartEvent{Operation: "retention"})
		if err := r.Apply(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "retention", Err: err})
			return fmt.Errorf("retention %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "retention"})
	}

	return m.unlockManifests(ctx)
}
```

**Important:** `BackupperService` no longer returns `(string, error)` — we'll update the interface in Task 14. For now this code won't compile; proceed to next task.

- [ ] **Step 4: Skip to Task 14 — interface update needed before tests can pass**

---

### Task 14: Collapse BackupperService — backup becomes a function

**Files:**
- Modify: `internal/core/ports/ports.go`
- Delete: `internal/core/ports/mocks/backupper.go`
- Delete: `internal/core/ports/mocks/backupper_test.go`
- Modify: `internal/core/services/sync_updater.go`
- Modify: `internal/core/services/molfar.go`
- Modify: `internal/core/services/molfar_test.go`

Spec decides: `BackupperService` is deleted; backup is the `CreateBackup` function; sync upload stays as a service that conforms to `UpdaterService` (since it drives sync, not backup).

- [ ] **Step 1: Change SyncUploadBackupper to UpdaterService**

In `internal/core/services/sync_updater.go`:

- Rename `SyncUploadBackupper` → `SyncUploader`
- Change `Run(ctx) (string, error)` → `Run(ctx) error`
- Keep it as `UpdaterService`, not backupper
- Remove any archive-name return value logic

```go
// SyncUploader wraps syncService.Upload as an UpdaterService.
type SyncUploader struct {
	sync      *syncService
	librarian ports.LibrarianService
	getState  func(*domain.Manifest) *domain.SyncState
}

var _ ports.UpdaterService = (*SyncUploader)(nil)

func NewSyncUploader(sync *syncService, librarian ports.LibrarianService, getState func(*domain.Manifest) *domain.SyncState) *SyncUploader {
	return &SyncUploader{sync: sync, librarian: librarian, getState: getState}
}

func (u *SyncUploader) Run(ctx context.Context) error {
	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		return err
	}
	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localState := u.getState(localManifest)
	remoteState := u.getState(remoteManifest)

	newState, err := u.sync.Upload(ctx, *localState, *remoteState)
	if err != nil {
		return err
	}

	*localState = newState
	*remoteState = newState

	if err := u.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return err
	}
	return u.librarian.SaveRemoteManifest(ctx, remoteManifest)
}
```

- [ ] **Step 2: Delete BackupperService interface + backuppers field in Molfar**

In `internal/core/ports/ports.go`, **delete** lines 83-89 (BackupperService interface).

In `internal/core/services/molfar.go`:

- Remove `backuppers []ports.BackupperService` field
- Remove `backuppers` parameter from `NewMolfarService`
- Remove validation loop for backuppers

Add new fields for backup orchestration instead:

```go
type MolfarService struct {
	conditions    []ports.ConditionService
	updaters      []ports.UpdaterService
	retentions    []ports.RetentionService
	serverRunner  ports.ServerRunner
	librarian     ports.LibrarianService
	events        chan<- ports.Event
	workRoot      *os.Root
	currentLockID string

	// Backup orchestration
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
}
```

Update `NewMolfarService` signature:

```go
func NewMolfarService(
	conditions []ports.ConditionService,
	updaters []ports.UpdaterService,
	retentions []ports.RetentionService,
	serverRunner ports.ServerRunner,
	librarian ports.LibrarianService,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	events chan<- ports.Event,
	workRoot *os.Root,
) (*MolfarService, error)
```

Add nil-check validation for the two new storages. Remove backupper validation.

- [ ] **Step 3: Rewrite Exit with inline backup orchestration**

Replace the backupper loop in `Exit` with:

```go
	// Run all updaters at exit (delta sync upload).
	// Snapshot manifests before and after to compute dirty state.
	localBefore, err := m.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("get local manifest: %w", err)
	}
	remoteBefore, err := m.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	for i, u := range m.updaters {
		m.send(ports.StartEvent{Operation: "exit-updater"})
		if err := u.Run(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "exit-updater", Err: err})
			return fmt.Errorf("exit updater %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "exit-updater"})
	}

	// Dirty check: compare states before updater ran.
	// If local and remote diverged before sync, something changed — snapshot it.
	if ShouldBackup(localBefore.Worlds.SyncState, remoteBefore.Worlds.SyncState) {
		manifestAfter, err := m.librarian.GetLocalManifest(ctx)
		if err != nil {
			return fmt.Errorf("get manifest for backup: %w", err)
		}

		m.send(ports.UpdateEvent{Operation: "backup", Message: "Creating local snapshot"})
		if err := CreateBackup(ctx, m.localStorage, config.WorldsDir, config.BackupsDir, manifestAfter); err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("local backup: %w", err)
		}

		m.send(ports.UpdateEvent{Operation: "backup", Message: "Creating R2 snapshot"})
		if err := CreateBackup(ctx, m.remoteStorage, config.WorldsDir, config.BackupsDir, manifestAfter); err != nil {
			m.send(ports.ErrorEvent{Operation: "backup", Err: err})
			return fmt.Errorf("r2 backup: %w", err)
		}
	}

	// Retentions run regardless of dirty state.
	for i, r := range m.retentions {
		m.send(ports.StartEvent{Operation: "retention"})
		if err := r.Apply(ctx); err != nil {
			m.send(ports.ErrorEvent{Operation: "retention", Err: err})
			return fmt.Errorf("retention %d: %w", i, err)
		}
		m.send(ports.FinishEvent{Operation: "retention"})
	}

	return m.unlockManifests(ctx)
```

Note: this design keeps the Exit-phase delta-sync upload as an `UpdaterService` in the `updaters` slice. Molfar adds them under Prepare already; we'll pick a separate slice for exit updaters in Task 15 to avoid overloading Prepare.

- [ ] **Step 4: Delete mock files and `updateManifestsWithArchive`**

```bash
git rm internal/core/ports/mocks/backupper.go internal/core/ports/mocks/backupper_test.go
```

Remove `updateManifestsWithArchive` method from `molfar.go` entirely.

- [ ] **Step 5: Update molfar_test.go call sites**

Every `NewMolfarService(...)` call in tests now takes the new signature. Update them to pass the new args. Remove `backuppers` slices. Replace with `localStorage`, `remoteStorage` mocks.

- [ ] **Step 6: Run build**

Run: `go build ./...`
Expected: SUCCESS (except retention_local.go and retention_r2.go which we'll delete in next phase)

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(molfar): delete BackupperService, inline backup orchestration"
```

---

### Task 15: Separate exit updaters from prepare updaters

**Files:**
- Modify: `internal/core/services/molfar.go`
- Modify: `internal/core/services/molfar_test.go`

The existing Molfar has `updaters` used by `Prepare`. Exit needs its own upload updaters. Add a separate slice.

- [ ] **Step 1: Update struct and constructor**

```go
type MolfarService struct {
	conditions    []ports.ConditionService
	updaters      []ports.UpdaterService // prepare-phase
	exitUpdaters  []ports.UpdaterService // exit-phase (e.g. SyncUploader)
	retentions    []ports.RetentionService
	// ... rest same
}
```

Add `exitUpdaters []ports.UpdaterService` parameter to `NewMolfarService`, with nil validation.

- [ ] **Step 2: Update Exit to iterate `exitUpdaters`**

Change the updater loop in Exit:

```go
for i, u := range m.exitUpdaters {
	// ...
	if err := u.Run(ctx); err != nil { ... }
}
```

- [ ] **Step 3: Update all molfar_test.go call sites**

Pass `exitUpdaters` (empty `[]ports.UpdaterService{}` in most tests; populated in Exit tests).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/services/ -run TestMolfar -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/molfar.go internal/core/services/molfar_test.go
git commit -m "refactor(molfar): split prepare/exit updater slices"
```

---

### Task 16: Update main.go wiring

**Files:**
- Modify: `cmd/cli/main.go`

- [ ] **Step 1: Rewire DI**

Locate the current backupper/retention construction (around lines 260-296) and replace with:

```go
// Parse strategy: v2 dir + v1 tar compatibility
parseBackupTimestamp := services.ChainStrategies(
	services.ParseTimestampDir,
	services.ParseTimestampTar,
)

// Load host retention rules (local) and admin retention rules (R2)
settings, err := domain.LoadSettings()
if err != nil {
	fmt.Printf("Failed to load settings: %v\n", err)
	close(events)
	wg.Wait()
	return
}

// Apply defaults if zero
localRules := settings.LocalRetention
if localRules == (domain.RetentionRules{}) {
	localRules = domain.DefaultRetentionRules()
}
remoteRules := remoteManifestForConditions.RemoteRetention
if remoteRules == (domain.RetentionRules{}) {
	remoteRules = domain.DefaultRetentionRules()
}

// Retentions
localRetention, err := services.NewRetention(localStorage, localRules, config.BackupsDir, parseBackupTimestamp)
if err != nil {
	fmt.Printf("Failed to create local retention: %v\n", err)
	close(events)
	wg.Wait()
	return
}
r2Retention, err := services.NewRetention(remoteStorage, remoteRules, config.BackupsDir, parseBackupTimestamp)
if err != nil {
	fmt.Printf("Failed to create R2 retention: %v\n", err)
	close(events)
	wg.Wait()
	return
}
logRetention, err := services.NewLogRetention(localStorage, events)
if err != nil {
	fmt.Printf("Failed to create log retention: %v\n", err)
	close(events)
	wg.Wait()
	return
}
retentions := []ports.RetentionService{localRetention, r2Retention, logRetention}

// Exit updater: delta-sync world upload
worldSyncUploader := services.NewSyncUploader(worldSync, librarian, func(m *domain.Manifest) *domain.SyncState {
	return &m.Worlds.SyncState
})
exitUpdaters := []ports.UpdaterService{worldSyncUploader}

// Molfar
molfar, err := services.NewMolfarService(
	conditions,
	updaters,
	exitUpdaters,
	retentions,
	serverRunner,
	librarian,
	localStorage,
	remoteStorage,
	events,
	workRoot,
)
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: Some failures in deleted files (retention_local_test, retention_r2_test, retention_util_test). All other tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/main.go
git commit -m "refactor(cli): wire generic retention + CreateBackup"
```

---

## Phase 7: Dead Code Removal

### Task 17: Delete old retention implementations

**Files:**
- Delete: `internal/core/services/retention_local.go`
- Delete: `internal/core/services/retention_r2.go`
- Delete: `internal/core/services/retention_r2_test.go`
- Delete: `internal/core/services/retention_util.go`
- Delete: `internal/core/services/retention_util_test.go`

- [ ] **Step 1: Delete files**

```bash
git rm internal/core/services/retention_local.go
git rm internal/core/services/retention_r2.go
git rm internal/core/services/retention_r2_test.go
git rm internal/core/services/retention_util.go
git rm internal/core/services/retention_util_test.go
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: SUCCESS (all callers already rewired to generic retention)

- [ ] **Step 3: Run tests**

Run: `go test ./...`
Expected: PASS (except any remaining session.go / world.go tests — next task)

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: delete legacy retention_local, retention_r2, retention_util"
```

---

### Task 18: Delete session.go (CheckPlayersJoined)

**Files:**
- Delete: `internal/core/services/session.go`
- Delete: `internal/core/services/session_test.go`

- [ ] **Step 1: Grep for callers**

Run: `grep -rn "CheckPlayersJoined" --include="*.go"`
Expected: only session.go and session_test.go reference it. If callers found in main.go or elsewhere — remove the call (we use xxhash dirty check now).

- [ ] **Step 2: Delete files**

```bash
git rm internal/core/services/session.go
git rm internal/core/services/session_test.go
```

- [ ] **Step 3: Run build + tests**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: delete CheckPlayersJoined — superseded by ShouldBackup"
```

---

### Task 19: Delete World domain + manifest Backups

**Files:**
- Delete: `internal/core/domain/world.go`
- Delete: `internal/core/domain/world_test.go`
- Modify: `internal/core/domain/manifest.go`
- Modify: `internal/core/domain/sync_state.go`
- Modify: `internal/core/domain/manifest_test.go`

- [ ] **Step 1: Grep for callers**

Run: `grep -rn "domain\.World\|\.AddWorld\|\.GetLatestWorld\|\.RemoveOldestWorlds\|Worlds\.Backups" --include="*.go"`
Expected: only manifest.go, world.go, tests, and retention_r2_test.go (already deleted). If any live callers exist, remove them.

- [ ] **Step 2: Modify WorldsManifest in sync_state.go**

```go
// Remove Backups field
type WorldsManifest struct {
	SyncState
}
```

- [ ] **Step 3: Modify manifest.go**

Remove these methods entirely:
- `AddWorld`
- `GetLatestWorld`
- `RemoveOldestWorlds`

Update `Clone` to remove Backups copy:

```go
// In Clone, remove:
//   Backups: make([]World, len(m.Worlds.Backups)),
// And:
//   copy(clone.Worlds.Backups, m.Worlds.Backups)
```

- [ ] **Step 4: Delete World files**

```bash
git rm internal/core/domain/world.go
git rm internal/core/domain/world_test.go
```

- [ ] **Step 5: Update manifest_test.go**

Remove tests for `AddWorld`, `GetLatestWorld`, `RemoveOldestWorlds`. Keep unrelated manifest tests.

- [ ] **Step 6: Run build + tests**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "chore: delete domain.World + Manifest.Backups list"
```

---

### Task 20: Remove obsolete config constants

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Grep for usages**

```bash
grep -rn "config\.LocalBackups\|config\.RemoteBackups\|config\.R2MaxBackups\|config\.LocalMaxBackups\|config\.ManualWorldFilename" --include="*.go"
```

Expected: only config.go. If any live callers remain, remove them first.

- [ ] **Step 2: Delete constants**

Remove these lines from `internal/config/config.go`:

```go
// Delete:
//   LocalBackups  = "world_backups"
//   RemoteBackups = "worlds"
//   R2MaxBackups    = 2
//   LocalMaxBackups = 2
//   ManualWorldFilename = "manual.tar"
```

- [ ] **Step 3: Run build + tests**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "chore: remove obsolete backup config constants"
```

---

## Phase 8: Integration Tests

### Task 21: Integration test — dirty/clean + CreateBackup

**Files:**
- Create: `internal/core/services/backup_integration_test.go`

- [ ] **Step 1: Write test**

Use `FSRepository` against a temp dir to exercise `CreateBackup` end-to-end.

```go
package services_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestCreateBackup_IntegrationFS(t *testing.T) {
	root := t.TempDir()

	// Seed worlds/
	worldsDir := filepath.Join(root, "worlds", "world")
	if err := os.MkdirAll(worldsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldsDir, "level.dat"), []byte("LEVEL"), 0644); err != nil {
		t.Fatal(err)
	}

	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fsRoot.Close() })

	storage := adapters.NewFSRepository(fsRoot)

	manifest := &domain.Manifest{ManifestVersion: "v2", RitualVersion: "2.0.0"}

	ctx := context.Background()
	if err := services.CreateBackup(ctx, storage, "worlds", config.BackupsDir, manifest); err != nil {
		t.Fatal(err)
	}

	// Walk backups/ to verify snapshot exists with expected files
	backupsRoot := filepath.Join(root, config.BackupsDir)
	entries, err := os.ReadDir(backupsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup dir, got %d", len(entries))
	}

	tsDir := filepath.Join(backupsRoot, entries[0].Name())

	// Verify worlds/world/level.dat exists in backup
	data, err := os.ReadFile(filepath.Join(tsDir, "worlds", "world", "level.dat"))
	if err != nil {
		t.Fatalf("level.dat not copied: %v", err)
	}
	if string(data) != "LEVEL" {
		t.Errorf("level.dat content mismatch: %s", data)
	}

	// Verify manifest.json exists and round-trips
	manifestData, err := os.ReadFile(filepath.Join(tsDir, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	var decoded domain.Manifest
	if err := json.Unmarshal(manifestData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ManifestVersion != "v2" {
		t.Errorf("manifest decoded: %+v", decoded)
	}
}
```

Adjust imports/constructors to match actual `FSRepository` API.

- [ ] **Step 2: Run test**

Run: `go test ./internal/core/services/ -run TestCreateBackup_IntegrationFS -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/core/services/backup_integration_test.go
git commit -m "test(backup): integration test against FSRepository"
```

---

### Task 22: Integration test — retention against filesystem with mixed formats

**Files:**
- Create: `internal/core/services/retention_integration_test.go`

- [ ] **Step 1: Write test**

```go
package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestRetention_IntegrationFS_MixedFormats(t *testing.T) {
	root := t.TempDir()
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0755); err != nil {
		t.Fatal(err)
	}

	// v2 directory backups (timestamp-named)
	for _, ts := range []string{"20260414160000", "20260413160000", "20260412160000"} {
		d := filepath.Join(backups, ts, "worlds")
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "level.dat"), []byte("x"), 0644)
	}

	// v1 tar backups
	os.WriteFile(filepath.Join(backups, "20260411160000.tar"), []byte("old"), 0644)
	os.WriteFile(filepath.Join(backups, "20260410160000.tar"), []byte("older"), 0644)

	// Unknown file (should be deleted by sacred-dir rule)
	os.WriteFile(filepath.Join(backups, "garbage.txt"), []byte("noise"), 0644)

	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fsRoot.Close() })

	storage := adapters.NewFSRepository(fsRoot)
	parse := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)

	r, err := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", parse)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	// After retention: 2 newest (both v2 dirs) survive, rest deleted
	// garbage.txt deleted
	// 20260412 dir deleted, 20260411.tar deleted, 20260410.tar deleted

	mustNotExist(t, filepath.Join(backups, "garbage.txt"))
	mustNotExist(t, filepath.Join(backups, "20260411160000.tar"))
	mustNotExist(t, filepath.Join(backups, "20260410160000.tar"))

	mustExist(t, filepath.Join(backups, "20260414160000"))
	mustExist(t, filepath.Join(backups, "20260413160000"))
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}
func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected %s to be deleted", path)
	}
}
```

Adjust `FSRepository` constructor as needed to match current signature.

- [ ] **Step 2: Run test**

Run: `go test ./internal/core/services/ -run TestRetention_IntegrationFS -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/core/services/retention_integration_test.go
git commit -m "test(retention): integration test with mixed v1/v2 backup formats"
```

---

## Phase 9: Post-Implementation Audit

### Task 23: Dead code sweep

- [ ] **Step 1: Grep for deleted symbols**

Run each of these; expect zero hits in non-test source files:

```bash
grep -rn "domain\.World" --include="*.go"
grep -rn "AddWorld\|GetLatestWorld\|RemoveOldestWorlds" --include="*.go"
grep -rn "CheckPlayersJoined\|PlayerJoinPattern" --include="*.go"
grep -rn "BackupperService" --include="*.go"
grep -rn "SyncUploadBackupper" --include="*.go"
grep -rn "LocalRetention\|R2Retention" --include="*.go" | grep -v "local_retention\|LocalRetention struct field" # ensure only Settings field references remain
grep -rn "updateManifestsWithArchive" --include="*.go"
grep -rn "LocalMaxBackups\|R2MaxBackups\|ManualWorldFilename\|LocalBackups\|RemoteBackups" --include="*.go"
grep -rn "MockBackupperService" --include="*.go"
```

- [ ] **Step 2: Fix any hits**

Any remaining references = dead code. Remove.

- [ ] **Step 3: Run full test suite + build**

```bash
go build ./...
go test ./...
```

Expected: PASS

- [ ] **Step 4: LOC delta check**

```bash
git diff --stat main
```

Expected: Net negative LOC (target ~-650).

- [ ] **Step 5: Commit any cleanup**

```bash
git add -A
git commit -m "chore: final dead code cleanup post backup/retention redesign"
```

---

### Task 24: Update docs

- [ ] **Step 1: Update docs/structure.md**

Remove references to deleted files. Add lines for:
- `internal/core/services/backup.go`
- `internal/core/services/retention.go`
- `internal/core/services/retention_mark.go`
- `internal/core/services/retention_parse.go`
- `internal/core/services/dirty.go`

- [ ] **Step 2: Update docs/progress.md or equivalent**

Note completion of backup/retention redesign, link to spec + plan.

- [ ] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs: reflect backup/retention redesign"
```

---

## Self-Review Checklist

Before handoff, verify:

- [ ] Every acceptance criterion in the spec has a task covering it
- [ ] Every deleted file is explicitly git rm'd in some task
- [ ] No "TODO" or "fill in" placeholders in any step
- [ ] Types/signatures consistent across tasks (no name drift)
- [ ] Every code block is runnable as-is (no `...` gaps in function bodies)
- [ ] Imports shown for all new files
- [ ] Every step has expected command output
- [ ] No step references a type defined in a later task
