# Event Architecture

Authoritative reference for how observability flows through the Ritual engine. Outlives individual sprints.

**Related plans:**
- [`superpowers/plans/2026-04-15-state-machine.md`](./superpowers/plans/2026-04-15-state-machine.md) — migrates legacy `chan<- ports.Event` to `ports.EventBus`.
- [`superpowers/plans/2026-04-16-manifest-store.md`](./superpowers/plans/2026-04-16-manifest-store.md) — prerequisite, deletes `LibrarianService`.
- [`superpowers/plans/2026-04-16-retry-coverage.md`](./superpowers/plans/2026-04-16-retry-coverage.md) — prerequisite, adds inline retry to R2 and introduces `RetryAttemptInfo` event.

---

## Topology: one bus, fan-out, non-blocking

```
                         ┌────────────────────┐
                         │  ports.EventBus    │  single instance, built in main
                         │  fan-out, lossy    │
                         └─────────┬──────────┘
                                   │
       ┌────────────────┬──────────┴──────────┬─────────────────┐
       ▼                ▼                     ▼                 ▼
   CLI log sub    GUI sub (future)      Metrics (future)   Retry log (opt.)

Publishers (anyone holding the bus):
  • State machine   → StateChangedInfo, StateFailedInfo
  • R2 adapter      → UploadProgress, DownloadProgress, RetryAttemptInfo
  • Services        → StartInfo, UpdateInfo, FinishInfo, ErrorInfo
  • Retention etc.  → domain events
```

### Properties

- **Single instance.** One `bus` variable, constructed in `main`, threaded through `Deps`. Do not construct per-component buses.
- **Fan-out.** Every `Subscribe()` call returns an independent channel. Each published event is delivered to every live subscriber.
- **Non-blocking publish.** If a subscriber's buffer is full, the event is dropped **for that subscriber only**. Publishers never stall.
- **Per-subscriber FIFO.** Order preserved within a subscription. Cross-subscriber interleaving undefined.
- **Observability-grade, not audit-grade.** Drops are acceptable for live rendering. For durable needs, attach a synchronous subscriber that writes to disk (not a second bus).
- **In-process.** No IPC today. Headless-engine topology would replace the adapter (not the interface) with an SSE/WebSocket-backed one.

### Port

```go
// internal/core/ports/eventbus.go
package ports

type EventBus interface {
    Publish(evt Event)
    Subscribe() (<-chan Event, func()) // channel + cancel
}
```

Cancel func closes the channel and removes the subscription. Idempotent.

---

## Event contract

```go
type Event = fmt.Stringer
```

**Open set.** Any package may define a new event struct. No central registry. Constraint: every event implements `String() string` so default consumers (logs, `%v`, slog) can render it without reflection.

### Conventions

- Struct name ends in `Info` (e.g. `StartInfo`, `RetryAttemptInfo`).
- Required fields: `Operation string` (what was happening) and, where applicable, `RunID string` (correlation).
- `String()` should be greppable and stable — that's the default log format.
- Use `UpdateInfo{Operation, Message, Data}` for generic progress text; define a new type only when you have unique structured fields a consumer needs to switch on.
- Publish at natural boundaries (start/finish/retry/error) — not on every loop tick.
- Throttle high-frequency publishes at the call site (progress per 1%, not per byte).

### Catalog (today + incoming)

| Type | Published by | Purpose |
|---|---|---|
| `StartInfo{Operation, RunID}` | services, states | an operation began |
| `UpdateInfo{Operation, Message, Data}` | services, adapters | progress / informational update |
| `FinishInfo{Operation}` | services, states | operation completed successfully |
| `ErrorInfo{Operation, Err}` | everywhere | operation failed (non-retryable or exhausted) |
| `StateChangedInfo{From, To, RunID}` | `Machine` core | state transition (state-machine sprint) |
| `StateFailedInfo{State, RunID, Err}` | `Machine` core | transition returned error |
| `RetryAttemptInfo{Operation, Key, Attempt, Err}` | R2 adapter | transient failure, retrying (retry-coverage sprint) |
| `UploadProgress{...}`, `DownloadProgress{...}` | R2 adapter | byte-level progress during object transfer |

Payload structs grow additively. New consumers must tolerate extra fields via `switch evt.(type)` default case.

---

## Subscriber patterns

Subscribers filter in their own loop:

```go
ch, cancel := bus.Subscribe()
defer cancel()
for evt := range ch {
    switch e := evt.(type) {
    case ports.ErrorInfo:
        handleErr(e)
    case ports.StateChangedInfo:
        route(e.To)
    case ports.RetryAttemptInfo:
        logRetry(e)
    default:
        // Stringer fallback — covers anything we don't switch on
        fmt.Fprintln(logFile, evt)
    }
}
```

For type-based or predicate filtering, wrap the bus with a decorator (added when a real consumer asks):

```go
errsOnly, _ := adapters.WithTypes(bus, ports.ErrorInfo{}).Subscribe()
recent,   _ := adapters.WithFilter(bus, func(e ports.Event) bool { ... }).Subscribe()
```

Decorators keep the `EventBus` interface unchanged.

### Typical subscribers

| Subscriber | Filters | Purpose |
|---|---|---|
| CLI log file | nothing (default case prints all) | durable record for debugging |
| CLI stdout | curated types (Start/Finish/Error + Progress) | human-visible run output |
| GUI progress widget | `UpdateInfo`, `ProgressInfo` | N-of-M counters, phase labels |
| GUI diagnostics panel | `RetryAttemptInfo`, `ErrorInfo` | advanced view |
| Metrics (future) | all, count by type+op | Prometheus / OTel |

---

## What does NOT go on the bus

These are explicitly kept off and have their own mechanisms:

### User-input RPC → `ports.Prompter`

Synchronous request/response (e.g. "Server won't start, edit config and press Enter"). Fan-out would race the response across subscribers. See `Prompter` port introduced in the state-machine plan.

```go
// Correct
answer, err := prompter.Prompt(ctx, id, "Edit config?", "yes")

// Wrong — removed in state-machine sprint
bus.Publish(PromptEvent{ResponseChan: ch}) // races across subs
```

### Commands (GUI → engine) → deferred `Command` port

User actions (Start, Stop, Retry) must reach exactly one handler with ack. Not observability. Separate Command port added when the GUI sprint needs it.

### Durable audit / compliance logging → synchronous subscriber, not a second bus

If reliability is required, attach a subscriber that writes synchronously (fsync before return). Other subscribers unaffected. Bus contract stays identical.

### Cross-process / network transport → replace adapter, not interface

If a headless-engine topology arrives, replace the in-memory adapter with SSE/WebSocket. `ports.EventBus` interface unchanged.

---

## When to split into multiple buses

**Default: don't.** One bus per process. Split only when all three are true:

1. **Different delivery semantics.** E.g. one event class requires at-least-once, another is fire-and-forget. Today everything is best-effort.
2. **Problem is not solvable with decorators or subscriber discipline.** `WithTypes`, `WithFilter`, throttling at publish site almost always suffice.
3. **The cost of split is recovered.** Two buses means two wirings, two subscriber sets, two debugging paths. Must earn it.

None of these criteria are met today. Revisit if proven necessary.

---

## Publisher rules

1. **One bus per process.** No `worldsBus`, `syncBus`, `retryBus`.
2. **Publishers do not filter.** Emit everything interesting; subscribers decide what to render.
3. **Events self-describe.** Every payload carries `Operation` / `RunID` / `Key` / whatever identifies it — a subscriber never has to guess "who emitted this".
4. **Publishers never block.** Bus publish is non-blocking by contract; publishers never wait on subscribers.
5. **No logging side channel.** The bus is the observability path. Do not also `log.Printf` from services — log subscriber on the bus is the single sink. Adapter-level SDK loggers (e.g. AWS SDK internals) stay scoped to their adapter.

---

## Subscriber rules

1. **Own your buffer.** Typical 64–128 per subscriber. Slow subscribers drop events — acceptable by design.
2. **Default case in type switch.** Print via `Stringer` fallback so new event types don't silently vanish.
3. **Cancel on exit.** Use the returned `cancel` func to prevent goroutine leaks.
4. **Never re-publish.** Subscribers must not feed their output back into the bus (loops).
5. **No blocking I/O in the subscriber goroutine for fast sinks.** Async batch or fsync in a dedicated goroutine if durability is needed.

---

## Event Flow — example run

```
StartInfo{Operation: "prepare"}
  StartInfo{Operation: "updater"}
    UpdateInfo{Operation: "updater", Message: "Checking version"}
    RetryAttemptInfo{Operation: "r2.Get", Key: "manifest.json", Attempt: 2, Err: "conn reset"}
    UpdateInfo{Operation: "updater", Message: "Downloading binary"}
  FinishInfo{Operation: "updater"}
FinishInfo{Operation: "prepare"}

StateChangedInfo{From: "Preparing", To: "Locking", RunID: "run-1234"}
StateChangedInfo{From: "Locking",   To: "Running", RunID: "run-1234"}

StartInfo{Operation: "run"}
  UpdateInfo{Operation: "run", Message: "Server started", Data: {"address": "0.0.0.0:25565"}}
  ...
FinishInfo{Operation: "run"}

StateChangedInfo{From: "Running", To: "Exiting", RunID: "run-1234"}

StartInfo{Operation: "exit"}
  StartInfo{Operation: "backup"}
    UploadProgress{Key: "backups/…/world.dat", Percent: 50}
    RetryAttemptInfo{Operation: "r2.Put", Key: "backups/…/world.dat", Attempt: 2, Err: "5xx"}
    UploadProgress{Key: "backups/…/world.dat", Percent: 50}   // retry restarts progress
    UploadProgress{Key: "backups/…/world.dat", Percent: 100}
  FinishInfo{Operation: "backup"}
  StartInfo{Operation: "retention"}
  FinishInfo{Operation: "retention"}
FinishInfo{Operation: "exit"}

StateChangedInfo{From: "Exiting", To: "Done", RunID: "run-1234"}
```

Retries are visible in the log but invisible to the user-facing progress widget (which reads only `UploadProgress.Percent`, not retry events). Advanced diagnostics subscribers see the full picture.

---

## Migration status

| Item | State |
|---|---|
| Legacy `chan<- ports.Event` model | **Being removed.** All call sites migrate to `EventBus` in the state-machine sprint. |
| Sealed-interface event types (`sealed()`) | **Being removed.** Replaced by `Event = fmt.Stringer` with `*Info` payload structs. |
| `PromptEvent` / `SendEvent` / `handlePrompt` | **Being removed.** Replaced by `Prompter` port. |
| `RetryAttemptInfo` | **Being added** in retry-coverage sprint; channel-compatible today, bus-native after state-machine sprint. |

See the linked plan files for concrete file-by-file changes.

---

## Test helpers

```go
// internal/adapters/eventbustest/eventbustest.go (introduced in state-machine sprint)

// Drain reads the bus until it's quiet for `quiet` duration or ctx expires.
// Returns all events observed. Use in integration tests to assert event order.
func Drain(ctx context.Context, bus ports.EventBus, quiet time.Duration) []ports.Event

// AssertContains checks that the event slice contains an event matching pred.
func AssertContains(t *testing.T, events []ports.Event, pred func(ports.Event) bool)
```

For unit tests, subscribe directly:

```go
ch, cancel := bus.Subscribe()
defer cancel()
service.DoThing(ctx)
evt := <-ch
if _, ok := evt.(ports.FinishInfo); !ok { t.Fatalf("got %T", evt) }
```

---

## Summary

- **One bus.** Fan-out, non-blocking, in-process.
- **`Event = fmt.Stringer`.** Open set; new types need no plumbing.
- **Publishers emit, subscribers filter.** Bus is dumb middleware.
- **RPC and commands are NOT events** — separate ports (`Prompter`, future `Command`).
- **Durability, cross-process, filtering** — solve with subscribers/decorators, not new buses.
- **Retry is an event, not a log line** — `RetryAttemptInfo` on the same bus as progress/state.
