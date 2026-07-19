// Package notify projects the critical run-lifecycle transitions onto native
// OS notifications (design-log/047). It is a pure bus consumer — no Wails, no
// window state — so it unit-tests against a fake Notifier and the composition
// root supplies the real platform adapter.
//
// Three criticals are surfaced (user directive — nothing else):
//
//   - running.ServerReadyInfo        → "Server started" (the server is up and
//     accepting connections; the only flow that boots a server).
//   - lifecycle.StatusChanged{Done}  → "Server stopped", but ONLY when the
//     server actually started this run (the sawReady latch). This keeps the
//     server-free flows (Download/Upload/Restore/Revert/RetentionApply) silent
//     on their own clean completion.
//   - lifecycle.StatusChanged{Failed} → "Run failed" + the error in the body,
//     for ANY flow (confirmed Q3: any failure stage is critical).
//
// ritual.FlowStartedInfo resets the per-run latch and bumps a run sequence so
// every toast carries a unique, clockless id (test-friendly — no Date.now).
package notify

import (
	"context"
	"fmt"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
)

// Notifier sends one OS notification. id de-duplicates / replaces on platforms
// that key by id; title is required by the platform layer; body may be empty.
type Notifier interface {
	Notify(id, title, body string) error
}

// Attach subscribes a goroutine that translates the critical lifecycle events
// into OS notifications until ctx is cancelled. Returns an idempotent stop func
// that cancels the subscription and waits for the consumer to drain.
//
// Each Notify is dispatched on a detached goroutine so a slow OS call (the
// Windows COM/registry toast path) can never back up the bus drain — same
// "never block the producer" stance as the Wails view emitter.
func Attach(ctx context.Context, bus ports.EventBus, n Notifier) func() {
	ch, cancel := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var sawReady bool
		var runSeq int
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				switch ev := e.(type) {
				case ritual.FlowStartedInfo:
					sawReady = false
					runSeq++
				case running.ServerReadyInfo:
					sawReady = true
					send(n, "ready", runSeq, "Server started", "")
				case lifecycle.StatusChanged:
					switch ev.Status {
					case lifecycle.Done:
						if sawReady {
							send(n, "stopped", runSeq, "Server stopped", "")
						}
					case lifecycle.Failed:
						send(n, "failed", runSeq, "Run failed", failBody(ev.Err))
					default:
						// Idle, Running, Dismissed don't trigger notifications.
					}
				}
			}
		}
	}()
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
}

// send dispatches one notification off the consumer goroutine. Title is the
// variant display name ("Ritual" or "Ritual Dev") so a dev-variant toast is
// visibly distinct from a prod one in the action center; the human-facing
// line lives in the body so the toast reads "Ritual / Server started".
// Errors are best-effort — a dropped toast is never load-bearing.
func send(n Notifier, kind string, runSeq int, line, detail string) {
	id := fmt.Sprintf("ritual-%s-%d", kind, runSeq)
	body := line
	if detail != "" {
		body = line + ": " + detail
	}
	go func() { _ = n.Notify(id, config.DisplayName(), body) }()
}

func failBody(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
