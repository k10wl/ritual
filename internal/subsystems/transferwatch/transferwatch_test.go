package transferwatch_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/transferwatch"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGate struct {
	mu  sync.Mutex
	seq []bool
}

func (g *fakeGate) SetTransferActive(a bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq = append(g.seq, a)
}

func (g *fakeGate) calls() []bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]bool(nil), g.seq...)
}

// TestWatch_ArmsOnlyForByteFlowingWindows locks design-log/022 #2: the
// heartbeat gate must be active for exactly Pulling[download] and Pushing —
// the two stages that move bytes over the wire — and inactive for every other
// beat, including Pulling's apply phase (local disk work). Without this the
// ticker either never pulses during a stall (gate stuck false) or spams idle
// stages with heartbeat ticks (gate stuck true), breaking
// TestTicker_StableCounters_NoTicks's contract at the system level.
func TestWatch_ArmsOnlyForByteFlowingWindows(t *testing.T) {
	bus := adapters.NewEventBus(64)
	gate := &fakeGate{}
	w := transferwatch.New(bus, gate)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Run(ctx)

	events := []ports.Event{
		ritual.StateChangedInfo{To: ritual.StageChecking},  // false — local probe
		ritual.StateChangedInfo{To: ritual.StagePulling},   // true  — download begins
		pulling.ApplyStartedInfo{},                         // false — network done, apply is local
		ritual.StateChangedInfo{To: ritual.StageRunning},   // false — server up, no wire transfer
		ritual.StateChangedInfo{To: ritual.StagePushing},   // true  — upload begins
		ritual.StateChangedInfo{To: ritual.StageUnlocking}, // false — local refs work
	}
	for _, e := range events {
		bus.Publish(e)
	}

	want := []bool{false, true, false, false, true, false}
	require.Eventually(t, func() bool { return len(gate.calls()) >= len(want) }, time.Second, 5*time.Millisecond,
		"watch must process every StateChanged + ApplyStarted event into a gate toggle")
	assert.Equal(t, want, gate.calls(),
		"gate must arm (true) only on entering Pulling and Pushing and disarm (false) on apply-start and every non-transfer stage — so the heartbeat pulses during wire stalls but idle/local beats stay silent")
}
