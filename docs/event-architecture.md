# Event Architecture Refactor

## Goal

Replace Logger DI with Event channel to enable UI-agnostic state machine (CLI/TUI/HTTP/any provider).

## Event Interface

```go
// internal/core/ports/events.go

type Event interface{ sealed() }

type StartEvent struct {
    Operation string // "prepare", "run", "exit", "backup", "download", "updater", "retention"
}

type UpdateEvent struct {
    Operation string
    Message   string         // human-readable
    Data      map[string]any // structured (optional, nil if just log)
}

type FinishEvent struct {
    Operation string
}

type ErrorEvent struct {
    Operation string
    Err       error
}

func (StartEvent) sealed()  {}
func (UpdateEvent) sealed() {}
func (FinishEvent) sealed() {}
func (ErrorEvent) sealed()  {}
```

## Implementation Steps

### Phase 1: Create Event System

- [ ] Create `internal/core/ports/events.go` with event types
- [ ] Remove `Logger` interface from `ports/ports.go`

### Phase 2: Update Services

Replace `logger ports.Logger` with `events chan<- Event` in:

- [ ] `MolfarService` - main orchestrator
- [ ] `WorldsUpdater` - world download/extract
- [ ] `LocalRetention` - local backup cleanup
- [ ] `R2Retention` - R2 backup cleanup
- [ ] `R2Repository` - upload/download progress
- [ ] `S3Uploader` - upload progress

### Phase 3: Update Adapters

- [ ] Remove `internal/adapters/logger.go` (SlogLogger, NopLogger)
- [ ] Update `R2Repository` constructor - remove logger param
- [ ] Update `S3Uploader` constructor - remove logger param
- [ ] Update progress readers to emit events

### Phase 4: Create Event Consumers

- [ ] Create CLI consumer in `cmd/cli/` - prints to stdout
- [ ] (Future) TUI consumer
- [ ] (Future) HTTP consumer

### Phase 5: Update main.go

- [ ] Create buffered event channel
- [ ] Start consumer goroutine
- [ ] Pass channel to services
- [ ] Handle channel close on exit

### Phase 6: Update Tests

- [ ] Create test helper for event collection
- [ ] Update all tests using NopLogger to use event channel
- [ ] Add event assertion helpers

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                 Event Consumers                      │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐             │
│  │   CLI   │  │   TUI   │  │  HTTP   │             │
│  │ Consumer│  │ Consumer│  │ Consumer│             │
│  └────▲────┘  └────▲────┘  └────▲────┘             │
│       │            │            │                   │
│       └────────────┼────────────┘                   │
│                    │                                │
│              chan Event                             │
└────────────────────┼────────────────────────────────┘
                     │
┌────────────────────┼────────────────────────────────┐
│                    │        Services                │
│  ┌─────────────────▼─────────────────────────────┐ │
│  │              MolfarService                     │ │
│  │  events <- StartEvent{Operation: "prepare"}   │ │
│  └───────────────────────────────────────────────┘ │
│                                                     │
│  ┌─────────────┐ ┌─────────────┐ ┌──────────────┐ │
│  │WorldsUpdater│ │  Retention  │ │ R2Repository │ │
│  └─────────────┘ └─────────────┘ └──────────────┘ │
└─────────────────────────────────────────────────────┘
```

## Event Flow Example

```
StartEvent{Operation: "prepare"}
  StartEvent{Operation: "updater"}
    UpdateEvent{Operation: "updater", Message: "Checking instance version"}
  FinishEvent{Operation: "updater"}
  StartEvent{Operation: "updater"}
    UpdateEvent{Operation: "updater", Message: "Downloading world"}
    UpdateEvent{Operation: "download", Data: {"percent": 50.0}}
  FinishEvent{Operation: "updater"}
FinishEvent{Operation: "prepare"}

StartEvent{Operation: "run"}
  UpdateEvent{Operation: "run", Message: "Server started", Data: {"address": "0.0.0.0:25565"}}
  ... server runs ...
FinishEvent{Operation: "run"}

StartEvent{Operation: "exit"}
  StartEvent{Operation: "backup"}
    UpdateEvent{Operation: "upload", Data: {"percent": 75.0}}
  FinishEvent{Operation: "backup"}
  StartEvent{Operation: "retention"}
  FinishEvent{Operation: "retention"}
FinishEvent{Operation: "exit"}
```

## CLI Consumer Example

```go
func consumeEvents(events <-chan ports.Event) {
    for evt := range events {
        switch e := evt.(type) {
        case ports.StartEvent:
            fmt.Printf("[START] %s\n", e.Operation)
        case ports.UpdateEvent:
            if e.Data != nil {
                fmt.Printf("[UPDATE] %s: %s %v\n", e.Operation, e.Message, e.Data)
            } else {
                fmt.Printf("[UPDATE] %s: %s\n", e.Operation, e.Message)
            }
        case ports.FinishEvent:
            fmt.Printf("[FINISH] %s\n", e.Operation)
        case ports.ErrorEvent:
            fmt.Printf("[ERROR] %s: %v\n", e.Operation, e.Err)
        }
    }
}
```

## Test Helper Example

```go
func collectEvents(fn func(chan<- ports.Event)) []ports.Event {
    events := make(chan ports.Event, 100)
    fn(events)
    close(events)

    var collected []ports.Event
    for evt := range events {
        collected = append(collected, evt)
    }
    return collected
}

// Usage in test
events := collectEvents(func(ch chan<- ports.Event) {
    service.Prepare(ch)
})
assert.Contains(t, events, ports.StartEvent{Operation: "prepare"})
```

## Migration Notes

- Services receive `events chan<- Event` (send-only)
- Consumers receive `<-chan Event` (receive-only)
- Use buffered channel to prevent blocking: `make(chan Event, 100)`
- Always close channel when done to signal consumers
- Nil channel is valid - services should check before sending
