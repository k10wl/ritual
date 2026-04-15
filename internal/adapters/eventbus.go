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

// NewEventBus returns a non-blocking fan-out bus. bufLen < 1 defaults to 64.
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
