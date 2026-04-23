// Package logsink subscribes to the event bus and forwards every event
// as a LogLine to a window-scoped emitter. cmd/gui wires the emitter to
// the logs Wails window (logsWindow.EmitEvent) so the log console is the
// only consumer — the main window never sees this traffic.
package logsink

import (
	"context"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"time"
)

// Level is the coarse severity the frontend uses to color log lines.
// Stable strings: the TypeScript log viewer matches on these.
type Level string

// Level values.
const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// LogLine is one entry rendered in the log console.
type LogLine struct {
	Ts    int64  `json:"ts"`
	Level Level  `json:"level"`
	Msg   string `json:"msg"`
}

// Emitter is the window-scoped sink. cmd/gui implements it with a
// logsWindow.EmitEvent call; tests wire a slice accumulator.
type Emitter interface {
	Emit(line LogLine)
}

// Sink subscribes to bus and forwards events as LogLines.
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
			s.emitter.Emit(LogLine{
				Ts:    s.now().UnixMilli(),
				Level: deriveLevel(evt),
				Msg:   evt.String(),
			})
		}
	}
}

func deriveLevel(evt ports.Event) Level {
	switch evt.(type) {
	case ritual.ErrorInfo, ritual.StateFailedInfo, ritual.LockLostInfo:
		return LevelError
	}
	return LevelInfo
}
