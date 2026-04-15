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

func TestEventBus_CancelTwice_Safe(t *testing.T) {
	bus := adapters.NewEventBus(8)
	_, cancel := bus.Subscribe()
	cancel()
	cancel() // must not panic
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
