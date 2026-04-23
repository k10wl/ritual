package adapters

import (
	"ritual/internal/core/ports"
	"sync"
)

type eventBus struct {
	mu     sync.RWMutex
	subs   map[int]chan ports.Event
	next   int
	bufLen int
	block  bool
}

// NewEventBus returns a non-blocking fan-out bus. Slow subscribers see
// dropped events when their channel buffer fills — production policy: never
// stall the producer on a slow GUI/log sink. bufLen < 1 defaults to 64.
func NewEventBus(bufLen int) ports.EventBus {
	if bufLen < 1 {
		bufLen = 64
	}
	return &eventBus{subs: map[int]chan ports.Event{}, bufLen: bufLen}
}

// NewBlockingEventBus returns a fan-out bus that blocks Publish until every
// subscriber accepts the event. Test-only: tests assert exact event sequences
// and silent drops under scheduler pressure (-race + parallel suites) make
// integration assertions flaky. Production must keep NewEventBus's drop
// semantics — the engine cannot afford to wait on a slow GUI subscriber.
func NewBlockingEventBus(bufLen int) ports.EventBus {
	if bufLen < 1 {
		bufLen = 64
	}
	return &eventBus{subs: map[int]chan ports.Event{}, bufLen: bufLen, block: true}
}

func (b *eventBus) Publish(evt ports.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		if b.block {
			ch <- evt
			continue
		}
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
