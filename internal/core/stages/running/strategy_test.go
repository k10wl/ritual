package running_test

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"ritual/internal/adapters"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess is re-entered as a subprocess via os.Args[0]. Guarded
// by HELPER_MODE env; returns immediately for normal test runs.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("HELPER_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "inside_stop":
		d, err := time.ParseDuration(os.Getenv("HELPER_DELAY"))
		if err != nil {
			os.Exit(99)
		}
		time.Sleep(d)
		os.Stdout.WriteString("[Server thread/INFO]: Stopping the server\n")
		os.Stdout.Sync()
		time.Sleep(20 * time.Millisecond)
		os.Exit(0)
	case "outside_stop_respects":
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "stop" {
				os.Stdout.WriteString("[Server thread/INFO]: Stopping the server\n")
				os.Stdout.Sync()
				time.Sleep(20 * time.Millisecond)
				os.Exit(0)
			}
		}
		os.Exit(91) // stdin EOF without stop
	case "outside_stop_ignores":
		// Ignore stdin entirely, simulate a hung server that does not
		// respond to stop. Must be force-killed by WaitDelay.
		for {
			time.Sleep(time.Hour)
		}
	case "ordered_stop":
		// Assert stdin ordering: first line must be "save-off" (proving
		// readiness handler ran before stop was flushed), then "stop"
		// terminates cleanly.
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			os.Exit(92)
		}
		if strings.TrimSpace(sc.Text()) != "save-off" {
			os.Stdout.WriteString("[ORDER_FAIL]: first line was not save-off\n")
			os.Stdout.Sync()
			os.Exit(93)
		}
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "stop" {
				os.Stdout.WriteString("[Server thread/INFO]: Stopping the server\n")
				os.Stdout.Sync()
				time.Sleep(20 * time.Millisecond)
				os.Exit(0)
			}
		}
		os.Exit(94)
	default:
		os.Exit(97)
	}
}

// helperCmdBuilder returns a ports.CmdBuilder that spawns os.Args[0] re-entered
// into TestHelperProcess with the given HELPER_MODE/env.
type helperCmdBuilder struct {
	mode string
	env  []string
}

func (b *helperCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "HELPER_MODE="+b.mode)
	cmd.Env = append(cmd.Env, b.env...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

// stubReadiness returns err from Wait immediately. Used to simulate ready/not-ready.
type stubReadiness struct{ err error }

func (s *stubReadiness) Wait(context.Context) error { return s.err }

// gatedReadiness blocks Wait until gate closes (or ctx cancels).
type gatedReadiness struct {
	gate <-chan struct{}
}

func (g *gatedReadiness) Wait(ctx context.Context) error {
	select {
	case <-g.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sentinel is a terminal strategy that records that it was visited.
type sentinel struct {
	name    string
	visited bool
}

func (s *sentinel) Name() string { return s.name }

func (s *sentinel) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	s.visited = true
	return nil, nil //nolint:nilnil // terminal test stub
}

// collectEvents subscribes to bus, returns a slice that grows until cancelCh closes.
func collectEvents(t *testing.T, bus ports.EventBus) (*[]ports.Event, func()) {
	t.Helper()
	ch, unsub := bus.Subscribe()
	events := []ports.Event{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			events = append(events, e)
		}
	}()
	return &events, func() {
		unsub()
		<-done
	}
}

// Story 5.1 — server exits 0 on its own (simulates player typing /stop in
// console, or any clean exit). Strategy must publish ServerStoppedInfo,
// leave rs.Err nil, and route to onNext (Done), never onCrash.
func TestRunning_InsideStop_PublishesStoppedAndRoutesOnNext(t *testing.T) {
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		&helperCmdBuilder{
			mode: "inside_stop",
			env:  []string{"HELPER_DELAY=20ms"},
		},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)

	rs := &ritual.RunState{Bus: bus}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "strategy.Run should not error on clean subprocess exit")
	assert.Nil(t, rs.Err, "rs.Err must stay nil when server exits cleanly")
	assert.Equal(t, machine.Strategy[ritual.RunState](onNext), next, "clean exit must route to onNext, not onCrash")
	assert.False(t, onCrash.visited, "onCrash must not be visited on clean exit")

	stopCollect()
	assert.False(t, hasEvent[running.ServerCrashedInfo](*events), "ServerCrashedInfo must NOT be published on clean exit")
	assertLifecycleOrder(t, *events)
}

// assertLifecycleOrder checks that the four lifecycle events appear in order:
// ServerStarting → ServerReady → ServerStopping → ServerStopped.
func assertLifecycleOrder(t *testing.T, events []ports.Event) {
	t.Helper()
	wanted := []string{"starting", "ready", "stopping", "stopped"}
	seen := []string{}
	for _, e := range events {
		switch e.(type) {
		case running.ServerStartingInfo:
			seen = append(seen, "starting")
		case running.ServerReadyInfo:
			seen = append(seen, "ready")
		case running.ServerStoppingInfo:
			seen = append(seen, "stopping")
		case running.ServerStoppedInfo:
			seen = append(seen, "stopped")
		}
	}
	assert.Equal(t, wanted, seen, "lifecycle events must be STARTING → STARTED → STOPPING → STOPPED")
}

func hasEvent[T ports.Event](events []ports.Event) bool {
	for _, e := range events {
		if _, ok := e.(T); ok {
			return true
		}
	}
	return false
}

func findEvent[T ports.Event](events []ports.Event) *T {
	for _, e := range events {
		if v, ok := e.(T); ok {
			return &v
		}
	}
	return nil
}

// Story 5.3 — outside stop: ctx cancel after readiness must trigger a
// graceful stdin `stop\n` rather than TerminateProcess. Server responds,
// exits 0, classifier routes to onNext with full lifecycle.
func TestRunning_OutsideStop_Graceful_PublishesStoppedAndRoutesOnNext(t *testing.T) {
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		&helperCmdBuilder{mode: "outside_stop_respects"},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(500 * time.Millisecond)

	rs := &ritual.RunState{Bus: bus}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	// Cancel ctx shortly after readiness has fired. 40ms is enough time
	// for the strategy to spawn the helper, readiness to resolve, and
	// stdin wiring to settle.
	time.AfterFunc(40*time.Millisecond, cancel)

	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "strategy.Run must not error on graceful outside stop")
	assert.Nil(t, rs.Err, "rs.Err must be nil on graceful outside stop")
	assert.Equal(t, machine.Strategy[ritual.RunState](onNext), next, "onNext must be returned on graceful outside stop")
	assert.False(t, onCrash.visited, "onCrash must not be visited")

	stopCollect()
	assert.False(t, hasEvent[running.ServerCrashedInfo](*events), "ServerCrashedInfo must NOT be published on graceful outside stop")
	assertLifecycleOrder(t, *events)
}

// Story 5.5 — ctx cancelled DURING starting (pre-ready). Stop must be
// queued and flushed AFTER readiness, so the server sees save-off followed
// by stop — not stop before it's listening. Helper asserts the ordering.
func TestRunning_StopDuringStarting_QueuedAndFlushedInOrder(t *testing.T) {
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	gate := make(chan struct{})
	strategy := running.New(
		&helperCmdBuilder{mode: "ordered_stop"},
		&gatedReadiness{gate: gate},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(1 * time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	// Cancel BEFORE the readiness gate opens — this is the key scenario.
	time.AfterFunc(30*time.Millisecond, cancel)
	// Open readiness gate AFTER cancel has fired.
	time.AfterFunc(80*time.Millisecond, func() { close(gate) })

	rs := &ritual.RunState{Bus: bus}
	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err)
	assert.Nil(t, rs.Err)
	assert.Equal(t, machine.Strategy[ritual.RunState](onNext), next, "queued-stop flow must route to onNext")
	assert.False(t, onCrash.visited)

	stopCollect()
	assert.False(t, hasEvent[running.ServerCrashedInfo](*events), "queued-stop is not a crash")

	// Helper prints "[ORDER_FAIL]" to stdout if save-off did not precede stop.
	for _, e := range *events {
		if out, ok := e.(running.ServerOutputInfo); ok {
			assert.NotContains(t, out.Line, "[ORDER_FAIL]", "stop was written before save-off — queue not working")
		}
	}

	// Full lifecycle: STARTING → STARTED (ready fired after queued cancel) → STOPPING → STOPPED
	assertLifecycleOrder(t, *events)
}

// Story 5.4 — ctx cancelled before strategy.Run is invoked. Strategy must
// detect the cancelled ctx and exit fast without spawning the subprocess
// (no orphan, no damage). Route to onNext as user-requested termination.
func TestRunning_CancelledBeforeRun_FastExitNoSpawn(t *testing.T) {
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		// If Run were to spawn this helper it would hang indefinitely;
		// a fast return is the only way this test can pass.
		&helperCmdBuilder{mode: "outside_stop_ignores"},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(500 * time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancelled before Run is called

	rs := &ritual.RunState{Bus: bus}
	start := time.Now()
	next, err := strategy.Run(ctx, rs)
	elapsed := time.Since(start)

	require.NoError(t, err, "pre-start cancel is not an error condition")
	assert.Nil(t, rs.Err, "pre-start cancel must leave rs.Err nil")
	assert.Equal(t, machine.Strategy[ritual.RunState](onNext), next, "pre-start cancel must route to onNext")
	assert.False(t, onCrash.visited, "onCrash must not be visited on pre-start cancel")
	assert.Less(t, elapsed, 100*time.Millisecond, "pre-start cancel must exit fast without spawning subprocess")

	stopCollect()
	assert.False(t, hasEvent[running.ServerCrashedInfo](*events), "pre-start cancel is not a crash")
}

// Concurrent-Run guard — Strategy must reject a second Run while the first
// is in flight, and must not spawn a second subprocess. This is the thin
// safety net layered under the orchestrator guard (story #7).
func TestRunning_ConcurrentRun_RejectsDuplicate(t *testing.T) {
	bus := adapters.NewEventBus(64)
	startingCh := make(chan struct{}, 1)
	go func() {
		sub, unsub := bus.Subscribe()
		defer unsub()
		for e := range sub {
			if _, ok := e.(running.ServerStartingInfo); ok {
				select {
				case startingCh <- struct{}{}:
				default:
				}
				return
			}
		}
	}()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		&helperCmdBuilder{mode: "outside_stop_respects"},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	rs1 := &ritual.RunState{Bus: bus}
	done := make(chan struct{})
	go func() {
		defer close(done)
		strategy.Run(ctx, rs1)
	}()

	// Wait for first Run to enter (has published ServerStartingInfo).
	select {
	case <-startingCh:
	case <-time.After(1 * time.Second):
		t.Fatal("first Run never reached ServerStartingInfo")
	}

	// Attempt second Run — must be rejected fast.
	rs2 := &ritual.RunState{Bus: bus}
	next, err := strategy.Run(ctx, rs2)

	require.Error(t, err, "second concurrent Run must return an error")
	assert.Nil(t, next, "rejected Run must not advance the machine")

	// Release first Run.
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("first Run did not return after cancel")
	}
}

// Story 5.3 fallback — server ignores stop\n. WaitDelay fires, Go kills
// the process. Because ctx was cancelled, the classifier still treats
// this as a graceful stop (user-requested, not a crash) per story #8.
func TestRunning_OutsideStop_ForceKillFallback_StillStopped(t *testing.T) {
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		&helperCmdBuilder{mode: "outside_stop_ignores"},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(150 * time.Millisecond)

	rs := &ritual.RunState{Bus: bus}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()
	time.AfterFunc(40*time.Millisecond, cancel)

	next, err := strategy.Run(ctx, rs)

	require.NoError(t, err, "strategy.Run must not propagate the kill error")
	assert.Nil(t, rs.Err, "ctx-cancelled run is user-requested, not a crash")
	assert.Equal(t, machine.Strategy[ritual.RunState](onNext), next, "ctx-cancel + force-kill must still route to onNext")
	assert.False(t, onCrash.visited, "onCrash must not be visited when ctx was cancelled")

	stopCollect()
	assert.True(t, hasEvent[running.ServerStoppingInfo](*events), "ServerStoppingInfo must be published even when force-killed")
	assert.True(t, hasEvent[running.ServerStoppedInfo](*events), "ServerStoppedInfo must be published after force-kill")
	assert.False(t, hasEvent[running.ServerCrashedInfo](*events), "force-kill after ctx-cancel is not a crash")

	stopped := findEvent[running.ServerStoppedInfo](*events)
	require.NotNil(t, stopped, "ServerStoppedInfo must be present")
	assert.True(t, stopped.Forced, "force-killed stop must set Forced=true so UI can distinguish from graceful")
}

// Fix 2 — cmd.Cancel publishes ServerStopRequestedInfo so the UI can show
// "Stopping…" (user intent) immediately, separate from ServerStoppingInfo
// which only fires when stop\n is actually delivered to the server.
func TestRunning_UserCancel_PublishesStopRequested(t *testing.T) {
	if raceEnabled {
		t.Skip("timing-sensitive: race-instrumented helper subprocess can exceed the tight grace period")
	}
	bus := adapters.NewEventBus(64)
	events, stopCollect := collectEvents(t, bus)
	defer stopCollect()

	onNext := &sentinel{name: "NEXT"}
	onCrash := &sentinel{name: "CRASH"}

	strategy := running.New(
		&helperCmdBuilder{mode: "outside_stop_respects"},
		&stubReadiness{err: nil},
		onNext,
		onCrash,
	)
	strategy.SetStopGracePeriod(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()
	time.AfterFunc(40*time.Millisecond, cancel)

	rs := &ritual.RunState{Bus: bus}
	_, err := strategy.Run(ctx, rs)
	require.NoError(t, err)

	stopCollect()
	assert.True(t, hasEvent[running.ServerStopRequestedInfo](*events), "cmd.Cancel must publish ServerStopRequestedInfo")

	stopped := findEvent[running.ServerStoppedInfo](*events)
	require.NotNil(t, stopped, "ServerStoppedInfo must be present")
	assert.False(t, stopped.Forced, "graceful stop must leave Forced=false")
}
