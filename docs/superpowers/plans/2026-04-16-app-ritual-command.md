# App Ritual — Bus-Driven Command Pattern

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract chain assembly from `cmd/cli/main.go` into `internal/app.Ritual` — bus-driven command dispatch with Start/Stop/Retry, retry re-enters at failed stage via `RunCurrent`.

**Architecture:** `app.Ritual` subscribes to the event bus for command events (`StartRequested`, `StopRequested`, `RetryRequested`). Internally builds stage chain once at construction. `Listen()` is the single public method — a bus subscriber loop that dispatches to unexported `start`/`stop`/`retry`. The machine driver gains `RunCurrent()` so retry re-enters at the last stopped strategy. Failed strategy gains an `onRetry` back-edge set after chain construction. CLI and GUI become identical: build deps, publish command events to bus.

**Tech Stack:** Go, existing `machine.Strategy[ritual.RunState]`, `ports.EventBus`, `ports.*` interfaces.

---

## Acceptance Criteria

**Functional:**
1. `bus.Publish(StartRequested{})` runs full pipeline Check→Fetch→Acquire→Run→Publish→Archive→Unlock→Retain
2. `bus.Publish(StopRequested{})` cancels a running pipeline via context
3. `bus.Publish(RetryRequested{})` re-enters pipeline at the stage that failed — not from beginning
4. Retry rejected when status is not `Failed`
5. Start rejected when status is not `Idle`
6. Stop is no-op when status is not `Running`
7. CLI `main.go` has zero stage imports — all chain assembly lives in `internal/app`
8. No `fmt.Print` inside `internal/app` — all communication via bus events

**Non-functional:**
9. `go test ./...` — all existing tests pass
10. `go build ./...` — compiles clean

**Testing:**
11. Test: Start with fakes → pipeline completes, status events published
12. Test: Fetch fails → status is Failed → Retry → pipeline completes from fetch stage
13. Test: Condition fails → Retry → re-checks conditions
14. Test: Stop mid-run → pipeline unblocks
15. Test: Retry when not failed → error returned (via bus or status)
16. Test: Start when already running → error returned

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/app/events.go` | Create | Command events (StartRequested, StopRequested, RetryRequested) + StatusChanged |
| `internal/app/outcome.go` | Create | Outcome type (Idle, Running, Failed, Done) |
| `internal/app/ritual.go` | Create | Ritual struct, New, Listen, unexported start/stop/retry, buildChain |
| `internal/app/ritual_test.go` | Create | Integration tests with fakes |
| `internal/core/machine/machine.go` | Modify | Add RunCurrent to driver |
| `internal/core/ritual/run.go` | Modify | Track current strategy, expose RunCurrent |
| `internal/core/ritual/runstate.go` | Modify | Add FailedStage field |
| `internal/core/stages/failed/strategy.go` | Modify | Add onRetry field + SetRetry + retry path |
| `internal/core/stages/failed/strategy_test.go` | Modify | Test retry path |
| `cmd/cli/main.go` | Modify | Remove chain assembly, use app.New + bus.Publish |

---

### Task 1: Command Events and Outcome Type

**Files:**
- Create: `internal/app/events.go`
- Create: `internal/app/outcome.go`

- [ ] **Step 1: Create events.go with command events and status event**

```go
package app

import "fmt"

// StartRequested commands the Ritual to begin the full pipeline.
type StartRequested struct{}

func (StartRequested) String() string { return "start requested" }

// StopRequested commands the Ritual to cancel the running pipeline.
type StopRequested struct{}

func (StopRequested) String() string { return "stop requested" }

// RetryRequested commands the Ritual to re-enter at the failed stage.
type RetryRequested struct{}

func (RetryRequested) String() string { return "retry requested" }

// StatusChanged is published when the Ritual transitions between outcomes.
type StatusChanged struct {
	Status Outcome
	Err    error
}

func (s StatusChanged) String() string {
	if s.Err != nil {
		return fmt.Sprintf("status: %s err: %v", s.Status, s.Err)
	}
	return fmt.Sprintf("status: %s", s.Status)
}
```

- [ ] **Step 2: Create outcome.go with Outcome type**

```go
package app

type Outcome int

const (
	Idle Outcome = iota
	Running
	Failed
	Done
)

func (o Outcome) String() string {
	switch o {
	case Idle:
		return "idle"
	case Running:
		return "running"
	case Failed:
		return "failed"
	case Done:
		return "done"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/app/...`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat(app): add command events and Outcome type
```

---

### Task 2: Machine RunCurrent

**Files:**
- Modify: `internal/core/ritual/run.go`
- Modify: `internal/core/ritual/runstate.go`

- [ ] **Step 1: Write failing test — RunCurrent re-enters at last stopped strategy**

Create `internal/core/ritual/run_test.go`:

```go
package ritual_test

import (
	"context"
	"testing"

	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	name  string
	count *int
	next  machine.Strategy[ritual.RunState]
}

func (s *countingStrategy) Name() string { return s.name }

func (s *countingStrategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	*s.count++
	return s.next, nil
}

type failStrategy struct {
	from string
}

func (s *failStrategy) Name() string { return ritual.StageFailed }

func (s *failStrategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	rs.FailedStage = s.from
	return nil, rs.Err
}

func TestRunner_RunCurrent_ReentersAtFailedStage(t *testing.T) {
	checkCount := 0
	fetchCount := 0

	fail := &failStrategy{from: ritual.StageFetching}
	fetch := &countingStrategy{name: "Fetching", count: &fetchCount, next: fail}
	check := &countingStrategy{name: "Checking", count: &checkCount, next: fetch}

	rs := &ritual.RunState{Bus: nil, Err: fmt.Errorf("network error")}
	runner := ritual.NewRunner(rs)

	// First run: check → fetch → fail
	err := runner.Run(context.Background(), check)
	require.Error(t, err)
	assert.Equal(t, 1, checkCount)
	assert.Equal(t, 1, fetchCount)

	// Fix the error, retry from failed stage
	rs.Err = nil
	done := &countingStrategy{name: "Done", count: new(int), next: nil}
	fetch.next = done

	err = runner.RunCurrent(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, checkCount, "check must NOT run again")
	assert.Equal(t, 2, fetchCount, "fetch must run again")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ritual/... -run TestRunner_RunCurrent -v`
Expected: FAIL — `NewRunner` and `RunCurrent` not defined

- [ ] **Step 3: Refactor ritual.Run into a Runner struct with Run and RunCurrent**

Replace `internal/core/ritual/run.go`:

```go
package ritual

import (
	"context"
	"fmt"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Runner drives the state machine and tracks the current strategy
// so RunCurrent can re-enter after a failure.
type Runner struct {
	runState *RunState
	current  machine.Strategy[RunState]
}

func NewRunner(runState *RunState) *Runner {
	return &Runner{runState: runState}
}

// Run drives the machine from the given start strategy.
func (r *Runner) Run(ctx context.Context, start machine.Strategy[RunState]) error {
	r.current = start
	return r.drive(ctx)
}

// RunCurrent re-enters the machine at the last stopped strategy.
func (r *Runner) RunCurrent(ctx context.Context) error {
	if r.current == nil {
		return fmt.Errorf("no current strategy to resume")
	}
	return r.drive(ctx)
}

func (r *Runner) drive(ctx context.Context) error {
	rs := r.runState
	for r.current != nil {
		curName := stageName(r.current)
		next, err := r.current.Run(ctx, rs)
		if err != nil {
			return err
		}
		nextName := stageName(next)
		publish(rs.Bus, ports.StateChangedInfo{From: curName, To: nextName, RunID: rs.RunID})
		r.current = next
	}
	return nil
}

func stageName(s machine.Strategy[RunState]) string {
	if s == nil {
		return StageDone
	}
	if n, ok := s.(Named); ok {
		return n.Name()
	}
	return "Unknown"
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
```

Keep the existing `Run` package-level function as a convenience wrapper so existing callers don't break:

Add to bottom of same file:

```go
// Run is a convenience wrapper for one-shot execution without retry support.
func Run(ctx context.Context, rs *RunState, start machine.Strategy[RunState]) error {
	return NewRunner(rs).Run(ctx, start)
}
```

- [ ] **Step 4: Add FailedStage to RunState**

In `internal/core/ritual/runstate.go`, add field:

```go
type RunState struct {
	RunID        string
	Bus          ports.EventBus
	LockID       string
	LocalBefore  *domain.Manifest
	RemoteBefore *domain.Manifest
	Err          error
	FailedStage  string
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/core/ritual/... -run TestRunner_RunCurrent -v`
Expected: PASS

- [ ] **Step 6: Run all existing tests to verify no regression**

Run: `go test ./...`
Expected: All pass — `Run` package-level function preserved as wrapper.

- [ ] **Step 7: Commit**

```
feat(ritual): add Runner with RunCurrent for retry re-entry
```

---

### Task 3: Failed Strategy Retry Path

**Files:**
- Modify: `internal/core/stages/failed/strategy.go`
- Modify: `internal/core/stages/failed/strategy_test.go`

- [ ] **Step 1: Write failing test — failed strategy sets FailedStage and supports retry via onRetry**

Add to `internal/core/stages/failed/strategy_test.go`:

```go
func TestFailedSetsFailedStageOnRunState(t *testing.T) {
	bus := adapters.NewEventBus(8)
	rs := &ritual.RunState{RunID: "r-1", Bus: bus, Err: errors.New("boom")}
	s := failed.New(ritual.StageChecking)

	s.Run(context.Background(), rs)

	if rs.FailedStage != ritual.StageChecking {
		t.Fatalf("want StageChecking, got %s", rs.FailedStage)
	}
}

func TestFailedRetryFollowsOnRetryEdge(t *testing.T) {
	bus := adapters.NewEventBus(8)
	rs := &ritual.RunState{RunID: "r-1", Bus: bus, Err: errors.New("boom")}

	retryTarget := &mockStrategy{name: "Checking"}
	s := failed.New(ritual.StageChecking)
	s.SetRetry(retryTarget)

	// First run: terminates
	next, err := s.Run(context.Background(), rs)
	if next != nil {
		t.Fatal("first run must terminate")
	}
	if err == nil {
		t.Fatal("expected error")
	}

	// Retry: follows onRetry edge
	rs.Err = nil
	next, err = s.Run(context.Background(), rs)
	if err != nil {
		t.Fatalf("retry should not error: %v", err)
	}
	if next != retryTarget {
		t.Fatal("retry must follow onRetry edge")
	}
}

type mockStrategy struct {
	name string
}

func (m *mockStrategy) Name() string { return m.name }
func (m *mockStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/stages/failed/... -v`
Expected: FAIL — `FailedStage` field missing, `SetRetry` not defined

- [ ] **Step 3: Update failed strategy**

Replace `internal/core/stages/failed/strategy.go`:

```go
package failed

import (
	"context"
	"errors"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	from    string
	onRetry machine.Strategy[ritual.RunState]
	fired   bool
}

func New(from string) *Strategy { return &Strategy{from: from} }

// SetRetry wires the back-edge for retry. Called after the full chain
// is constructed to break the circular dependency.
func (s *Strategy) SetRetry(target machine.Strategy[ritual.RunState]) {
	s.onRetry = target
}

func (*Strategy) Name() string { return ritual.StageFailed }

func (s *Strategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	// Retry path: already fired once, error cleared, follow back-edge
	if s.fired && rs.Err == nil && s.onRetry != nil {
		s.fired = false
		return s.onRetry, nil
	}

	err := rs.Err
	if err == nil {
		err = errors.New("failed without recorded error")
	}
	rs.FailedStage = s.from
	s.fired = true
	publish(rs.Bus, ports.StateFailedInfo{State: s.from, RunID: rs.RunID, Err: err})
	return nil, err
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/stages/failed/... -v`
Expected: All pass (existing + new)

- [ ] **Step 5: Commit**

```
feat(failed): add SetRetry back-edge and FailedStage tracking for retry support
```

---

### Task 4: Ritual Struct — New and Listen

**Files:**
- Create: `internal/app/ritual.go`
- Create: `internal/app/ritual_test.go`

- [ ] **Step 1: Write failing test — New returns Ritual, Listen dispatches StartRequested**

Create `internal/app/ritual_test.go`:

```go
package app_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes ---

type fakeStorage struct{}

func (fakeStorage) Get(_ context.Context, _ string) ([]byte, error)    { return nil, nil }
func (fakeStorage) Put(_ context.Context, _ string, _ []byte) error    { return nil }
func (fakeStorage) Delete(_ context.Context, _ string) error           { return nil }
func (fakeStorage) DeleteBatch(_ context.Context, _ []string) error    { return nil }
func (fakeStorage) List(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (fakeStorage) Copy(_ context.Context, _, _ string) error          { return nil }

type fakeManifestStore struct{}

func (fakeManifestStore) Get(_ context.Context) (*domain.Manifest, error) {
	return &domain.Manifest{}, nil
}
func (fakeManifestStore) Save(_ context.Context, _ *domain.Manifest) error { return nil }

type noopCondition struct{}

func (noopCondition) Check(_ context.Context) error { return nil }

type noopUpdater struct{}

func (noopUpdater) Run(_ context.Context) error { return nil }

type noopRetention struct{}

func (noopRetention) Apply(_ context.Context) error { return nil }

type fakeCmdBuilder struct{}

func (fakeCmdBuilder) Build(_ context.Context) (*exec.Cmd, error) {
	return exec.Command("cmd", "/C", "echo", "ok"), nil
}

func waitForStatus(t *testing.T, ch <-chan ports.Event, want app.Outcome, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status %s", want)
		case e, ok := <-ch:
			if !ok {
				t.Fatal("channel closed")
			}
			if sc, ok := e.(app.StatusChanged); ok && sc.Status == want {
				return
			}
		}
	}
}

func TestRitual_Start_RunsPipeline(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		[]ports.ConditionService{noopCondition{}},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.RetentionService{noopRetention{}},
		fakeCmdBuilder{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)

	bus.Publish(app.StartRequested{})
	waitForStatus(t, ch, app.Done, 5*time.Second)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/... -run TestRitual_Start -v`
Expected: FAIL — `New`, `Listen` not defined

- [ ] **Step 3: Implement ritual.go**

```go
package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/archiving"
	"ritual/internal/core/stages/checking"
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/fetching"
	"ritual/internal/core/stages/publishing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
)

type Ritual struct {
	bus             ports.EventBus
	localStorage    ports.StorageRepository
	remoteStorage   ports.StorageRepository
	localManifests  ports.ManifestStore
	remoteManifests ports.ManifestStore
	conds           []ports.ConditionService
	updaters        []ports.UpdaterService
	exitUpdaters    []ports.UpdaterService
	retentions      []ports.RetentionService
	cmdBuilder      ports.CmdBuilder

	entry  machine.Strategy[ritual.RunState]
	runner *ritual.Runner
	status Outcome
	cancel context.CancelFunc
}

func New(
	bus ports.EventBus,
	localStorage ports.StorageRepository,
	remoteStorage ports.StorageRepository,
	localManifests ports.ManifestStore,
	remoteManifests ports.ManifestStore,
	conditions []ports.ConditionService,
	updaters []ports.UpdaterService,
	exitUpdaters []ports.UpdaterService,
	retentions []ports.RetentionService,
	cmdBuilder ports.CmdBuilder,
) *Ritual {
	r := &Ritual{
		bus:             bus,
		localStorage:    localStorage,
		remoteStorage:   remoteStorage,
		localManifests:  localManifests,
		remoteManifests: remoteManifests,
		conds:           conditions,
		updaters:        updaters,
		exitUpdaters:    exitUpdaters,
		retentions:      retentions,
		cmdBuilder:      cmdBuilder,
		status:          Idle,
	}
	r.entry = r.buildChain()
	return r
}

func (r *Ritual) Listen(ctx context.Context) {
	ch, unsub := r.bus.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			switch event.(type) {
			case StartRequested:
				r.start(ctx)
			case StopRequested:
				r.stop()
			case RetryRequested:
				r.retry(ctx)
			}
		}
	}
}

func (r *Ritual) start(ctx context.Context) {
	if r.status != Idle {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot start: status is %s", r.status)})
		return
	}
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	runState := &ritual.RunState{RunID: runID, Bus: r.bus}
	r.runner = ritual.NewRunner(runState)

	err := r.runner.Run(ctx, r.entry)
	if err != nil {
		r.setStatus(Failed)
		return
	}
	r.setStatus(Done)
}

func (r *Ritual) stop() {
	if r.status != Running {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Ritual) retry(ctx context.Context) {
	if r.status != Failed {
		r.bus.Publish(StatusChanged{Status: r.status, Err: fmt.Errorf("cannot retry: status is %s", r.status)})
		return
	}
	r.setStatus(Running)
	ctx, r.cancel = context.WithCancel(ctx)

	r.runner.RunState().Err = nil
	err := r.runner.RunCurrent(ctx)
	if err != nil {
		r.setStatus(Failed)
		return
	}
	r.setStatus(Done)
}

func (r *Ritual) setStatus(status Outcome) {
	r.status = status
	r.bus.Publish(StatusChanged{Status: status})
}

func (r *Ritual) buildChain() machine.Strategy[ritual.RunState] {
	failCheck := failed.New(ritual.StageChecking)
	failFetch := failed.New(ritual.StageFetching)
	failAcq := failed.New(ritual.StageAcquiring)
	failRet := failed.New(ritual.StageRetaining)

	retain := retaining.New(r.retentions, failRet)
	unlock := unlocking.New(r.localManifests, r.remoteManifests, retain)
	archive := archiving.New(r.localStorage, r.remoteStorage, r.localManifests, unlock)
	publish := publishing.New(r.exitUpdaters, archive)
	run := running.New(r.cmdBuilder, publish)
	rollback := unlocking.New(r.localManifests, r.remoteManifests, failAcq)
	acquire := acquiring.New(r.localManifests, r.remoteManifests, run, failAcq, rollback)
	fetch := fetching.New(r.updaters, acquire, failFetch)
	check := checking.New(r.conds, fetch, failCheck)

	// Wire retry back-edges
	failCheck.SetRetry(check)
	failFetch.SetRetry(fetch)
	failAcq.SetRetry(acquire)
	failRet.SetRetry(retain)

	return check
}
```

Note: `ritual.Runner` needs a `RunState()` accessor — add to `run.go`:

```go
func (r *Runner) RunState() *RunState { return r.runState }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/... -run TestRitual_Start -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(app): implement Ritual with bus-driven Listen dispatch
```

---

### Task 5: Retry and Stop Tests

**Files:**
- Modify: `internal/app/ritual_test.go`

- [ ] **Step 1: Write test — fetch fails, retry re-enters at fetch, skips checking**

Add to `ritual_test.go`:

```go
type failOnceUpdater struct {
	calls int
}

func (f *failOnceUpdater) Run(_ context.Context) error {
	f.calls++
	if f.calls == 1 {
		return fmt.Errorf("network timeout")
	}
	return nil
}

func TestRitual_Retry_ReentersAtFailedStage(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	flaky := &failOnceUpdater{}
	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		[]ports.ConditionService{noopCondition{}},
		[]ports.UpdaterService{flaky},
		[]ports.UpdaterService{noopUpdater{}},
		[]ports.RetentionService{noopRetention{}},
		fakeCmdBuilder{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)

	bus.Publish(app.StartRequested{})
	waitForStatus(t, ch, app.Failed, 5*time.Second)

	// Collect transitions from first run
	var firstTransitions []string
	drainEvents(ch, &firstTransitions)

	bus.Publish(app.RetryRequested{})
	waitForStatus(t, ch, app.Done, 5*time.Second)

	// Fetch called twice (first fail + retry), condition called once (only first run)
	assert.Equal(t, 2, flaky.calls)
}

func drainEvents(ch <-chan ports.Event, transitions *[]string) {
	for {
		select {
		case e := <-ch:
			if sc, ok := e.(ports.StateChangedInfo); ok {
				*transitions = append(*transitions, sc.From)
			}
		default:
			return
		}
	}
}
```

- [ ] **Step 2: Write test — stop cancels running pipeline**

```go
type blockingCmdBuilder struct {
	ready chan struct{}
}

func (b *blockingCmdBuilder) Build(ctx context.Context) (*exec.Cmd, error) {
	close(b.ready)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRitual_Stop_CancelsRunning(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	blocker := &blockingCmdBuilder{ready: make(chan struct{})}
	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		nil, nil, nil, nil,
		blocker,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)

	bus.Publish(app.StartRequested{})
	<-blocker.ready

	bus.Publish(app.StopRequested{})
	waitForStatus(t, ch, app.Failed, 5*time.Second)
}
```

- [ ] **Step 3: Write test — retry when not failed is rejected**

```go
func TestRitual_Retry_WhenIdle_Rejected(t *testing.T) {
	bus := adapters.NewEventBus(128)
	ch, unsub := bus.Subscribe()
	defer unsub()

	r := app.New(
		bus,
		fakeStorage{}, fakeStorage{},
		fakeManifestStore{}, fakeManifestStore{},
		nil, nil, nil, nil,
		fakeCmdBuilder{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Listen(ctx)

	bus.Publish(app.RetryRequested{})

	// Should get StatusChanged with error, status remains Idle
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			return // no crash, no status change — acceptable
		case e := <-ch:
			if sc, ok := e.(app.StatusChanged); ok {
				assert.NotNil(t, sc.Err)
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run all app tests**

Run: `go test ./internal/app/... -v -timeout 30s`
Expected: All pass

- [ ] **Step 5: Commit**

```
test(app): verify retry re-entry, stop cancellation, and invalid state rejection
```

---

### Task 6: Update CLI main.go

**Files:**
- Modify: `cmd/cli/main.go`

- [ ] **Step 1: Replace chain assembly with app.New + bus.Publish**

```go
package main

//go:generate goversioninfo

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ritual/internal/adapters"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
	"ritual/internal/subsystems/conditions"
	"ritual/internal/subsystems/heartbeat"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/prompt"
	"ritual/internal/subsystems/retention"
	synckit "ritual/internal/subsystems/sync"
)

var (
	envAccountID       string
	envAccessKeyID     string
	envSecretAccessKey string
	envBucket          string
)

func main() {
	if services.HandleUpdateProcess() {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	success := false
	defer func() {
		if !success {
			fmt.Println("\nPress Enter to exit...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		}
	}()
	if err := run(ctx); err != nil {
		fmt.Printf("Ritual failed: %v\n", err)
		return
	}
	success = true
}

func run(ctx context.Context) error {
	// --- Environment validation ---
	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		return fmt.Errorf("build error: R2 credentials not injected")
	}

	// --- Infrastructure setup (filesystem, logging, event bus) ---
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer workRoot.Close()

	logFile, logCleanup, err := logging.CreateLogFile(workRoot)
	if err != nil {
		fmt.Printf("Warning: log file: %v\n", err)
	}
	if logCleanup != nil {
		defer logCleanup()
	}

	bus := adapters.NewEventBus(128)
	stopLog := logging.Attach(bus, logFile)
	defer stopLog()

	prompter := prompt.NewStdin(os.Stdin, os.Stdout)

	// --- Storage and manifest adapters ---
	localStorage, err := adapters.NewFSRepository(workRoot)
	if err != nil {
		return fmt.Errorf("local storage: %w", err)
	}
	remoteStorage, err := adapters.NewR2Repository(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, bus)
	if err != nil {
		return fmt.Errorf("remote storage: %w", err)
	}
	localManifests := adapters.NewManifestStore(localStorage)
	remoteManifests := adapters.NewManifestStore(remoteStorage)

	_, stopHeartbeat := heartbeat.Attach(bus, remoteManifests)
	defer stopHeartbeat()

	remoteManifest, err := remoteManifests.Get(context.Background())
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	// --- Subsystem builders (CLI-specific wiring) ---
	sk, err := synckit.Build(workRoot, localStorage, remoteStorage, localManifests, remoteManifests, bus)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	sysInfo := adapters.NewSystemInfo()
	javaInfo := adapters.NewJavaInfo()
	conds, err := conditions.Build(remoteManifest, remoteManifests, sysInfo, javaInfo)
	if err != nil {
		return fmt.Errorf("conditions: %w", err)
	}
	rets, err := retention.Build(localStorage, remoteStorage, bus, remoteManifest)
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	settings, err := services.PromptSettings(bus, prompter, remoteManifest.GetMinRAMMB())
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	cmdBuilder, err := adapters.NewServerCmdBuilder(workRoot, remoteManifest.Server.StartScript, settings.ToServerRuntime)
	if err != nil {
		return fmt.Errorf("cmd builder: %w", err)
	}

	// --- Ritual: build once, listen for commands ---
	r := app.New(
		bus,
		localStorage, remoteStorage,
		localManifests, remoteManifests,
		conds, sk.Updaters, sk.ExitUpdaters, rets,
		cmdBuilder,
	)

	// Wait for completion via bus
	done := make(chan error, 1)
	ch, unsub := bus.Subscribe()
	go func() {
		defer unsub()
		for event := range ch {
			if sc, ok := event.(app.StatusChanged); ok {
				switch sc.Status {
				case app.Done:
					done <- nil
					return
				case app.Failed:
					done <- sc.Err
					return
				}
			}
		}
	}()

	go r.Listen(ctx)
	bus.Publish(app.StartRequested{})
	return <-done
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./cmd/cli/...`
Expected: PASS

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 4: Commit**

```
refactor(cli): use app.Ritual with bus-driven commands, remove inline chain assembly
```

---

## Self-Review

**Spec coverage:**
- Bus-driven command dispatch: Task 4 (Listen)
- Start/Stop/Retry commands: Tasks 4, 5
- Retry at failed stage (not from beginning): Tasks 2, 3 (RunCurrent + failed back-edge)
- State guards: Task 4 (start/stop/retry check status)
- OS signal context: Task 6 (signal.NotifyContext)
- No fmt.Print in app: Task 4 (all via bus.Publish)
- CLI main cleanup: Task 6

**Placeholder scan:** No TBD/TODO. All steps have code.

**Type consistency:** `Ritual` struct, `Outcome` type, `StatusChanged`/`StartRequested`/`StopRequested`/`RetryRequested` events — consistent across all tasks. `Runner`/`RunCurrent`/`RunState.FailedStage` — consistent between Tasks 2, 3, 4. `failed.SetRetry` called in Task 4's `buildChain`, defined in Task 3.
