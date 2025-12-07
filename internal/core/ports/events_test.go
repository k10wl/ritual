package ports_test

import (
	"errors"
	"testing"

	"ritual/internal/core/ports"

	"github.com/stretchr/testify/assert"
)

func TestSendEvent(t *testing.T) {
	t.Run("sends event to channel", func(t *testing.T) {
		events := make(chan ports.Event, 10)

		ports.SendEvent(events, ports.StartEvent{Operation: "test"})
		ports.SendEvent(events, ports.UpdateEvent{Operation: "test", Message: "progress"})
		ports.SendEvent(events, ports.FinishEvent{Operation: "test"})

		close(events)

		var received []ports.Event
		for evt := range events {
			received = append(received, evt)
		}

		assert.Len(t, received, 3)
	})

	t.Run("nil channel is safe", func(t *testing.T) {
		// Should not panic
		ports.SendEvent(nil, ports.StartEvent{Operation: "test"})
		ports.SendEvent(nil, ports.ErrorEvent{Operation: "test", Err: errors.New("fail")})
	})

	t.Run("all event types work", func(t *testing.T) {
		events := make(chan ports.Event, 10)

		ports.SendEvent(events, ports.StartEvent{Operation: "op"})
		ports.SendEvent(events, ports.UpdateEvent{Operation: "op", Message: "msg", Data: map[string]any{"key": "value"}})
		ports.SendEvent(events, ports.FinishEvent{Operation: "op"})
		ports.SendEvent(events, ports.ErrorEvent{Operation: "op", Err: errors.New("error")})

		close(events)

		var count int
		for range events {
			count++
		}
		assert.Equal(t, 4, count)
	})
}
