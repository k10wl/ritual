package logsink_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/gui/logsink"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu    sync.Mutex
	lines []logsink.ServerLog
}

func (r *recorder) Emit(l logsink.ServerLog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, l)
}

func (r *recorder) snapshot() []logsink.ServerLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]logsink.ServerLog, len(r.lines))
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

// The allowlist is the whole point of design-log/042: only MC console events
// reach the GUI console; engine internals (sync/storage/lifecycle) stay off the
// wire (they still land on disk via internal/subsystems/logging).
func TestLogsink_AllowlistsOnlyServerConsole(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(64)
	rec := &recorder{}
	sink := logsink.New(bus, rec)
	done := make(chan struct{})
	go func() { sink.Run(ctx); close(done) }()

	// Mixed stream: 2 server output lines + a crash + an echo, interleaved
	// with non-console noise that must be filtered out.
	bus.Publish(ritual.StartInfo{Operation: "acquire"})                     // noise
	bus.Publish(running.ServerOutputInfo{Line: "Done (5.2s)!"})             // keep (out)
	bus.Publish(ritual.ErrorInfo{Operation: "fetch", Err: errors.New("x")}) // noise
	bus.Publish(running.ConsoleEchoInfo{Text: "time set day"})              // keep (in)
	bus.Publish(running.ServerReadyInfo{})                                  // noise
	bus.Publish(running.ServerOutputInfo{Line: "k10wl joined"})             // keep (out)
	bus.Publish(running.ServerCrashedInfo{Err: errors.New("boom")})         // keep (error)

	waitFor(t, func() bool { return len(rec.snapshot()) >= 4 }, "four console lines")
	cancel()
	<-done

	lines := rec.snapshot()
	require.Len(t, lines, 4, "only the four MC console events must pass the allowlist; the three engine events must be dropped")

	assert.Equal(t, "out", lines[0].Kind)
	assert.Equal(t, "Done (5.2s)!", lines[0].Text)
	assert.Equal(t, logsink.Level(""), lines[0].Level, "normal output carries no level — severity is derived frontend-side")
	assert.NotZero(t, lines[0].Ts, "every line needs a unix-milli timestamp")

	assert.Equal(t, "in", lines[1].Kind, "an echoed command is an input row")
	assert.Equal(t, "time set day", lines[1].Text)

	assert.Equal(t, "out", lines[2].Kind)
	assert.Equal(t, "k10wl joined", lines[2].Text)

	assert.Equal(t, "out", lines[3].Kind)
	assert.Equal(t, logsink.LevelError, lines[3].Level, "a crash is backend-flagged error — it is not a parseable MC line")
	assert.Contains(t, lines[3].Text, "boom")
}

// Pure noise must produce zero emits — the console stays empty when only
// engine telemetry flows.
func TestLogsink_NonConsoleEventsProduceNoEmits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	bus := adapters.NewEventBus(16)
	rec := &recorder{}
	sink := logsink.New(bus, rec)
	done := make(chan struct{})
	go func() { sink.Run(ctx); close(done) }()

	bus.Publish(ritual.StartInfo{Operation: "acquire"})
	bus.Publish(running.ServerStartingInfo{})
	bus.Publish(running.ServerStoppingInfo{})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Empty(t, rec.snapshot(), "non-console events must never reach the GUI console (they still land on disk)")
}
