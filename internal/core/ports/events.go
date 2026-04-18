package ports

import "fmt"

// Event is any fmt.Stringer. Open set, self-describing, compile-safe.
//
// Concrete event types live next to the package that emits them — e.g.
// server lifecycle in core/stages/running, sync in core/sync, lock/state
// in core/ritual, retry/readiness in adapters. See ports.EventBus for
// subscription mechanics.
//
// Conventions:
//   - Use ritual.UpdateInfo{Operation, Message, Data} for generic progress;
//     only define a new type when you have unique structured fields.
//   - Throttle high-frequency publishes at the call site — slow subscribers
//     drop, and console floods are unfriendly.
//   - Namespace event names if defined outside core (e.g. gui.ScreenChangedInfo).
//   - Per-subscriber FIFO is preserved; cross-subscriber order is not.
//   - Bus delivery is non-blocking and observability-grade. For durable record
//     (audit, billing), attach a file-writing subscriber — out of scope here,
//     trivial when needed.
type Event = fmt.Stringer
