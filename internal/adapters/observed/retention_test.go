package observed_test

import (
	"context"
	"errors"
	"ritual/internal/adapters/observed"
	"ritual/internal/core/ports"
	"sync"
	"testing"
	"time"
)

type retentionFunc func(ctx context.Context) ([]string, error)

func (f retentionFunc) Select(ctx context.Context) ([]string, error) { return f(ctx) }

type capturingBus struct {
	mu     sync.Mutex
	events []ports.Event
}

func (b *capturingBus) Publish(e ports.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *capturingBus) Subscribe() (<-chan ports.Event, func()) { return nil, func() {} }

func (b *capturingBus) events_() []ports.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ports.Event(nil), b.events...)
}

func TestObservedRetention_PublishesMarkedKeysPerSelect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	inner := retentionFunc(func(_ context.Context) ([]string, error) {
		return []string{"refs/20260414160000.json", "refs/20260413160000.json"}, nil
	})
	bus := &capturingBus{}

	obs := observed.NewRetention(inner, bus, "refs-local")
	got, err := obs.Select(ctx)
	if err != nil {
		t.Fatalf("decorator must forward Select err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("decorator must forward keys verbatim; got %v", got)
	}

	events := bus.events_()
	if len(events) != 1 {
		t.Fatalf("one event per Select call; got %d", len(events))
	}
	evt, ok := events[0].(observed.RetentionSelectInfo)
	if !ok {
		t.Fatalf("event must be RetentionSelectInfo; got %T", events[0])
	}
	if evt.Label != "refs-local" {
		t.Errorf("label must be propagated; got %q", evt.Label)
	}
	if evt.Count != 2 {
		t.Errorf("count must reflect key length; got %d", evt.Count)
	}
}

func TestObservedRetention_OnInnerError_StillPublishesEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	boom := errors.New("boom")
	inner := retentionFunc(func(_ context.Context) ([]string, error) { return nil, boom })
	bus := &capturingBus{}

	obs := observed.NewRetention(inner, bus, "refs-remote")
	_, err := obs.Select(ctx)

	if !errors.Is(err, boom) {
		t.Errorf("decorator must forward inner error verbatim; got %v", err)
	}
	if len(bus.events_()) != 1 {
		t.Errorf("error must still publish an event so failures are observable; got %d", len(bus.events_()))
	}
}

func TestObservedRetention_NilBus_NoPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	inner := retentionFunc(func(_ context.Context) ([]string, error) { return nil, nil })

	obs := observed.NewRetention(inner, nil, "refs-local")
	_, err := obs.Select(ctx)
	if err != nil {
		t.Errorf("nil bus must not alter behaviour; got %v", err)
	}
}
