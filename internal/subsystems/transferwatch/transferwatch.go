// Package transferwatch marks a progress gate active for exactly the two
// byte-flowing windows of a ritual run — Pulling[download] and Pushing — so
// progress.Ticker emits heartbeat pulses while bytes are in-flight even if the
// link stalls (an R2 PutStream blocked on a TCP retransmit; 31s silences
// observed). Pulling's apply phase and every other stage are local work, so the
// gate disarms and idle silence is preserved. See design-log/022 #2.
package transferwatch

import (
	"context"

	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
)

// Gate is the subset of *progress.Ticker this watcher drives. Kept minimal so
// the dependency points at behaviour, not the concrete ticker.
type Gate interface {
	SetTransferActive(active bool)
}

// Watch subscribes to the bus on construction (not in Run) so events published
// before the Run goroutine attaches are not missed — mirrors projection.New.
type Watch struct {
	ch    <-chan ports.Event
	unsub func()
	gate  Gate
}

// New subscribes to bus immediately and returns a Watch ready for Run.
func New(bus ports.EventBus, gate Gate) *Watch {
	ch, unsub := bus.Subscribe()
	return &Watch{ch: ch, unsub: unsub, gate: gate}
}

// Run blocks until ctx is cancelled or the bus closes, toggling the gate as
// stage transitions cross into and out of the wire-transfer windows.
func (w *Watch) Run(ctx context.Context) {
	defer w.unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-w.ch:
			if !ok {
				return
			}
			switch e := evt.(type) {
			case ritual.StateChangedInfo:
				// Pulling[download] and Pushing are the only stages that move
				// bytes over the wire. Entering either arms the heartbeat;
				// every other transition disarms it.
				w.gate.SetTransferActive(e.To == ritual.StagePulling || e.To == ritual.StagePushing)
			case pulling.ApplyStartedInfo:
				// Network phase of Pulling is done; apply is local disk work, so
				// disarm — an apply pause must not masquerade as a wire stall.
				w.gate.SetTransferActive(false)
			}
		}
	}
}
