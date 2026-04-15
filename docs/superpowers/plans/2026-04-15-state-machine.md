# Molfar → State Machine Implementation Plan (v3)

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox (`- [ ]`) tracking.

**Goal:** Replace `MolfarService` with explicit state machine over a fan-out `EventBus`. Migrate every legacy `chan<- ports.Event` callsite to `EventBus` *in-sprint* (no bridge scaffolding). Extract user-input RPC from events into a dedicated `Prompter` port.

**LOC target:** ≥ −500 net vs Molfar baseline. Earlier ≥ −600 estimate adjusted after adding GUI-readiness scope (ctx discipline + `ServerRunner.Run(ctx, ...)`). Lower delta accepted in exchange for shipping a Wails-ready core, no future migration debt.

**Design principles (per user memory):** hexagonal, DI, SOLID/ISP, minimal code, tests as docs, expressiveness through ideas, flat code (early returns, no nested ifs, no `else`), patterns are vocabulary not templates.

**Architecture decisions:**

- **Core knows nothing about GUI.** The `internal/core/statemachine/` package depends only on `context`, `log/slog`, `internal/core/ports`, and `internal/core/domain`. It does not import any `cmd/*`, GUI framework, or front-end-specific package. Hexagonal boundary makes this a compile-checked invariant. GUI integration is a future sprint that wraps the existing surfaces — no core code changes when GUI lands.
- **State pattern.** Each lifecycle phase = small struct implementing `Handler`. Transitions by returning next `Handler`. `nil` = terminal success. No `DoneState`.
- **Constructor injection.** Each state holds only its own deps. No god-struct `MachineContext`. State fields *are* the contract.
- **Abstract Factory.** `StateFactory` owns the dep graph and wires states. Transitions pass typed payloads through factory args (`factory.Exiting(lockID, manifests)`), compiler-checked. Factory methods live next to each state.
- **No `Builder`.** `main.go` builds `Deps` and calls `NewFactory(deps)` directly.
- **No `Enterer`/`Exiter` hooks.** Brackets that span phases (lock acquire/release) modeled as state pairs (Locking ↔ Unlocking/Exiting). Brackets within a phase use `defer`.
- **`StateName` as typed string.** `type StateName string` with const values. Free `Stringer`, free JSON/slog, no int-enum switch.
- **No injected logger.** Logging is a bus subscriber. Every observation a state would log is already an event (`StartInfo`/`UpdateInfo`/`FinishInfo`/`ErrorInfo`). The CLI consumer subscribes and writes to stdout + log file. Single observability path; no `bus.Publish` vs `logger.Info` ambiguity. Adapter-level loggers (e.g. AWS SDK) stay scoped to their adapter.
- **Event = `fmt.Stringer`.** Open set; any package can define new event types. Constraint earns its keep — every event self-describes for logs/`%v`/slog.
- **EventBus.** Pubsub fan-out. Non-blocking publish (slow subs drop). `Subscribe()` returns every event; subscribers filter via `switch evt.(type)`. No reflection in the bus. If type or predicate filtering becomes load-bearing later, add a Decorator (`adapters.WithTypes(bus, ErrorInfo{})` / `adapters.WithFilter(bus, fn)`) — bus interface stays unchanged.
- **Prompter port.** User-input RPC extracted from events into `ports.Prompter`. `PromptEvent` and its `ResponseChan` are deleted. Fan-out + RPC are incompatible — multiple subscribers would race the response.
- **Deferred to GUI sprint:** Command (GUI input dispatch), `CoolDown`/`Idle` states, OTel tracer.
- **Out of scope (not deferred — actively rejected):** Memento / crash recovery. Manifest lock semantics already cover the crash scenario: if a run crashes, `LockedBy` stays set, next run sees "already locked" and exits with a clear error. User clears manually. Persisting in-flight state to resume mid-phase adds significant complexity for a problem the lock already solves.

**Tech stack:** Go stdlib (`sync`, `context`, `fmt`, `errors`), existing ports/domain, zero new deps.

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `internal/core/ports/eventbus.go` | `EventBus` interface |
| `internal/core/ports/prompter.go` | `Prompter` interface (RPC for user input) |
| `internal/adapters/eventbus.go` | fan-out adapter; non-blocking publish; cancel-safe subscribe |
| `internal/adapters/eventbus_test.go` | subscribe/publish/cancel/drop/multi-sub-fanout |
| `internal/core/statemachine/handler.go` | `StateName` typed string + consts + `Handler` interface |
| `internal/core/statemachine/machine.go` | `Machine.Run` loop; emits `StateChangedInfo` |
| `internal/core/statemachine/machine_test.go` | transitions; terminal; error propagation |
| `internal/core/statemachine/factory.go` | `StateFactory` interface + `Deps` struct + concrete factory |
| `internal/core/statemachine/factory_test.go` | wiring smoke tests |
| `internal/core/statemachine/preparing.go` (+ `_test.go`) | `PreparingState` |
| `internal/core/statemachine/locking.go`   (+ `_test.go`) | `LockingState` |
| `internal/core/statemachine/running.go`   (+ `_test.go`) | `RunningState` |
| `internal/core/statemachine/exiting.go`   (+ `_test.go`) | `ExitingState` |
| `internal/core/statemachine/unlocking.go` (+ `_test.go`) | `UnlockingState` |
| `internal/core/statemachine/failed.go`    (+ `_test.go`) | `FailedState` |
| `cmd/cli/prompter.go` | stdin `Prompter` implementation |
| `docs/state-machine.md` | promoted from proposal |

### Modified files

| Path | Change |
|---|---|
| `internal/core/ports/events.go` | **Rewrite.** `type Event = fmt.Stringer`. Replace sealed-interface variants with `*Info` payload structs that implement `String()`. Add `StateChangedInfo`, `StateFailedInfo`. Delete `PromptEvent`, `SendEvent`. |
| `internal/core/ports/events_test.go` | Delete `SendEvent` tests; keep one `String()` smoke test per payload (asserts format). |
| `internal/adapters/r2.go` | Field `events chan<- ports.Event` → `bus ports.EventBus`. `send()` → `publish()` calling `bus.Publish`. `*Event` types → `*Info`. Same for `progressReadCloser`. |
| `internal/core/services/sync.go` | Same ctor + field swap; one `SendEvent` callsite → `bus.Publish`. |
| `internal/core/services/retention_logs.go` | Same. |
| `internal/core/services/settings.go` | Ctor signature `(bus ports.EventBus, prompter ports.Prompter, minRAMMB int)`. Delete `PromptEvent` round-trip; replace with `prompter.Prompt(ctx, id, prompt, default)`. Validation feedback → `bus.Publish(UpdateInfo{...})`. |
| `cmd/cli/main.go` | Delete legacy `events` chan + `close(events)`. Construct `bus`, `stdinPrompter`. Subscribe consumer. Build `Deps` (incl. `Bus`, `Prompter`). `NewFactory(deps)` → `NewMachine(factory.Preparing(), bus, runID).Run(ctx)`. |
| `cmd/cli/consumer.go` | Signature `<-chan ports.Event`. Switch over `*Info` payload types or default to `fmt.Fprintln(w, evt)` (Stringer). Delete `handlePrompt` (moves to `cmd/cli/prompter.go`). |
| `docs/event-architecture.md` | Rewrite to reflect Stringer events + EventBus + Prompter port. |
| `docs/structure.md` | Replace Molfar section with state machine + EventBus + Prompter blurb. |
| `docs/progress.md` | Sprint 6 entry. |

### Deletion Manifest

Code deletions:

| Path / Symbol | Reason | Phase |
|---|---|---|
| `internal/core/services/molfar.go` | replaced by state machine | 6 |
| `internal/core/services/molfar_test.go` | replaced by per-state tests | 6 |
| `internal/core/ports/mocks/molfar.go` | port removed | 6 |
| `internal/core/ports/mocks/molfar_test.go` | mock removed | 6 |
| `ports.MolfarService` interface in `internal/core/ports/ports.go` | port removed | 6 |
| `ports.PromptEvent` struct (in events.go) | replaced by `Prompter` port (RPC ≠ event) | 1 |
| `ports.SendEvent` helper (in events.go) | replaced by non-blocking `bus.Publish` | 1 |
| `ports.StartEvent` / `UpdateEvent` / `FinishEvent` / `ErrorEvent` (sealed-interface variants) | replaced by `*Info` Stringer payloads | 1 |
| `Event interface { sealed() }` and all `sealed()` markers | replaced by `type Event = fmt.Stringer` | 1 |
| `events_test.go` SendEvent test cases (10 tests) | covered by `eventbus_test.go` | 1 |
| `(r *R2Repository).send(evt)` helper | replaced by `publish(evt)` calling bus | 16 |
| `events chan<- ports.Event` field/param across services and adapters | replaced by `bus ports.EventBus` | 15–17 |
| `events := make(chan ports.Event, 100)` + `close(events)` in main | bus owns lifecycle | 21 |
| `handlePrompt` function in consumer.go | moved to `cmd/cli/prompter.go` | 19, 20 |
| `responseChan` round-trip in `promptWithValidation` | replaced by `Prompter.Prompt` RPC | 15 |

Doc deletions / rewrites:

| Path | Action | Phase |
|---|---|---|
| `docs/state-machine-proposal.md` | renamed to `docs/state-machine.md` (content rewritten) | 24 |
| `docs/event-architecture.md` Phase 1–6 step list (sealed-interface plan) | rewritten for new design | 25 |
| Molfar component section in `docs/structure.md` | rewritten as state machine + bus + prompter | 26 |
| Stale `StartEvent` / `SendEvent` / `sealed()` references in any other doc | updated by sweep | 28 |

---

## Phase 1 — Ports

### Task 1: Rewrite `events.go`

- [ ] Replace `internal/core/ports/events.go`:

```go
package ports

import "fmt"

// Event is any fmt.Stringer. Open set, self-describing, compile-safe.
//
// To add a new event type:
//   1. Define a struct (anywhere in the codebase, no central registry).
//   2. Implement String() string — used by default consumers and logs.
//   3. Publish via bus.Publish(MyEvent{...}).
//
// Conventions:
//   - Use UpdateInfo{Operation, Message, Data} for generic progress; only
//     define a new type when you have unique structured fields.
//   - Throttle high-frequency publishes at the call site — slow subscribers
//     drop, and console floods are unfriendly.
//   - Namespace event names if defined outside core (e.g. gui.ScreenChangedInfo).
//   - Per-subscriber FIFO is preserved; cross-subscriber order is not.
//   - Bus delivery is non-blocking and observability-grade. For durable record
//     (audit, billing), attach a file-writing subscriber — out of scope here,
//     trivial when needed.
type Event = fmt.Stringer

type StartInfo struct{ Operation string }

func (s StartInfo) String() string { return fmt.Sprintf("start %s", s.Operation) }

type UpdateInfo struct {
    Operation, Message string
    Data               map[string]any
}

func (u UpdateInfo) String() string {
    if p, ok := u.Data["percent"]; ok {
        return fmt.Sprintf("%s: %s (%.1f%%)", u.Operation, u.Message, p)
    }
    return fmt.Sprintf("%s: %s", u.Operation, u.Message)
}

type FinishInfo struct{ Operation string }

func (f FinishInfo) String() string { return fmt.Sprintf("finish %s", f.Operation) }

type ErrorInfo struct {
    Operation string
    Err       error
}

func (e ErrorInfo) String() string { return fmt.Sprintf("error %s: %v", e.Operation, e.Err) }

type StateChangedInfo struct{ From, To, RunID string }

func (s StateChangedInfo) String() string { return fmt.Sprintf("%s → %s", s.From, s.To) }

type StateFailedInfo struct {
    State, RunID string
    Err          error
}

func (s StateFailedInfo) String() string { return fmt.Sprintf("failed in %s: %v", s.State, s.Err) }
```

- [ ] Update `internal/core/ports/events_test.go`: delete all `SendEvent` tests; add one `String()` smoke per payload (e.g. `assertEqual(t, "start backup", StartInfo{Operation: "backup"}.String())`).
- [ ] `go build ./...` — expect failures in callers (covered by Phase 4).
- [ ] Commit: `feat(events): Stringer-based events with *Info payloads`

---

### Task 2: `EventBus` port

- [ ] Create `internal/core/ports/eventbus.go`:

```go
package ports

// EventBus is a pubsub fan-out. Multiple subscribers can attach; each
// receives every published event.
//
// Publish is non-blocking. If a subscriber's buffer is full the event is
// dropped for that subscriber — producers never stall on slow consumers.
// This is observability-grade, not audit-grade. For durable logging, attach
// a subscriber that writes synchronously to a file (trivial; out of scope here).
//
// Subscribe takes no arguments: the bus delivers every event. Subscribers
// filter in their own loop:
//   ch, cancel := bus.Subscribe()
//   defer cancel()
//   for evt := range ch {
//       switch e := evt.(type) {
//       case ports.ErrorInfo:        handleErr(e)
//       case ports.StateChangedInfo: route(e.To)
//       }
//   }
//
// For type or predicate filtering, wrap the bus with a Decorator:
//   errs, _ := adapters.WithTypes(bus, ports.ErrorInfo{}).Subscribe()
//   ops,  _ := adapters.WithFilter(bus, func(e ports.Event) bool { ... }).Subscribe()
// Decorators are not provided today — added when the first real consumer needs them.
//
// The returned cancel func closes the channel and removes the subscription.
// Calling cancel twice is safe (no-op the second time).
//
// Per-subscriber FIFO order is preserved; cross-subscriber interleaving
// is not guaranteed.
//
// Typical buffer per subscriber: 64–128 events.
type EventBus interface {
    Publish(evt Event)
    Subscribe() (<-chan Event, func())
}
```

- [ ] Commit: `feat(ports): add EventBus port (Publish + Subscribe)`

---

### Task 3: `Prompter` port

- [ ] Create `internal/core/ports/prompter.go`:

```go
package ports

import "context"

// Prompter requests a single line of user input.
// Implementations: stdin (CLI), modal dialog (GUI), scripted (tests).
//
// Synchronous RPC, separate from the fan-out EventBus —
// fan-out semantics would race the response across subscribers.
type Prompter interface {
    Prompt(ctx context.Context, id, prompt, defaultValue string) (string, error)
}
```

- [ ] Commit: `feat(ports): add Prompter port for user-input RPC`

---

### Task 4: EventBus adapter (TDD)

- [ ] Write `internal/adapters/eventbus_test.go`:

```go
package adapters_test

import (
    "io"
    "testing"
    "time"

    "ritual/internal/adapters"
    "ritual/internal/core/ports"
)

func TestEventBus_Subscribe_Receives(t *testing.T) {
    bus := adapters.NewEventBus(8)
    ch, cancel := bus.Subscribe()
    defer cancel()
    bus.Publish(ports.StartInfo{Operation: "x"})
    select {
    case evt := <-ch:
        if evt.(ports.StartInfo).Operation != "x" {
            t.Fatalf("got %+v", evt)
        }
    case <-time.After(100 * time.Millisecond):
        t.Fatal("no event")
    }
}

func TestEventBus_Cancel_Closes(t *testing.T) {
    bus := adapters.NewEventBus(8)
    ch, cancel := bus.Subscribe()
    cancel()
    if _, ok := <-ch; ok {
        t.Fatal("channel not closed")
    }
}

func TestEventBus_SlowSub_NoBlock(t *testing.T) {
    bus := adapters.NewEventBus(1)
    _, cancel := bus.Subscribe()
    defer cancel()
    done := make(chan struct{})
    go func() {
        for i := 0; i < 100; i++ {
            bus.Publish(ports.StartInfo{Operation: "x"})
        }
        close(done)
    }()
    select {
    case <-done:
    case <-time.After(500 * time.Millisecond):
        t.Fatal("publisher blocked")
    }
}

func TestEventBus_DeliversAllEventsToAllSubscribers(t *testing.T) {
    bus := adapters.NewEventBus(16)
    ch1, cancel1 := bus.Subscribe()
    defer cancel1()
    ch2, cancel2 := bus.Subscribe()
    defer cancel2()

    bus.Publish(ports.StartInfo{Operation: "x"})
    bus.Publish(ports.ErrorInfo{Operation: "y", Err: io.EOF})

    drain := func(ch <-chan ports.Event) (got int) {
        deadline := time.After(100 * time.Millisecond)
        for got < 2 {
            select {
            case <-ch:
                got++
            case <-deadline:
                return
            }
        }
        return
    }
    if n := drain(ch1); n != 2 {
        t.Fatalf("sub1 got %d, want 2", n)
    }
    if n := drain(ch2); n != 2 {
        t.Fatalf("sub2 got %d, want 2", n)
    }
}
```

- [ ] Implement `internal/adapters/eventbus.go`:

```go
package adapters

import (
    "sync"

    "ritual/internal/core/ports"
)

type eventBus struct {
    mu     sync.RWMutex
    subs   map[int]chan ports.Event
    next   int
    bufLen int
}

func NewEventBus(bufLen int) ports.EventBus {
    if bufLen < 1 {
        bufLen = 64
    }
    return &eventBus{subs: map[int]chan ports.Event{}, bufLen: bufLen}
}

func (b *eventBus) Publish(evt ports.Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.subs {
        select {
        case ch <- evt:
        default:
        }
    }
}

func (b *eventBus) Subscribe() (<-chan ports.Event, func()) {
    b.mu.Lock()
    id := b.next
    b.next++
    ch := make(chan ports.Event, b.bufLen)
    b.subs[id] = ch
    b.mu.Unlock()

    return ch, func() {
        b.mu.Lock()
        defer b.mu.Unlock()
        c, ok := b.subs[id]
        if !ok {
            return
        }
        delete(b.subs, id)
        close(c)
    }
}
```

- [ ] `go test ./internal/adapters/ -run TestEventBus -v` → 4 PASS
- [ ] Commit: `feat(adapters): EventBus fan-out (publish + subscribe + cancel)`

---

## Phase 2 — Machine core

### Task 5: `StateName` + `Handler`

- [ ] Create `internal/core/statemachine/handler.go`:

```go
package statemachine

import "context"

// StateName is a typed string. Free Stringer, free JSON/slog.
type StateName string

const (
    Preparing StateName = "Preparing"
    Locking   StateName = "Locking"
    Running   StateName = "Running"
    Exiting   StateName = "Exiting"
    Unlocking StateName = "Unlocking"
    Failed    StateName = "Failed"
)

// Handler is the state contract. Returns next state; nil = terminal success.
// Each state struct carries its own deps (constructor injection).
//
// No Enter/Exit hooks: brackets that span phases (e.g. lock acquire/release)
// are modeled as state pairs (Locking ↔ Unlocking/Exiting). Brackets within
// a phase use defer inside Handle.
type Handler interface {
    Name() StateName
    Handle(ctx context.Context) (Handler, error)
}
```

- [ ] Commit: `feat(statemachine): StateName + Handler interface`

---

### Task 6: `Machine` loop (TDD)

- [ ] Write `internal/core/statemachine/machine_test.go`:

```go
package statemachine_test

import (
    "context"
    "errors"
    "testing"

    "ritual/internal/adapters"
    "ritual/internal/core/ports"
    sm "ritual/internal/core/statemachine"
)

type step struct {
    n    sm.StateName
    next sm.Handler
    err  error
}

func (s *step) Name() sm.StateName                                { return s.n }
func (s *step) Handle(_ context.Context) (sm.Handler, error)      { return s.next, s.err }

func TestMachine_TransitionsAndEmitsChange(t *testing.T) {
    bus := adapters.NewEventBus(16)
    ch, cancel := bus.Subscribe()
    defer cancel()

    end := &step{n: "B"}
    start := &step{n: "A", next: end}

    m := sm.NewMachine(start, bus, "r1")
    if err := m.Run(context.Background()); err != nil {
        t.Fatalf("run: %v", err)
    }

    var got []ports.StateChangedInfo
    deadline := time.After(100 * time.Millisecond)
    for len(got) < 2 {
        select {
        case evt := <-ch:
            if sc, ok := evt.(ports.StateChangedInfo); ok {
                got = append(got, sc)
            }
        case <-deadline:
            t.Fatalf("got %d transitions, want 2", len(got))
        }
    }
    if got[0].From != "A" || got[0].To != "B" {
        t.Errorf("transition[0] = %+v", got[0])
    }
    if got[1].From != "B" || got[1].To != "Done" {
        t.Errorf("transition[1] = %+v", got[1])
    }
}

func TestMachine_PropagatesError(t *testing.T) {
    bus := adapters.NewEventBus(16)
    boom := errors.New("boom")
    m := sm.NewMachine(&step{n: "A", err: boom}, bus, "r1")
    if err := m.Run(context.Background()); !errors.Is(err, boom) {
        t.Fatalf("err = %v", err)
    }
}
```

(Add `"time"` import.)

- [ ] Implement `internal/core/statemachine/machine.go`:

```go
package statemachine

import (
    "context"
    "errors"

    "ritual/internal/core/ports"
)

type Machine struct {
    current Handler
    bus     ports.EventBus
    runID   string
}

func NewMachine(initial Handler, bus ports.EventBus, runID string) *Machine {
    return &Machine{current: initial, bus: bus, runID: runID}
}

func (m *Machine) Run(ctx context.Context) error {
    if m.current == nil {
        return errors.New("nil initial state")
    }
    for {
        next, err := m.current.Handle(ctx)
        if err != nil {
            return err
        }
        to := StateName("Done")
        if next != nil {
            to = next.Name()
        }
        m.publish(ports.StateChangedInfo{
            From:  string(m.current.Name()),
            To:    string(to),
            RunID: m.runID,
        })
        if next == nil {
            return nil
        }
        m.current = next
    }
}

func (m *Machine) publish(evt ports.Event) {
    if m.bus != nil {
        m.bus.Publish(evt)
    }
}
```

- [ ] `go test ./internal/core/statemachine/ -run TestMachine` → PASS
- [ ] Commit: `feat(statemachine): Machine.Run with StateChangedInfo emission`

---

### Task 7: `StateFactory` + `Deps`

- [ ] Create `internal/core/statemachine/factory.go`:

```go
package statemachine

import (
    "ritual/internal/core/domain"
    "ritual/internal/core/ports"
)

// StateFactory builds states with their deps. Transition payloads flow as args.
type StateFactory interface {
    Preparing() Handler
    Locking() Handler
    Running() Handler
    Exiting(lockID string, localBefore, remoteBefore *domain.Manifest) Handler
    Unlocking(lockID string, cause error) Handler
    Failed(from StateName, err error) Handler
    RunID() string
}

// Deps is the full dependency set for a run. Constructed once in main.go.
type Deps struct {
    Bus          ports.EventBus
    Prompter     ports.Prompter
    RunID        string
    Server       *domain.ServerRuntime
    Librarian    ports.LibrarianService
    LocalStore   ports.StorageRepository
    RemoteStore  ports.StorageRepository
    ServerRunner ports.ServerRunner
    Conditions   []ports.ConditionService
    Updaters     []ports.UpdaterService
    ExitUpdaters []ports.UpdaterService
    Retentions   []ports.RetentionService
}

type factory struct{ d Deps }

func NewFactory(d Deps) StateFactory { return &factory{d: d} }

func (f *factory) RunID() string { return f.d.RunID }
```

(Concrete builder methods — `Preparing()`, `Locking()`, etc. — live next to each state file.)

- [ ] Commit: `feat(statemachine): StateFactory interface + Deps`

---

## Phase 3 — States (TDD, ctor-injected)

Pattern per state:
1. Struct fields = only the deps it uses.
2. `Handle` returns next via factory: `s.factory.Locking()`, `s.factory.Failed(Preparing, err)`.
3. **Ctx discipline** (Wails compliance): each `Handle` checks `ctx.Err()` at entry and inside every loop over conditions/updaters/retentions. Cancellation must reach the state loop so window-close stops work.
4. Tests build the state directly with mocks; a tiny `stubFactory` handles outbound transitions.
5. Factory builder method lives at the bottom of each state file.

Ctx-check helper, defined alongside `publish`:
```go
func ctxFailed(ctx context.Context, factory StateFactory, from StateName) Handler {
    if err := ctx.Err(); err != nil {
        return factory.Failed(from, err)
    }
    return nil
}
```

Use at the top of each `Handle` and inside loops:
```go
if next := ctxFailed(ctx, s.factory, Preparing); next != nil { return next, nil }
for i, c := range s.conditions {
    if next := ctxFailed(ctx, s.factory, Preparing); next != nil { return next, nil }
    if err := c.Check(ctx); err != nil { ... }
}
```

`publish` helper (defined alongside `PreparingState`):
```go
func publish(bus ports.EventBus, evt ports.Event) {
    if bus != nil {
        bus.Publish(evt)
    }
}
```

### Shared test helper (lives in `statemachine_test` files)

```go
type stubFactory struct{ last StateName }

func (s *stubFactory) Preparing() Handler                                                      { return &step{n: Preparing} }
func (s *stubFactory) Locking() Handler                                                        { s.last = Locking;   return &step{n: Locking} }
func (s *stubFactory) Running() Handler                                                        { s.last = Running;   return &step{n: Running} }
func (s *stubFactory) Exiting(_ string, _, _ *domain.Manifest) Handler                         { s.last = Exiting;   return &step{n: Exiting} }
func (s *stubFactory) Unlocking(_ string, _ error) Handler                                     { s.last = Unlocking; return &step{n: Unlocking} }
func (s *stubFactory) Failed(from StateName, _ error) Handler                                  { s.last = "Failed:" + from; return &step{n: Failed} }
```

(Use the `step` stub from Task 6 for return values.)

---

### Task 8: `PreparingState`

Maps from `molfar.go:Prepare()`.

- [ ] Test `internal/core/statemachine/preparing_test.go`:

```go
package statemachine_test

import (
    "context"
    "errors"
    "testing"

    "ritual/internal/core/ports"
    sm "ritual/internal/core/statemachine"
)

type fakeCond struct{ err error }
func (f fakeCond) Check(_ context.Context) error { return f.err }

type fakeUpd struct{ err error }
func (f fakeUpd) Run(_ context.Context) error { return f.err }

func TestPreparing_Happy(t *testing.T) {
    f := &stubFactory{}
    s := sm.NewPreparingState(
        []ports.ConditionService{fakeCond{}},
        []ports.UpdaterService{fakeUpd{}},
        nil, f,
    )
    next, err := s.Handle(context.Background())
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if next.Name() != sm.Locking {
        t.Fatalf("next = %v", next.Name())
    }
}

func TestPreparing_CondFail(t *testing.T) {
    f := &stubFactory{}
    s := sm.NewPreparingState(
        []ports.ConditionService{fakeCond{err: errors.New("bad")}},
        nil, nil, f,
    )
    next, _ := s.Handle(context.Background())
    if next.Name() != sm.Failed {
        t.Fatalf("next = %v", next.Name())
    }
    if f.last != "Failed:Preparing" {
        t.Fatalf("last = %q", f.last)
    }
}
```

- [ ] Implement `internal/core/statemachine/preparing.go`:

```go
package statemachine

import (
    "context"
    "fmt"

    "ritual/internal/core/ports"
)

type PreparingState struct {
    conditions []ports.ConditionService
    updaters   []ports.UpdaterService
    bus        ports.EventBus
    factory    StateFactory
}

func NewPreparingState(c []ports.ConditionService, u []ports.UpdaterService, bus ports.EventBus, f StateFactory) *PreparingState {
    return &PreparingState{conditions: c, updaters: u, bus: bus, factory: f}
}

func (*PreparingState) Name() StateName { return Preparing }

func (s *PreparingState) Handle(ctx context.Context) (Handler, error) {
    publish(s.bus, ports.StartInfo{Operation: "prepare"})
    for i, c := range s.conditions {
        if err := c.Check(ctx); err != nil {
            return s.factory.Failed(Preparing, fmt.Errorf("condition %d: %w", i, err)), nil
        }
    }
    for i, u := range s.updaters {
        if err := u.Run(ctx); err != nil {
            return s.factory.Failed(Preparing, fmt.Errorf("updater %d: %w", i, err)), nil
        }
    }
    publish(s.bus, ports.FinishInfo{Operation: "prepare"})
    return s.factory.Locking(), nil
}

func (f *factory) Preparing() Handler {
    return NewPreparingState(f.d.Conditions, f.d.Updaters, f.d.Bus, f)
}

// publish is the nil-safe helper shared by every state.
func publish(bus ports.EventBus, evt ports.Event) {
    if bus != nil {
        bus.Publish(evt)
    }
}
```

- [ ] Commit: `feat(statemachine): PreparingState`

---

### Task 9: `LockingState`

Maps from `molfar.go:getRemoteManifest`, `validateAndRetrieveManifest`, `acquireManifestLocks`.

Test scenarios:
- Happy → Running, `LockID` derived from manifest after save.
- `local.LockedBy != ""` or `remote.LockedBy != ""` → `Failed:Locking`.
- `SaveRemoteManifest` returns error → `Unlocking` (factory called with lockID + cause).

- [ ] Implement `internal/core/statemachine/locking.go`:

```go
package statemachine

import (
    "context"
    "errors"
    "fmt"
    "os"
    "time"

    "ritual/internal/config"
    "ritual/internal/core/ports"
)

type LockingState struct {
    librarian ports.LibrarianService
    bus       ports.EventBus
    factory   StateFactory
}

func NewLockingState(l ports.LibrarianService, bus ports.EventBus, f StateFactory) *LockingState {
    return &LockingState{librarian: l, bus: bus, factory: f}
}

func (*LockingState) Name() StateName { return Locking }

func (s *LockingState) Handle(ctx context.Context) (Handler, error) {
    publish(s.bus, ports.StartInfo{Operation: "lock"})

    local, err := s.librarian.GetLocalManifest(ctx)
    if err != nil {
        return s.factory.Failed(Locking, fmt.Errorf("get local: %w", err)), nil
    }
    remote, err := s.librarian.GetRemoteManifest(ctx)
    if err != nil {
        return s.factory.Failed(Locking, fmt.Errorf("get remote: %w", err)), nil
    }
    if local == nil || remote == nil {
        return s.factory.Failed(Locking, errors.New("nil manifest")), nil
    }
    if local.LockedBy != "" || remote.LockedBy != "" {
        return s.factory.Failed(Locking, errors.New("already locked")), nil
    }

    host, err := os.Hostname()
    if err != nil {
        return s.factory.Failed(Locking, fmt.Errorf("hostname: %w", err)), nil
    }
    lockID := fmt.Sprintf("%s%s%d", host, config.LockIDSeparator, time.Now().UnixNano())
    local.LockedBy, remote.LockedBy = lockID, lockID

    if err := s.librarian.SaveLocalManifest(ctx, local); err != nil {
        return s.factory.Failed(Locking, fmt.Errorf("save local: %w", err)), nil
    }
    if err := s.librarian.SaveRemoteManifest(ctx, remote); err != nil {
        return s.factory.Unlocking(lockID, fmt.Errorf("save remote: %w", err)), nil
    }
    publish(s.bus, ports.FinishInfo{Operation: "lock"})
    return s.factory.Running(), nil
}

func (f *factory) Locking() Handler {
    return NewLockingState(f.d.Librarian, f.d.Bus, f)
}
```

- [ ] Commit: `feat(statemachine): LockingState with rollback-via-Unlocking`

---

### Task 9b: `ServerRunner.Run` accepts `ctx` + graceful stop

Pre-req for `RunningState` to honor cancellation. Critical for OS-shutdown handling: ctx cancel must trigger a *graceful* JVM stop (Minecraft `stop` command), NOT a kill — kill = world corruption.

- [ ] Change `ServerRunner` interface in `internal/core/ports/ports.go`:
  ```go
  type ServerRunner interface {
      Run(ctx context.Context, server *domain.ServerRuntime) error
  }
  ```
- [ ] Update real impl `internal/adapters/server_runner.go`:
  ```go
  func (r *Runner) Run(ctx context.Context, server *domain.ServerRuntime) error {
      cmd := exec.Command(server.Command(), server.Args()...)
      stdin, err := cmd.StdinPipe()
      if err != nil { return err }
      if err := cmd.Start(); err != nil { return err }

      done := make(chan struct{})
      go func() {
          select {
          case <-ctx.Done():
              _, _ = io.WriteString(stdin, "stop\n")
              _ = stdin.Close()
              // Force-kill if graceful stop hangs.
              t := time.AfterFunc(config.GracefulStopTimeout, func() { _ = cmd.Process.Kill() })
              defer t.Stop()
              <-done
          case <-done:
          }
      }()
      err = cmd.Wait()
      close(done)
      return err
  }
  ```
- [ ] Add `config.GracefulStopTimeout` (default 30s) to `internal/config/config.go`.
- [ ] Update mock `internal/core/ports/mocks/server_runner.go`: signature update; existing tests propagate `ctx`.
- [ ] Add real-impl test `internal/adapters/server_runner_ctx_test.go`: launch a fake binary that prints to stdout and reads stdin; cancel ctx; assert it received "stop\n" before `Wait` returned. Second test: launch a fake that ignores stdin; cancel ctx; assert force-kill happens within `GracefulStopTimeout + 1s`.
- [ ] Verify no other callers exist: `rg "ServerRunner\b" --type go`. Expected: `molfar.go` (deleted in Phase 6) + `running.go` (Task 10) + tests/mocks. Nothing else.
- [ ] `go build ./... && go test ./...` → green.
- [ ] Commit: `feat(server_runner): ctx-driven graceful stop, force-kill fallback`

---

### Task 10: `RunningState`

Maps from `molfar.go:executeServer`. Always routes to `Exiting` (unlock guarantee). Snapshots manifests for `Exiting`'s backup decision.

- [ ] Implement `internal/core/statemachine/running.go`:

```go
package statemachine

import (
    "context"

    "ritual/internal/core/domain"
    "ritual/internal/core/ports"
)

type RunningState struct {
    server    *domain.ServerRuntime
    runner    ports.ServerRunner
    librarian ports.LibrarianService
    bus       ports.EventBus
    factory   StateFactory
}

func NewRunningState(
    server *domain.ServerRuntime,
    runner ports.ServerRunner,
    librarian ports.LibrarianService,
    bus ports.EventBus,
    f StateFactory,
) *RunningState {
    return &RunningState{server: server, runner: runner, librarian: librarian, bus: bus, factory: f}
}

func (*RunningState) Name() StateName { return Running }

func (s *RunningState) Handle(ctx context.Context) (Handler, error) {
    publish(s.bus, ports.StartInfo{Operation: "server"})

    localBefore, _ := s.librarian.GetLocalManifest(ctx)
    remoteBefore, _ := s.librarian.GetRemoteManifest(ctx)
    lockID := ""
    if localBefore != nil {
        lockID = localBefore.LockedBy
    }

    if err := s.runner.Run(ctx, s.server); err != nil {
        publish(s.bus, ports.ErrorInfo{Operation: "server", Err: err})
        return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
    }
    publish(s.bus, ports.FinishInfo{Operation: "server"})
    return s.factory.Exiting(lockID, localBefore, remoteBefore), nil
}

func (f *factory) Running() Handler {
    return NewRunningState(f.d.Server, f.d.ServerRunner, f.d.Librarian, f.d.Bus, f)
}
```

- [ ] Tests: ok → Exiting; runner err → Exiting (still). Both with valid `lockID` propagated.
- [ ] Commit: `feat(statemachine): RunningState always routes to Exiting`

---

### Task 11: `ExitingState`

Maps from `molfar.go:Exit + unlockManifests`.

- [ ] Struct fields: `exitUpdaters`, `retentions`, `localStore`, `remoteStore`, `librarian`, `bus`, `factory`. Ctor payload: `lockID`, `localBefore`, `remoteBefore`.
- [ ] Flow (flat, early-return):
  1. `publish(StartInfo{Operation: "exit"})`
  2. If `lockID == ""` → return `nil` (terminal success).
  3. `ctx := context.WithoutCancel(parentCtx)` — Exiting runs to completion regardless of upstream cancellation.
  4. **No `ctxFailed` calls in this state.** Exiting is the documented exception to the ctx-discipline rule.
  5. For each `exitUpdaters[i].Run(ctx)`: error → `factory.Failed(Exiting, ...)`.
  6. If `services.ShouldBackup(localBefore.Worlds.SyncState, remoteBefore.Worlds.SyncState)`: refetch local manifest; `services.CreateBackup(ctx, LocalStore, ...)`; `services.CreateBackup(ctx, RemoteStore, ...)`.
  7. For each `retentions[i].Apply(ctx)`: error → `factory.Failed(Exiting, ...)`.
  8. Unlock both manifests (idempotent — only if `LockedBy == lockID`); update `RitualVersion = config.AppVersion`.
  9. `publish(FinishInfo{Operation: "exit"})`. Return `nil`.
- [ ] Factory method:

```go
func (f *factory) Exiting(lockID string, localBefore, remoteBefore *domain.Manifest) Handler {
    return NewExitingState(
        f.d.ExitUpdaters, f.d.Retentions,
        f.d.LocalStore, f.d.RemoteStore, f.d.Librarian,
        f.d.Bus, f, lockID, localBefore, remoteBefore,
    )
}
```

- [ ] Tests: happy → nil; no-lock shortcut → nil; updater fail → Failed; backup fail → Failed; retention fail → Failed.
- [ ] Commit: `feat(statemachine): ExitingState (updaters + backup + retention + unlock)`

---

### Task 12: `UnlockingState`

- [ ] Struct: `librarian`, `bus`, `factory`; ctor payload `lockID`, `cause`.
- [ ] Handle: idempotent release of either manifest whose `LockedBy == lockID`. Save errors swallowed (best-effort rollback). Returns `factory.Failed(Locking, cause)`.

```go
package statemachine

import (
    "context"

    "ritual/internal/core/ports"
)

type UnlockingState struct {
    librarian ports.LibrarianService
    bus       ports.EventBus
    factory   StateFactory
    lockID    string
    cause     error
}

func NewUnlockingState(l ports.LibrarianService, bus ports.EventBus, f StateFactory, lockID string, cause error) *UnlockingState {
    return &UnlockingState{librarian: l, bus: bus, factory: f, lockID: lockID, cause: cause}
}

func (*UnlockingState) Name() StateName { return Unlocking }

func (s *UnlockingState) Handle(ctx context.Context) (Handler, error) {
    publish(s.bus, ports.StartInfo{Operation: "unlock-rollback"})

    if local, err := s.librarian.GetLocalManifest(ctx); err == nil && local != nil && local.LockedBy == s.lockID {
        local.Unlock()
        _ = s.librarian.SaveLocalManifest(ctx, local)
    }
    if remote, err := s.librarian.GetRemoteManifest(ctx); err == nil && remote != nil && remote.LockedBy == s.lockID {
        remote.Unlock()
        _ = s.librarian.SaveRemoteManifest(ctx, remote)
    }
    publish(s.bus, ports.FinishInfo{Operation: "unlock-rollback"})
    return s.factory.Failed(Locking, s.cause), nil
}

func (f *factory) Unlocking(lockID string, cause error) Handler {
    return NewUnlockingState(f.d.Librarian, f.d.Bus, f, lockID, cause)
}
```

- [ ] Tests: local-only locked, both locked, neither locked (no-op).
- [ ] Commit: `feat(statemachine): UnlockingState rollback`

---

### Task 13: `FailedState`

- [ ] Struct: `bus`, `runID`, `from StateName`, `err error`.
- [ ] Handle: publish `StateFailedInfo{State: string(from), RunID: runID, Err: err}`, return `(nil, err)`.

```go
package statemachine

import (
    "context"
    "errors"

    "ritual/internal/core/ports"
)

type FailedState struct {
    bus   ports.EventBus
    runID string
    from  StateName
    err   error
}

func NewFailedState(bus ports.EventBus, runID string, from StateName, err error) *FailedState {
    if err == nil {
        err = errors.New("failed without recorded error")
    }
    return &FailedState{bus: bus, runID: runID, from: from, err: err}
}

func (*FailedState) Name() StateName { return Failed }

func (s *FailedState) Handle(_ context.Context) (Handler, error) {
    publish(s.bus, ports.StateFailedInfo{State: string(s.from), RunID: s.runID, Err: s.err})
    return nil, s.err
}

func (f *factory) Failed(from StateName, err error) Handler {
    return NewFailedState(f.d.Bus, f.d.RunID, from, err)
}
```

- [ ] Test: `bus.Subscribe()`; drain channel; type-switch for `StateFailedInfo`; assert payload + returned error.
- [ ] Commit: `feat(statemachine): FailedState terminal`

---

### Task 14: Factory smoke

- [ ] `internal/core/statemachine/factory_test.go`: build `Deps` with mocks; call each builder method; assert non-nil + correct `Name()`.
- [ ] Commit: `test(statemachine): factory wiring smoke`

---

## Phase 4 — Service migration (legacy `chan` → `EventBus` + `Prompter`)

Path B: migrate every legacy callsite in this sprint. No bridge.

### Task 15: `PromptSettings` → `Prompter`

The hardest migration — `PromptEvent` round-trip extracted to RPC.

- [ ] Rewrite `internal/core/services/settings.go`:
  - Ctor: `func PromptSettings(bus ports.EventBus, prompter ports.Prompter, minRAMMB int) (*domain.Settings, error)`.
  - Replace every `ports.SendEvent(events, X)` with `bus.Publish(X)` using `*Info` types.
  - Replace `promptWithValidation`'s channel round-trip with `prompter.Prompt(ctx, prompt, prompt, defaultValue)`. Validation feedback emits `ports.UpdateInfo` via bus.
- [ ] Update `settings_test.go`:
  - Replace `make(chan ports.Event, N)` with `bus := adapters.NewEventBus(N); ch, cancel := bus.Subscribe(); defer cancel()`.
  - Add scripted `Prompter` stub:
    ```go
    type scriptedPrompter struct{ resp []string; i int }
    func (p *scriptedPrompter) Prompt(_ context.Context, _, _, _ string) (string, error) {
        r := p.resp[p.i]; p.i++; return r, nil
    }
    ```
- [ ] Commit: `refactor(settings): use EventBus + Prompter (drop PromptEvent)`

---

### Task 16: `R2Repository`

- [ ] In `internal/adapters/r2.go`:
  - Field `events chan<- ports.Event` → `bus ports.EventBus` (also in `progressReadCloser`).
  - Ctor params `events chan<- ports.Event` → `bus ports.EventBus`.
  - Helper `(r *R2Repository) send(evt)` → `(r *R2Repository) publish(evt)` calling `r.bus.Publish` (nil-safe).
  - Replace `*Event` literals with `*Info`.
- [ ] Update r2 tests to use `adapters.NewEventBus(N)`.
- [ ] Commit: `refactor(r2): EventBus instead of legacy chan`

---

### Task 17: Other services

- [ ] `internal/core/services/sync.go`: ctor + field swap; one `SendEvent` → `bus.Publish`.
- [ ] `internal/core/services/retention_logs.go`: same pattern.
- [ ] Update their tests in lockstep.
- [ ] Commit: `refactor(services): EventBus migration (sync, retention_logs)`

---

### Task 18: Delete `SendEvent` + `PromptEvent`

- [ ] Confirm no remaining references: `rg "SendEvent\(|PromptEvent"` → empty.
- [ ] (Already removed from `events.go` in Task 1; this task verifies and deletes any stragglers.)
- [ ] Commit: `chore(events): remove SendEvent helper and PromptEvent type`

---

## Phase 5 — Cutover

### Task 19: `cmd/cli/prompter.go`

- [ ] Create stdin `Prompter` impl:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "io"
    "strings"

    "ritual/internal/core/ports"
)

type stdinPrompter struct {
    in  *bufio.Reader
    out io.Writer
}

func newStdinPrompter(in io.Reader, out io.Writer) ports.Prompter {
    return &stdinPrompter{in: bufio.NewReader(in), out: out}
}

func (p *stdinPrompter) Prompt(_ context.Context, _, prompt, def string) (string, error) {
    if def != "" {
        fmt.Fprintf(p.out, "%s [%s]: ", prompt, def)
    } else {
        fmt.Fprintf(p.out, "%s: ", prompt)
    }
    line, err := p.in.ReadString('\n')
    if err != nil {
        return def, nil // existing behavior: fallback on read error
    }
    line = strings.TrimSpace(line)
    if line == "" {
        return def, nil
    }
    fmt.Fprintln(p.out, line)
    return line, nil
}
```

- [ ] Commit: `feat(cli): stdin Prompter implementation`

---

### Task 20: `cmd/cli/consumer.go`

- [ ] Update consumer signature to `<-chan ports.Event`. Switch on payload types:

```go
for evt := range events {
    switch e := evt.(type) {
    case ports.StartInfo:
        fmt.Fprintf(w, "[%s] [%s] Starting...\n", timestamp(), e.Operation)
    case ports.UpdateInfo:
        if pct, ok := e.Data["percent"]; ok {
            fmt.Fprintf(w, "[%s] [%s] %s (%.1f%%)\n", timestamp(), e.Operation, e.Message, pct)
            continue
        }
        fmt.Fprintf(w, "[%s] [%s] %s\n", timestamp(), e.Operation, e.Message)
    case ports.FinishInfo:
        fmt.Fprintf(w, "[%s] [%s] Completed\n", timestamp(), e.Operation)
    case ports.ErrorInfo:
        fmt.Fprintf(w, "[%s] [%s] ERROR: %v\n", timestamp(), e.Operation, e.Err)
    case ports.StateChangedInfo:
        fmt.Fprintf(w, "[%s] %s → %s\n", timestamp(), e.From, e.To)
    case ports.StateFailedInfo:
        fmt.Fprintf(w, "[%s] FAILED in %s: %v\n", timestamp(), e.State, e.Err)
    default:
        fmt.Fprintf(w, "[%s] %v\n", timestamp(), evt) // Stringer fallback
    }
}
```

- [ ] Delete `handlePrompt` (moved to `cmd/cli/prompter.go`).
- [ ] Commit: `refactor(cli): consumer reads payload types from EventBus`

---

### Task 21: `cmd/cli/main.go`

- [ ] Remove `events := make(chan ports.Event, 100)` and the `close(events)` lifecycle.
- [ ] Construct bus + prompter + subscriber goroutine:

```go
bus := adapters.NewEventBus(128)
prompter := newStdinPrompter(os.Stdin, os.Stdout)

busCh, cancelSub := bus.Subscribe()
defer cancelSub()
wg.Add(1)
go func() { defer wg.Done(); consumeEvents(busCh, logFile) }()
```

- [ ] Construct factory + machine:

```go
hostnameRun, _ := os.Hostname()
runID := fmt.Sprintf("%s-%d", hostnameRun, time.Now().UnixNano())

deps := statemachine.Deps{
    Bus:          bus,
    Prompter:     prompter,
    RunID:        runID,
    Server:       server,
    Librarian:    librarian,
    LocalStore:   localStorage,
    RemoteStore:  remoteStorage,
    ServerRunner: serverRunner,
    Conditions:   conditions,
    Updaters:     updaters,
    ExitUpdaters: exitUpdaters,
    Retentions:   retentions,
}
factory := statemachine.NewFactory(deps)
machine := statemachine.NewMachine(factory.Preparing(), bus, factory.RunID())

if err := machine.Run(context.Background()); err != nil {
    fmt.Printf("Ritual failed: %v\n", err)
    cancelSub()
    wg.Wait()
    return
}
cancelSub()
wg.Wait()
fmt.Println("Ritual completed successfully")
success = true
```

- [ ] Wherever `services.PromptSettings(events, ...)` was called: pass `bus, prompter` instead.
- [ ] `go build ./...` + `go test ./...` → all green.
- [ ] Commit: `feat(cli): wire EventBus + Prompter + state machine`

---

## Phase 6 — Remove Molfar

### Task 22: Delete files

- [ ] `rm internal/core/services/molfar.go internal/core/services/molfar_test.go`
- [ ] Remove `MolfarService` block from `internal/core/ports/ports.go`.
- [ ] `rm internal/core/ports/mocks/molfar.go internal/core/ports/mocks/molfar_test.go`
- [ ] `go build ./... && go test ./...` → green.
- [ ] Commit: `refactor: delete MolfarService (replaced by state machine)`

### Task 23: Verify no legacy chan callers

- [ ] `rg "chan<- ports.Event|chan ports.Event"` → empty (or only test fixtures).
- [ ] `rg "ports\.SendEvent|ports\.PromptEvent|ports\.StartEvent|ports\.UpdateEvent|ports\.FinishEvent|ports\.ErrorEvent"` → empty.
- [ ] Commit: `chore: verify legacy event channel removed`

---

## Phase 7 — Docs

### Task 24: Promote proposal → `docs/state-machine.md`

- [ ] `git mv docs/state-machine-proposal.md docs/state-machine.md`
- [ ] Edit:
  - Drop "Status: Memo — not for implementation yet".
  - Add "Implementation: `internal/core/statemachine/`".
  - Update design section to reflect ctor injection (no `MachineContext`), no Builder, typed-string `StateName`, no `DoneState`, no Enter/Exit hooks.
- [ ] Commit: `docs: promote state machine proposal to implemented design`

### Task 25: Rewrite `docs/event-architecture.md`

Current content describes sealed-interface + raw `chan<- Event`. Rewrite for:
- `type Event = fmt.Stringer`, payload `*Info` structs implementing `String()`.
- `EventBus` interface + variadic `Subscribe(filters ...Event)`.
- `Prompter` port — RPC, separate from events.
- `StateChangedInfo` / `StateFailedInfo` for state machine integration.
- Keep the high-level Event Flow diagram; update struct names.
- [ ] Commit: `docs(events): rewrite for Stringer events + EventBus + Prompter`

### Task 26: Update `docs/structure.md`

- [ ] Replace Molfar component section with:

```markdown
### State Machine (Orchestration)

Explicit lifecycle: `Preparing → Locking → Running → Exiting` (success: nil),
with `Unlocking` as rollback from `Locking`, and `Failed` as terminal error.
Each state is a small struct holding its own deps (constructor injection).
`StateFactory` owns the dependency graph and wires states at transition time;
transition payloads flow as factory args (compiler-checked).

### Event Bus

Events are `fmt.Stringer` values. `EventBus.Subscribe(filters ...Event)`
filters delivery by type prototype (no filter = receive all). Publish is
non-blocking; slow subscribers drop events.

### Prompter

User-input RPC, separate from the event bus (fan-out and request-response
are incompatible). One `Prompter` implementation per front-end (CLI / GUI).
```

- [ ] Add `core/statemachine/` to directory tree; mark Molfar removed.
- [ ] Commit: `docs(structure): replace Molfar with state machine + bus + prompter`

### Task 27: `docs/progress.md`

- [ ] Insert before `# >>> We are here`:

```markdown
### Sprint 6: State Machine + EventBus Migration (Completed)
- [x] EventBus port + adapter (non-blocking Publish, multi-subscriber fan-out; subscriber-side type-switch for filtering; Decorator extension point documented)
- [x] Prompter port (RPC extracted from events)
- [x] StateName + Handler interface (no hooks — brackets are state pairs)
- [x] Machine.Run loop with StateChangedInfo emission
- [x] StateFactory + Deps (ctor injection, no god-struct, no Builder)
- [x] States: Preparing, Locking, Running, Exiting, Unlocking, Failed
- [x] Service migrations: r2, sync, retention_logs, settings — all use EventBus
- [x] PromptSettings uses Prompter; legacy PromptEvent + SendEvent deleted
- [x] cmd/cli cutover — Molfar removed
- [x] Docs: state-machine.md, event-architecture.md, structure.md, progress.md
```

- [ ] Commit: `docs(progress): mark state machine + bus migration complete`

### Task 28: Sweep remaining docs

- [ ] `rg -l "StartEvent|UpdateEvent|FinishEvent|ErrorEvent|PromptEvent|SendEvent|sealed\(\)" docs/`
- [ ] Update each match to new naming. Common targets: `docs/coding-practices.md`, `docs/overview.md`, `docs/delta-sync-v2.md`, `docs/v2-foundation.md`.
- [ ] Commit: `docs: sweep references to legacy event types`

---

## Phase 8 — LOC verification

### Task 29: Net diff

- [ ] `git diff --stat origin/main...HEAD | tail -20`
- [ ] Record the *real* number. Target ≤ −600.
- [ ] Final empty commit:

```
chore: state machine + bus migration — LOC delta report

Net diff: <real number>
```

---

## Cross-Task Dependencies

This plan does not stand alone. The following sister tracks affect targets, ordering, and merge strategy.

### `docs/v2-foundation.md` (composition-root + GUI prep)

Parallel plan, **not yet executed**. Its "Post-v2" section explicitly schedules state-machine migration *after* its own P1–P9 land. Key impacts:

| v2-foundation step | Impact on this plan |
|---|---|
| P4 — new `cmd/gui/main.go` (≤30 LOC) replacing `cmd/cli/` | Phase 5 cutover targets the wrong file once v2 ships. |
| P9 — `rm -rf cmd/cli` | Our `cmd/cli/prompter.go`, `consumer.go` cease to exist. |
| Composition root in `internal/app/wire.go` | Inline wiring in `main.go` becomes a wire function `internal/app/wire_machine.go`. |
| Wails as GUI binding (post-v2 ticket) | `EventBus.Subscribe` + `Prompter` are the exact surface Wails will bind to. Document this here so the binding is trivial. |
| Fixture adapter pattern | Integration tests (F1–F5) should consume fixture adapters instead of hand-rolled mocks once available. |
| Logger DI cleanup | Our `*slog.Logger` injection aligns with v2's "no `func init()` doing silent work" rule. No conflict. |

### Recommended ordering

1. **v2-foundation lands first.** Its rewrite leaves a clean `internal/app/` composition root, fixture-adapter test plumbing, and a `cmd/gui/` entry point. State-machine work then plugs into a stable surface.
2. **State machine lands second.** Phase 5 (Cutover) targets `internal/app/wire_machine.go` + `cmd/gui/main.go` (instead of `cmd/cli/`).
3. **Wails GUI binding lands third.** Subscribes to `EventBus`; provides a `Prompter` impl that opens modal dialogs.

If state-machine must land **before** v2-foundation (interleaved schedule):
- Keep Phase 5 targeting `cmd/cli/` as written.
- Add a v2-foundation follow-up task: when v2 deletes `cmd/cli`, move `prompter.go` + state-machine wiring into `internal/app/`. v2 already plans to rewrite `consumer.go` and `main.go` from scratch — the migration is light.

### `docs/progress.md` sprint-numbering conflict

`progress.md` currently shows:
- Sprint 5 done (backup/retention).
- Sprint 6 *planned* = "Logging Integration".
- v2-foundation not listed at all.

This plan provisionally calls itself "Sprint 6" in Task 27. Reconcile before merging:

- [ ] Confirm sprint sequence with project owner. Recommended: v2-foundation = Sprint 6, state-machine = Sprint 7. The "Logging Integration" sprint is likely absorbed into v2-foundation's logger DI cleanup; verify and either fold or renumber.
- [ ] Update Task 27 sprint number once decided.
- [ ] Update `docs/progress.md` Sprint 6/7 ordering in same commit.

### Doc-touch overlap

Both plans modify:
- `docs/structure.md`
- `docs/progress.md`

State-machine renames `docs/state-machine-proposal.md` → `docs/state-machine.md`; v2-foundation references the proposal name. Coordinate the rename: either land state-machine first and let v2 see the new name, or update v2's references in the same PR that does the rename.

### File-target adjustments if state-machine lands first

| Target in this plan | Substitute target if v2 hasn't shipped yet |
|---|---|
| `cmd/cli/main.go` | unchanged |
| `cmd/cli/consumer.go` | unchanged |
| `cmd/cli/prompter.go` (new) | unchanged |

| Target in this plan | Substitute target if v2 has shipped first |
|---|---|
| `cmd/cli/main.go` | `cmd/gui/main.go` (≤30 LOC) — only `gui.Run(deps)` style call |
| `cmd/cli/consumer.go` | `internal/app/consumer.go` |
| `cmd/cli/prompter.go` | `internal/app/prompter_stdin.go` (later replaced by `prompter_wails.go` in GUI sprint) |
| Inline wiring block (Task 21) | `internal/app/wire_machine.go` — exposes `BuildMachine(deps app.Deps) (*statemachine.Machine, ports.EventBus, ports.Prompter)` |

### Hand-off notes for downstream Wails sprint

The state machine produces these surfaces that Wails should bind to directly:

- **`EventBus.Subscribe()`** — Wails goroutine ranges over the channel and calls `runtime.EventsEmit(ctx, name, payload)`.
- **`EventBus.Subscribe(StateChangedInfo{}, StateFailedInfo{})`** — drives the GUI screen router.
- **`Prompter` interface** — Wails provides a `wailsPrompter` whose `Prompt()` emits a `prompt-request` event with an ID, awaits the bound `app.AnswerPrompt(id, answer)` call, returns the answer.
- **`StateChangedInfo.To`** — primary screen-routing key. Frontend `switch state.to` over the typed-string constants.
- **State payload structs** — Wails frontend uses them as TypeScript types (auto-generated by `wails generate`).

### Wails compliance checklist (apply during this sprint, not deferred)

These items ensure the state machine doesn't lock out Wails when its sprint comes:

- [ ] **Honor `ctx.Done()` in every state.** Add `if err := ctx.Err(); err != nil { return s.factory.Failed(<self>, err), nil }` at the top of every `Handle` and inside every loop over `conditions`/`updaters`/`exitUpdaters`/`retentions`. Window-close cancellation must reach the state loop.
- [ ] **`ServerRunner.Run` must accept `context.Context`.** Today it takes `*ServerRuntime` only. Add `ctx` parameter; mock impls accept it; real impl uses `exec.CommandContext(ctx, ...)` so killing the JVM is wired to ctx cancel. Track this as a pre-req for the GUI sprint; do *not* skip it here.
- [ ] **`Prompter.Prompt` already takes `ctx`.** Verify implementations honor it.
- [ ] **No direct stdout/stderr writes in core.** All output via `bus.Publish` or `slog.Logger`. (Already enforced by hunt-list grep.)

### Wails JSON-serialization adapter (GUI sprint, documented now)

`ErrorInfo.Err` and `StateFailedInfo.Err` hold a Go `error` interface, which marshals to `{}` over Wails' JSON wire. Solution lives in the GUI sprint, not core:

```go
// internal/app/wails_emit.go (GUI sprint)
type errorInfoDTO struct{ Operation, Err string }
type stateFailedInfoDTO struct{ State, RunID, Err string }

func toDTO(evt ports.Event) any {
    switch e := evt.(type) {
    case ports.ErrorInfo:        return errorInfoDTO{e.Operation, e.Err.Error()}
    case ports.StateFailedInfo:  return stateFailedInfoDTO{e.State, e.RunID, e.Err.Error()}
    default:                     return e
    }
}
```

Wails goroutine: `runtime.EventsEmit(ctx, reflect.TypeOf(evt).Name(), toDTO(evt))`. Core stays JSON-ignorant.

### Wails app shell (documented now, written in GUI sprint)

```go
type App struct {
    ctx      context.Context
    bus      ports.EventBus
    prompter ports.Prompter
    deps     app.Deps
    running  atomic.Bool
}

func (a *App) OnStartup(ctx context.Context) {
    a.ctx = ctx
    a.bus = adapters.NewEventBus(128)
    a.prompter = newWailsPrompter(ctx, a)
    // … wire fixtures vs prod adapters via internal/app/wire.go
    go a.subscribeAndEmit()
}

func (a *App) StartSession() error {
    if !a.running.CompareAndSwap(false, true) {
        return errors.New("session already running")
    }
    runID := fmt.Sprintf("%d", time.Now().UnixNano())
    factory := statemachine.NewFactory(a.depsForRun(runID))
    machine := statemachine.NewMachine(factory.Preparing(), a.bus, runID)
    go func() {
        defer a.running.Store(false)
        _ = machine.Run(a.ctx) // error already in bus via StateFailedInfo
    }()
    return nil
}

func (a *App) AnswerPrompt(id, answer string) {
    if p, ok := a.prompter.(*wailsPrompter); ok {
        p.Resolve(id, answer)
    }
}
```

Bound methods `StartSession`, `AnswerPrompt` are the only frontend → backend RPCs needed.

---

## Acceptance Criteria

Each criterion has a binary pass/fail check. All must pass for the sprint to be considered done.

### Functional

| # | Criterion | Verification |
|---|---|---|
| F1 | A successful run transitions Preparing → Locking → Running → Exiting → terminal-success and the lock is released. | Integration test asserts `StateChangedInfo` sequence + final manifest `LockedBy == ""`. |
| F2 | A condition failure in Preparing produces a `Failed` terminal state, no lock is acquired. | Integration test with failing condition; assert `StateFailedInfo{State:"Preparing"}` + manifest unchanged. |
| F3 | A remote-save failure during Locking triggers Unlocking → Failed; no manifest stays locked. | Integration test with mock librarian failing `SaveRemoteManifest`; assert local + remote `LockedBy == ""`. |
| F4 | A server runner error still results in Exiting being entered (lock released), and the failure is published. | Integration test with mock runner returning err; assert lock released + `ErrorInfo` published. |
| F5 | `ShouldBackup` true → both local and r2 backups created in Exiting; false → neither. | Two integration tests; assert backup files present/absent. |
| F6 | `PromptSettings` collects user input via `Prompter` (no `PromptEvent`). | Test with `scriptedPrompter`; assert returned `Settings` matches script. |
| F7 | EventBus delivers events to subscribers and drops on slow subs (no producer block). | `eventbus_test.go` already covers — must pass. |
| F8 | `Subscribe()` delivers every published event to every subscriber. Bus performs no filtering — that's a subscriber concern (planned `adapters.WithTypes` / `WithFilter` Decorator when needed). | `TestEventBus_DeliversAllEventsToAllSubscribers`. |
| F9 | Cancel function on a subscription closes the channel exactly once and removes it from fan-out. | `TestEventBus_Cancel_Closes` + manual second-cancel call (idempotent). |
| F10 | Cancelling the run context mid-flight stops the state machine within one state boundary. **Exception:** `ExitingState` runs to completion regardless of cancellation. | Per-state test: cancel ctx before `Handle`; assert `Failed` with `ctx.Err()`. For Exiting: cancel ctx before `Handle`; assert it still completes successfully. |
| F11 | `ServerRunner.Run(ctx, server)` honors ctx cancellation by killing the JVM subprocess. | Real-impl test uses `exec.CommandContext`; assert process exits when ctx cancelled. (Pre-req for Wails — track in this sprint, not deferred.) |

### Non-functional

| # | Criterion | Verification |
|---|---|---|
| N1 | Net LOC delta vs `origin/main` is ≤ −500. | `git diff --stat origin/main...HEAD` |
| N2 | `go build ./...` passes after every commit (no broken intermediate state). | CI runs build per commit. |
| N3 | `go test ./...` passes after every commit. | CI runs tests per commit. |
| N4 | Zero references to legacy types remain after Phase 6. | `rg "MolfarService\|SendEvent\|PromptEvent\|StartEvent\|UpdateEvent\|FinishEvent\|ErrorEvent\|sealed\(\)"` returns empty (excluding docs that intentionally describe history). |
| N5 | Zero `chan<- ports.Event` or `chan ports.Event` field/param in `internal/`. | `rg "chan<- ports\.Event\|chan ports\.Event" internal/` returns empty. |
| N6 | No `else` branches in any new code under `internal/core/statemachine/`. | `rg "^\s*} else" internal/core/statemachine/` returns empty. |
| N7 | No nested `if` inside `if` (max one level of indentation per condition) in new state code. | Code review; `gocyclo` complexity ≤ 7 per `Handle`. |
| N8 | Reflection in `EventBus.Publish` is gated — zero `reflect.TypeOf` calls when no subscriber filters. | Benchmark `BenchmarkPublish_NoFilter` shows zero allocations. Optional bench task. |
| N9 | All public new symbols have doc comments. | `go vet ./...` + `revive` exported lint. |

---

## Testing Plan

### Unit tests (per-package, isolated)

| Package | Test | What it proves |
|---|---|---|
| `internal/core/ports` | `String()` smoke per `*Info` payload | Format strings stable |
| `internal/adapters` | `TestEventBus_Subscribe_Receives` | Basic delivery |
| `internal/adapters` | `TestEventBus_Cancel_Closes` | Lifecycle cleanup |
| `internal/adapters` | `TestEventBus_SlowSub_NoBlock` | Non-blocking publish under buffer pressure |
| `internal/adapters` | `TestEventBus_NoFilter_ReceivesAll` | Default = receive-all invariant |
| `internal/adapters` | `TestEventBus_Filter_DeliversOnlyRequestedTypes` | Variadic filter correctness |
| `internal/adapters` | `TestBridge*` | (deleted — bridge dropped in Path B) |
| `internal/core/statemachine` | `TestMachine_TransitionsAndEmitsChange` | Loop transitions; `StateChangedInfo` per hop incl. `To: "Done"` terminal |
| `internal/core/statemachine` | `TestMachine_PropagatesError` | Errors short-circuit the loop |
| `internal/core/statemachine` | `TestPreparing_Happy` / `_CondFail` / `_UpdaterFail` | Phase logic + Failed routing |
| `internal/core/statemachine` | `TestLocking_Happy` / `_AlreadyLocked` / `_RemoteSaveFails` | Lock acquire + Unlocking rollback |
| `internal/core/statemachine` | `TestRunning_OK` / `_Err` | Always routes to Exiting; payload includes lockID |
| `internal/core/statemachine` | `TestExiting_Happy` / `_NoLock` / `_BackupFails` / `_RetentionFails` | All branches |
| `internal/core/statemachine` | `TestUnlocking_LocalOnly` / `_Both` / `_Neither` | Idempotent rollback |
| `internal/core/statemachine` | `TestFailed_EmitsAndReturns` | StateFailedInfo + err |
| `internal/core/statemachine` | `TestFactory_BuildsAllStates` | Wiring smoke |
| `internal/core/services` (each migrated) | existing tests retargeted to bus + prompter mocks | Behavior preserved |

### Integration tests (cross-package, real bus + factory)

| Test | Setup | Assert |
|---|---|---|
| `TestE2E_HappyPath` | All deps mocked to return success | Full transition sequence; lock released; `nil` from `Run` |
| `TestE2E_PreparingFails` | Mock condition returns err | `StateFailedInfo{State:"Preparing"}`; no lock acquired |
| `TestE2E_LockingFails_RollsBack` | Mock librarian fails `SaveRemoteManifest` after `SaveLocalManifest` succeeds | Both manifests end with `LockedBy == ""`; `StateFailedInfo{State:"Locking"}` |
| `TestE2E_RunningFails_StillUnlocks` | Mock runner returns err | Lock released; ErrorInfo + StateChangedInfo to Exiting still emitted |
| `TestE2E_ExitingBackupFails` | Mock LocalStore.CreateBackup fails | `StateFailedInfo{State:"Exiting"}`; partial backup tolerated per existing semantics |

Integration tests live in `internal/core/statemachine/integration_test.go`; build a real factory with mock services; subscribe to the bus; assert on the full event sequence.

### Manual smoke (one-shot before merge)

- Run `cmd/cli` against a local mock R2 + temp world dir.
- Verify CLI output shows `start prepare` → `state-changed Preparing → Locking` → ... → `state-changed Exiting → Done`.
- Trigger a settings prompt (delete settings file); assert stdin RPC works (no events stuck waiting for response).

### What we are NOT testing in this sprint

- GUI rendering — out of scope (sprint deferred).
- Real R2 round-trip — covered by existing R2 tests retained as-is.
- Performance under high event throughput — see N8 if benchmark is added; otherwise not measured.

---

## Projection

### LOC delta (per-task estimate; revisit at LOC verification)

| Phase | Item | Δ LOC |
|---|---|---|
| 1 | Rewrite `events.go` (delete sealed + variants + SendEvent + PromptEvent; add `*Info` + Stringer) | −5 |
| 1 | Drop `events_test.go` SendEvent suite | −80 |
| 1 | Add `EventBus` port | +12 |
| 1 | Add `Prompter` port | +10 |
| 1 | Add `eventbus.go` adapter (publish + subscribe + cancel; no filter) | +55 |
| 1 | Add `eventbus_test.go` (4 tests) | +75 |
| 2 | `handler.go` (StateName + interface) | +25 |
| 2 | `machine.go` + `machine_test.go` | +90 |
| 2 | `factory.go` + smoke test | +75 |
| 3 | Six state files + their test files | +500 |
| 3 | `ctxFailed` helper + per-Handle entry checks + inner-loop checks | +25 |
| 3 | F10 ctx-cancel tests (one per state) | +60 |
| 3 | `ServerRunner.Run(ctx, ...)` ripple — port + adapter + mock + RunningState call site | +5 |
| 3 | F11 ctx-cancel kills JVM test (real adapter) | +30 |
| 4 | Settings ctor swap + drop PromptEvent plumbing | −20 |
| 4 | R2 + sync + retention_logs ctor swaps | wash |
| 4 | Test harness updates across services | wash |
| 5 | `prompter.go` stdin impl | +35 |
| 5 | Consumer rewrite (drop handlePrompt, add new event cases) | −15 |
| 5 | main.go (drop legacy chan, add bus + prompter + factory wiring) | +5 |
| 6 | Delete Molfar (file + test + mock) | −350 |
| 7 | Doc churn | wash |
| **Total (estimated)** | | **~−575** |

Target met (≥ −500). Initial −600 estimate; GUI-readiness scope added +120 (ctx discipline + ServerRunner ctx + per-state cancel tests); dropped Logger field −10; dropped variadic-filter machinery −45 (smaller adapter + smaller test). Trade-off accepted: Wails-ready core, simplest possible bus, no second migration when GUI lands. Revisit at Task 29 with actual `git diff --stat`.

### Risk register

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | Settings prompt RPC behaves differently than event-based round-trip (timing, nested cases) | Med | High (user-facing) | scripted Prompter unit tests + manual smoke before merge |
| R2 | `reflect.TypeOf(evt)` cost regresses publish throughput | Low | Low | Gated by `filteringSubs > 0`; benchmark optional (N8) |
| R3 | Hidden caller of `chan<- ports.Event` outside `internal/` | Low | Med | Acceptance N5 grep across whole repo, not just `internal/` |
| R4 | `errors.Is`/`fmt.Errorf` chain breakage when wrapping happens in factory.Failed | Low | Med | Test asserts `errors.Is(returned, original)` after `FailedState.Handle` |
| R5 | Race on subscription map when goroutines subscribe/cancel concurrently | Med | High | `sync.RWMutex` pattern is std; covered by `-race` in CI |
| R6 | Event ordering across subscribers diverges (one sub falls behind) | High by design | Low | Documented; per-sub FIFO preserved; cross-sub interleaving accepted |
| R7 | Doc rewrite drift — old `StartEvent` references linger in undocumented places | Med | Low | Sweep task (28) + grep |
| R8 | LOC undershoot — net diff > −500 | Med | Low | Identified follow-up: drop `Logger` field where unused; further service trim; consolidate `*Info` String formatters. Track for Sprint 8 if relevant. |
| R11 | `ServerRunner.Run(ctx, ...)` signature change breaks an unforeseen caller | Low | Med | Pre-merge grep `rg "ServerRunner\b"`; today's only callers are Molfar (deleted in Phase 6) and the new RunningState. |
| R9 | Test flakes from `time.After` deadlines on slow CI | Med | Med | Use 200ms+ deadlines; deterministic where possible |
| R10 | A subagent partially completes a task and commits a broken intermediate | Med | Med | Per-task `go build ./...` + `go test ./...` before commit; subagent prompt enforces |

### Schedule projection (rough)

| Phase | Tasks | Effort |
|---|---|---|
| 1 — Ports | 1–4 | ~½ day |
| 2 — Machine core | 5–7 | ~½ day |
| 3 — States | 8–14 | ~1 day (six TDD states) |
| 4 — Service migration | 15–18 | ~½ day |
| 5 — Cutover | 19–21 | ~½ day |
| 6 — Delete Molfar | 22–23 | ~½ hr |
| 7 — Docs | 24–28 | ~½ day |
| 8 — LOC verify | 29 | ~10 min |
| **Total** | 29 tasks | **~3.5 days** |

Estimate assumes one focused engineer or one disciplined subagent loop. Add 50 % buffer for review checkpoints.

---

## Projection Meeting (mid-sprint checkpoint)

After Phase 3 (state machine compiles + all unit tests green) and before Phase 4 (service migration), pause for a checkpoint:

- [ ] Run all tests. Confirm green.
- [ ] `git diff --stat origin/main...HEAD` — report current delta.
- [ ] Re-validate LOC projection: do the actual deltas in Phases 1–3 match the projection? If +20 % over budget, identify cuts before continuing into the high-blast-radius Phase 4.
- [ ] Re-validate risk register: any risk realised? Any new risk surfaced?
- [ ] Re-confirm acceptance criteria are still achievable as written.
- [ ] If scope creep detected: stop, surface the cut/defer decision, do not proceed to Phase 4.

Output a 1-paragraph status report:
- Delta so far vs projection.
- Risks observed.
- Decision: continue / adjust / abort.

Commit empty: `chore: mid-sprint checkpoint — Phase 3 complete, Phase 4 cleared`.

---

## Re-Review Hunt List

After Phase 8 (LOC verification), before declaring the sprint done, hunt for these specific patterns. Anything found = a follow-up commit, not a "ship it":

### Code

- [ ] `rg "interface\{\s*\}" internal/` — empty `interface{}` (likely a leftover from the Stringer migration).
- [ ] `rg "MachineContext"` — should be empty (god-struct gone).
- [ ] `rg "Builder" internal/core/statemachine/` — should be empty (no Builder).
- [ ] `rg "Enterer\|Exiter\|StateEnteredEvent" internal/` — should be empty (hooks dropped).
- [ ] `rg "DoneState"` — should be empty (nil = success).
- [ ] `rg "} else " internal/core/statemachine/` — should be empty (no `else`).
- [ ] `rg "if .* {\s*if " internal/core/statemachine/` — should be empty (no nested ifs).
- [ ] `rg "panic\(" internal/core/statemachine/` — should be empty (no panics in new code).
- [ ] Every `Handle` calls `ctxFailed(ctx, ...)` at entry. `rg -L "ctxFailed\(ctx" internal/core/statemachine/*.go` — files missing the check are listed; should be empty (excluding `exiting.go`, `failed.go`, `handler.go`, `factory.go`, `machine.go`, `*_test.go`).
- [ ] `rg "ctxFailed" internal/core/statemachine/exiting.go` — should be empty.
- [ ] `rg "context.WithoutCancel" internal/core/statemachine/exiting.go` — should match exactly one occurrence.
- [ ] `ServerRunner.Run` signature includes `ctx context.Context`. `rg "Run\(.*\*domain\.ServerRuntime" internal/core/ports/` — should be empty (old signature gone).
- [ ] `rg "TODO\|FIXME\|XXX" internal/core/statemachine/` — should be empty.
- [ ] `rg "log\.Print\|fmt\.Print" internal/core/statemachine/` — should be empty (states must use `bus`/`logger`, not direct stdout).
- [ ] `rg "\.\(ports\.\w+Event\)" internal/ cmd/` — should be empty (legacy Event-typed assertions).
- [ ] `rg "responseChan" internal/ cmd/` — should be empty (PromptEvent gone).
- [ ] `rg "Subscribe\(.+\)" internal/ cmd/` — should be empty (only no-arg `Subscribe()`; variadic-filter not implemented).
- [ ] `rg "WithTypes\|WithFilter" internal/adapters/` — should be empty (Decorator deferred until a real consumer asks).
- [ ] `rg "Operation:\s*\"" internal/core/statemachine/` — every state should publish StartInfo / FinishInfo with consistent `Operation` strings.
- [ ] All state structs are unexported except for the type itself? (i.e., struct fields lowercase). Spot-check.

### Tests

- [ ] Every state file has a `_test.go` with at least: happy path + one failure path.
- [ ] Every state test uses the shared `step` + `stubFactory` helpers — no per-test factory variant.
- [ ] No test uses `time.Sleep` (use `time.After` with `select`).
- [ ] Integration tests in `integration_test.go` cover all five F1–F5 acceptance scenarios.
- [ ] `go test -race ./...` passes (catches subscription concurrency bugs).

### Docs

- [ ] `docs/state-machine.md` describes ctor injection (no MachineContext), no Builder, typed StateName, no hooks.
- [ ] `docs/event-architecture.md` describes Stringer events, EventBus, Prompter — no sealed-interface holdover.
- [ ] `docs/structure.md` lists `core/statemachine/` and `Prompter` port; Molfar absent.
- [ ] `docs/progress.md` Sprint 6 entry checked off.
- [ ] `docs/event-architecture.md`'s old sealed-interface code blocks all replaced.

### Build / CI

- [ ] `go build ./...` clean.
- [ ] `go vet ./...` clean.
- [ ] `go test ./...` clean.
- [ ] `go test -race ./...` clean.
- [ ] `git status` clean (no stray files).
- [ ] No commit titled "WIP" or "temp" in the sprint range.

---

## Self-Review

Canonical content lives in earlier sections; this is the cross-check.

**Pattern coverage** (decisions ↔ implementing tasks)

| Design decision | Implementing task(s) |
|---|---|
| Stringer events (open set, self-describing) | 1 |
| EventBus (fan-out, no bus filter, Decorator extension point) | 2, 4 |
| Prompter port (RPC split from events) | 3, 15, 19 |
| State pattern + Handler | 5 |
| Machine loop + StateChangedInfo emission | 6 |
| Abstract Factory + Deps | 7 |
| Constructor injection (no MachineContext) | 8–13 |
| `StateName` typed string | 5 |
| No `DoneState` (nil = success) | 6 |
| No Enter/Exit hooks | 5 |
| `ServerRunner.Run(ctx, ...)` ctx propagation | 9b |
| Rollback via Unlocking | 9, 12 |
| Service migrations (no bridge) | 15, 16, 17, 18 |
| Cutover wiring | 19, 20, 21 |
| Delete Molfar | 22, 23 |
| Docs sweep | 24–28 |
| LOC verification | 29 |

**Deferred / Rejected:** see Architecture decisions section (top of doc).

**LOC accounting:** see Projection section (single source of truth: ~−575 net, target ≥ −500).

**Invariants:**
- `Handle` signature uniform: `(ctx) → (Handler, error)`.
- Terminal success = nil `Handler` (Machine emits synthetic `To: "Done"` event).
- Terminal failure = `FailedState` returning `(nil, err)`.
- Only `Locking` acquires lock; only `Exiting` and `Unlocking` release. All release paths idempotent (`LockedBy == lockID` check).
- `Running` always transitions to `Exiting` (unlock guarantee).
- State fields immutable post-construction (no mutable god-struct).
- Factory is the only non-test code that knows how to build states.
- `EventBus.Subscribe()` delivers every event to every subscriber. No bus-side filtering. Subscribers discriminate via `switch evt.(type)`.
- `EventBus.Publish` is non-blocking and drops on slow subscribers.
- `Prompter` is the only path for user input; events never carry response channels.
- Every `Handle` checks `ctx.Err()` at entry (and inside loops over conditions/updaters/retentions). **Exception:** `ExitingState` does not — it uses `context.WithoutCancel(parentCtx)` and runs to completion.

**Flat-code invariants** (per memory `feedback_flat_code`):
- No `else` branches in any state's `Handle`.
- No nested `if` inside `if`. Guard clauses at top, early returns.

**Placeholder scan:** zero TBD; every task has code or exact command.

---

Plan saved to `docs/superpowers/plans/2026-04-15-state-machine.md`. Execution:

1. **Subagent-driven** (recommended) — fresh subagent per task, review between.
2. **Inline** — single session with checkpoints.

Which?
