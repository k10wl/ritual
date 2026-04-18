// Package ports defines hexagonal ports — interfaces implemented by adapters.
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
//
//	ch, cancel := bus.Subscribe()
//	defer cancel()
//	for evt := range ch {
//	    switch e := evt.(type) {
//	    case ritual.ErrorInfo:        handleErr(e)
//	    case ritual.StateChangedInfo: route(e.To)
//	    }
//	}
//
// For type or predicate filtering, wrap the bus with a Decorator:
//
//	errs, _ := adapters.WithTypes(bus, ritual.ErrorInfo{}).Subscribe()
//	ops,  _ := adapters.WithFilter(bus, func(e ports.Event) bool { ... }).Subscribe()
//
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
