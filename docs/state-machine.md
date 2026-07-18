# State Machine

## Status: Implemented in `internal/core/statemachine/`

This document describes the Ritual state machine as it ships on `feat/delta-sync`.
Previously labelled a "proposal"; now the authoritative design reference.

Implementation landmarks:
- `internal/core/statemachine/handler.go` — `StateName` typed string, `Handler` interface, 6 state constants
- `internal/core/statemachine/machine.go` — `Machine.Run` loop, emits `StateChangedInfo` on every transition
- `internal/core/statemachine/factory.go` — `StateFactory` + `Deps`, shared `publish` and `ctxFailed` helpers
- Per-state files + tests: `preparing.go`, `locking.go`, `running.go`, `exiting.go`, `unlocking.go`, `failed.go`

Design deltas from the original proposal:
- **No `MachineContext` god-struct.** Each state carries only its own deps via constructor injection.
- **No `DoneState`.** Terminal success = `Handle` returns `(nil, nil)`.
- **No `Enter`/`Exit` hooks.** Brackets that span phases are modeled as state pairs (`Locking`↔`Unlocking`/`Exiting`); brackets within a phase use `defer`.
- **`ExitingState` and `UnlockingState` use `context.WithoutCancel`.** Lock release must complete even if the upstream ctx is cancelled — stranded locks are worse than a slow shutdown.
- **No Memento / crash recovery.** Manifest lock semantics already cover the crash scenario.

---

## Current Architecture

Linear pipeline: `Prepare → Run → Exit`. Each phase is a method on MolfarService.
No explicit state enum. No crash recovery. No phase persistence.

Works for CLI. Breaks for GUI — GUI needs to show current state, handle crashes
gracefully, and provide retry/recovery UX.

---

## Proposed State Machine (GoF State Pattern)

```
┌─────────┐
│  Idle    │
└────┬─────┘
     │ Start()
     ▼
┌──────────┐  fail   ┌───────────┐
│Validating├────────►│  Failed   │
└────┬─────┘         └─────┬─────┘
     │ pass                │ Retry()/Reset()
     ▼                     │
┌──────────┐               │
│Preparing ├───fail────────┤
└────┬─────┘               │
     │ done                │
     ▼                     │
┌──────────┐               │
│ Locking  ├───fail────────┤
└────┬─────┘               │
     │ acquired            │
     ▼                     │
┌──────────┐               │
│ Running  ├───crash───────┤
└────┬─────┘               │
     │ Stop()              │
     ▼                     │
┌──────────┐               │
│ Exiting  ├───fail────────┘
└────┬─────┘
     │ synced
     ▼
┌──────────┐
│CoolDown  │ (60s cancel window from issue #13)
└────┬─────┘
     │ timeout/confirm
     ▼
┌──────────┐
│  Idle    │
└──────────┘
```

---

## GoF Patterns Involved

### State Pattern (core)

Each state is a concrete type implementing a State interface.

```go
type State interface {
    Enter(ctx *SessionContext) error
    Exit(ctx *SessionContext) error
    Handle(ctx *SessionContext, event Event) (State, error)
    Name() StateName
}

type SessionContext struct {
    current   State
    manifest  *Manifest
    lockID    string
    eventBus  EventBus
    persister StatePersister
}
```

Context delegates to current State. State transitions happen via `Handle()` returning
a new State. Context calls `Exit()` on old state, persists new state name, calls
`Enter()` on new state, emits `StateChanged` event.

### Observer Pattern (EventBus)

Publish-subscribe inside one process. State machine emits events, GUI subscribes.
No coupling between producer and consumer.

```go
type EventBus interface {
    Emit(event Event)
    Subscribe(handler func(Event))
}
```

Current event channel (single `chan ports.Event`) is 70% of this. Changes needed:
- Wrap channel in EventBus struct with `Subscribe()` for multiple consumers
- Add state transition events alongside activity events
- Separate command channel for GUI → engine (user actions)
- Typed subscriptions (subscribe to specific event types)

### Memento Pattern (StatePersister)

Crash recovery. Persist state name + minimal context to disk.

```go
type StateSnapshot struct {
    State     StateName
    LockID    string
    Timestamp time.Time
}
```

Process dies mid-Exit → restart reads snapshot → enters ExitingState directly →
resumes backup/unlock. No restart from Idle.

---

## Concrete States

```go
type IdleState struct{}
type ValidatingState struct{}
type PreparingState struct{}
type LockingState struct{}
type RunningState struct{}
type ExitingState struct{}
type CoolDownState struct{}
type FailedState struct {
    Reason    error
    FromState StateName  // enables targeted retry
}
```

`FailedState.FromState` is key — retry knows which state to re-enter.
Not blind restart from Idle.

---

## Gaps That Must Be Closed Before Implementation

These were identified during delta sync v2 design:

| Gap | Required for state machine | Status after delta sync v2 |
|---|---|---|
| Explicit state enum | Yes | Not yet — add when GUI work starts |
| Crash recovery | Yes | Partially addressed — sync folder survives crashes |
| Phase persistence | Yes | Not yet — state not persisted to disk |
| Retry logic for transient failures | Yes | Done — per-file retry in SyncService |
| Stale lock detection | Yes | Not yet — locks still orphan on crash |
| EventBus with multiple subscribers | Yes | Not yet — single channel consumer |
| Command channel (GUI → engine) | Yes | Not yet |

---

## Why Not Implement Now

Delta sync v2 is the priority. State machine adds complexity without immediate value
in CLI-only context. The linear pipeline (Prepare → Run → Exit) maps cleanly to
SyncService.Download → ServerRunner.Run → SyncService.Upload.

State machine becomes load-bearing when:
1. GUI needs to display current state
2. Users need retry/recovery UX
3. Multiple operations can be in-flight (future)

Until then — keep the pipeline, design for future state machine compatibility by:
- Keeping phases isolated (each phase is a potential State)
- Emitting events at phase boundaries (future StateChanged events)
- Not coupling SyncService to Molfar internals
