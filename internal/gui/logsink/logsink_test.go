package logsink_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/ritual"
	"ritual/internal/gui/logsink"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu    sync.Mutex
	lines []logsink.LogLine
}

func (r *recorder) Emit(l logsink.LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, l)
}

func (r *recorder) snapshot() []logsink.LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]logsink.LogLine, len(r.lines))
	copy(out, r.lines)
	return out
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestLogsink_ForwardsEventStringAsMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	sink := logsink.New(bus, rec)
	done := make(chan struct{})
	go func() { sink.Run(ctx); close(done) }()

	bus.Publish(ritual.StartInfo{Operation: "acquire"})
	waitFor(t, func() bool { return len(rec.snapshot()) >= 1 }, "first log line")
	cancel()
	<-done

	lines := rec.snapshot()
	require.GreaterOrEqual(t, len(lines), 1, "logsink must forward every bus event as a LogLine — the logs window depends on it")
	assert.Equal(t, "start acquire", lines[0].Msg, "LogLine.Msg must equal Event.String() verbatim so the log console matches the internal event representation")
	assert.Equal(t, logsink.LevelInfo, lines[0].Level, "StartInfo must be classified as info — it is a normal lifecycle marker, not a warning")
	assert.NotZero(t, lines[0].Ts, "LogLine.Ts must be a non-zero unix millisecond timestamp so the UI can render a time column")
}

func TestLogsink_ErrorInfo_TaggedError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	sink := logsink.New(bus, rec)
	done := make(chan struct{})
	go func() { sink.Run(ctx); close(done) }()

	bus.Publish(ritual.ErrorInfo{Operation: "fetch", Err: errors.New("disk full")})
	waitFor(t, func() bool { return len(rec.snapshot()) >= 1 }, "one log line")
	cancel()
	<-done

	lines := rec.snapshot()
	require.Len(t, lines, 1, "exactly one event was published — exactly one log line must be forwarded")
	assert.Equal(t, logsink.LevelError, lines[0].Level, "ErrorInfo must be tagged LevelError so the log console renders it red — users should notice failures immediately")
}

