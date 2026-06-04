// Package logsink subscribes to the event bus and forwards the Minecraft
// server console stream — and only that — as ServerLogs to a window-scoped
// emitter. cmd/gui wires the emitter to the logs Wails window so the log
// console is the only consumer; the main window never sees this traffic.
//
// Scope (design-log/042): the GUI console is a product surface — it shows the
// MC server, not the engine's internal sync/storage/update telemetry. The full
// bus stream still lands on disk via internal/subsystems/logging; this filter
// is GUI-only.
package logsink

import (
	"context"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/running"
	"time"
)

// Level is the coarse severity the frontend uses to color a console line.
// Normal output carries "" (severity is derived frontend-side from MC's own
// /WARN]|/ERROR] tags); only a backend-flagged crash carries LevelError, since
// a crash is not a parseable MC line (design-log/042 §Q7).
type Level string

// LevelError flags a crash line. Empty Level = normal output.
const LevelError Level = "error"

// ServerLog is one line of the Minecraft server console.
type ServerLog struct {
	Ts    int64  `json:"ts"`
	Kind  string `json:"kind"`  // "out" server output | "in" echoed command
	Level Level  `json:"level"` // "" normal | "error" crash (backend-flagged)
	Text  string `json:"text"`
}

// ServerLogBatch is a coalesced flush of console lines (design-log/006 batching
// applied to the narrow 042 stream). Dropped counts lines the emitter ring shed
// on overflow since the previous flush.
type ServerLogBatch struct {
	Lines   []ServerLog `json:"lines"`
	Dropped int         `json:"dropped"`
}

// Emitter is the window-scoped sink. cmd/gui implements it with a batching
// ring over logsWindow.EmitEvent; tests wire a slice accumulator.
type Emitter interface {
	Emit(line ServerLog)
}

// Sink subscribes to bus and forwards MC console events as ServerLogs.
type Sink struct {
	ch      <-chan ports.Event
	unsub   func()
	emitter Emitter
	now     func() time.Time
}

// New subscribes to bus immediately and returns a Sink ready for Run.
// Subscribing here (not in Run) avoids the race where callers publish
// before the Run goroutine manages to attach.
func New(bus ports.EventBus, emitter Emitter) *Sink {
	ch, unsub := bus.Subscribe()
	return &Sink{ch: ch, unsub: unsub, emitter: emitter, now: time.Now}
}

// Run blocks until ctx is cancelled or the bus closes.
func (s *Sink) Run(ctx context.Context) {
	defer s.unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-s.ch:
			if !ok {
				return
			}
			line, ok := serverLine(s.now(), evt)
			if !ok {
				continue // not MC console — the on-disk file sink still records it
			}
			s.emitter.Emit(line)
		}
	}
}

// serverLine maps the allowlisted MC console events to a ServerLog. Everything
// else (sync, storage, update, lifecycle) returns ok=false and stays off the
// wire (design-log/042 §Q1).
func serverLine(now time.Time, evt ports.Event) (ServerLog, bool) {
	ts := now.UnixMilli()
	switch e := evt.(type) {
	case running.ServerOutputInfo:
		return ServerLog{Ts: ts, Kind: "out", Text: e.Line}, true
	case running.ServerCrashedInfo:
		return ServerLog{Ts: ts, Kind: "out", Level: LevelError, Text: e.String()}, true
	case running.ConsoleEchoInfo:
		return ServerLog{Ts: ts, Kind: "in", Text: e.Text}, true
	}
	return ServerLog{}, false
}
