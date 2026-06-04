package running

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAlreadyRunning rejects a second concurrent Run as a safety net under
// the orchestrator-level guard that refuses Start while in Running state.
var ErrAlreadyRunning = errors.New("running: another Run is in flight")

// Strategy implements the Running stage that owns the server subprocess.
type Strategy struct {
	cmd             ports.CmdBuilder
	readiness       ports.ReadinessCheck
	onNext          machine.Strategy[ritual.RunState]
	onCrash         machine.Strategy[ritual.RunState]
	stopGracePeriod time.Duration
	inFlight        atomic.Bool
}

// DefaultStopGracePeriod is the WaitDelay applied when the caller does not
// override via SetStopGracePeriod. Generous enough to cover Minecraft cold
// start + save on graceful stop; force-kill fires only if truly hung.
const DefaultStopGracePeriod = 5 * time.Minute

// New builds a Running Strategy with DefaultStopGracePeriod.
func New(
	cmd ports.CmdBuilder,
	readiness ports.ReadinessCheck,
	onNext, onCrash machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{
		cmd:             cmd,
		readiness:       readiness,
		onNext:          onNext,
		onCrash:         onCrash,
		stopGracePeriod: DefaultStopGracePeriod,
	}
}

// SetStopGracePeriod bounds how long Go waits after ctx.Done — and the
// graceful stdin stop — before TerminateProcess-ing the subprocess.
func (s *Strategy) SetStopGracePeriod(d time.Duration) { s.stopGracePeriod = d }

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageRunning }

// Run launches the server process and blocks until it exits or is cancelled.
//
//nolint:contextcheck // readinessCtx intentionally independent: survives ctx cancellation to complete boot.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	if !s.inFlight.CompareAndSwap(false, true) {
		return nil, ErrAlreadyRunning
	}
	defer s.inFlight.Store(false)

	if ctx.Err() != nil {
		return s.onNext, nil
	}

	fail := func(err error) (machine.Strategy[ritual.RunState], error) {
		rs.Err = err
		publish(rs.Bus, ritual.ErrorInfo{Operation: "server", Err: err})
		return s.onCrash, nil
	}

	stdinR, stdinW, outR, outW, err := openPipes()
	if err != nil {
		return fail(err)
	}

	// Subscribe BEFORE Build so any external observer that learns Build has
	// run (in tests this is fakeServerCmdBuilder.ready firing during Build)
	// is guaranteed to find this stage's sub already wired. Audit fix #4
	// makes the bus path the only delivery channel for ritual.StopRequested
	// during running, so a missed subscription = a missed stop.
	sub, unsub := rs.Bus.Subscribe()
	defer unsub()

	cmd, err := s.cmd.Build(ctx, stdinR, outW)
	if err != nil {
		return fail(err)
	}

	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{}, 1)
	stoppingDetectedCh := make(chan struct{}, 1)
	coordDone := make(chan struct{})

	cmd.Cancel = func() error {
		publish(rs.Bus, ServerStopRequestedInfo{})
		signal(stopCh)
		return nil
	}
	cmd.WaitDelay = s.stopGracePeriod

	publish(rs.Bus, ritual.StartInfo{Operation: "server"})
	jobGuard, err := startGuarded(cmd)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = jobGuard.Close() }()

	// Readiness probes across user-initiated ctx cancellation so a queued
	// stop can be flushed after the server finishes booting. Bounded by
	// cmd.WaitDelay and torn down on return.
	readinessCtx, readinessCancel := context.WithCancel(context.Background())
	defer readinessCancel()

	outCh := make(chan string, 64)
	go scanOutput(outR, outCh, rs.Bus, stoppingDetectedCh)
	go func() {
		if err := s.readiness.Wait(readinessCtx); err == nil {
			signal(readyCh)
		}
	}()
	go coordinate(coordDone, stdinW, outCh, rs.Bus, sub, stopCh, readyCh, stoppingDetectedCh)

	publish(rs.Bus, ServerStartingInfo{})

	waitErr := cmd.Wait()
	close(coordDone)
	_ = stdinR.Close()
	_ = outW.Close()

	if ctx.Err() == nil && waitErr != nil {
		rs.Err = waitErr
		publish(rs.Bus, ServerCrashedInfo{Err: waitErr})
		return s.onCrash, nil
	}
	// With Cancel set, Go's exec returns ctx.Err() as waitErr even on clean
	// process exit. ProcessState reflects the actual exit code; Success()
	// is the true signal for "exited cleanly within grace period".
	forced := ctx.Err() != nil && cmd.ProcessState != nil && !cmd.ProcessState.Success()
	publish(rs.Bus, ServerStoppedInfo{Forced: forced})
	publish(rs.Bus, ritual.FinishInfo{Operation: "server"})
	return s.onNext, nil
}

// coordinate owns stdin writes and ServerStoppingInfo emission. Exits when
// done closes (after cmd.Wait), which gives cmd.Cancel the whole run window
// to deliver its stop signal. The sub channel is owned and unsubscribed
// by the caller — registration must happen BEFORE Build returns to close
// the race where an external observer sees the server as ready and
// publishes StopRequested before this goroutine has wired its subscription.
func coordinate(
	done <-chan struct{},
	stdin io.Writer,
	outCh <-chan string,
	bus ports.EventBus,
	sub <-chan ports.Event,
	stopCh, readyCh, stoppingDetectedCh <-chan struct{},
) {
	var ready, stopQueued bool
	var stoppingOnce sync.Once
	emitStopping := func() {
		stoppingOnce.Do(func() { publish(bus, ServerStoppingInfo{}) })
	}
	writeStdin := func(s string) error {
		_, err := io.WriteString(stdin, s)
		if err != nil {
			publish(bus, ritual.ErrorInfo{Operation: "server", Err: err})
		}
		return err
	}
	writeStop := func() {
		if writeStdin("stop\n") == nil {
			emitStopping()
		}
	}

	for {
		select {
		case <-done:
			return
		case <-readyCh:
			_ = writeStdin("save-off\n")
			ready = true
			publish(bus, ServerReadyInfo{})
			if stopQueued {
				writeStop()
			}
		case <-stopCh:
			if ready {
				writeStop()
			} else {
				stopQueued = true
			}
		case <-stoppingDetectedCh:
			emitStopping()
		case e, ok := <-sub:
			if !ok {
				return
			}
			if _, ok := e.(ritual.StopRequested); ok {
				publish(bus, ServerStopRequestedInfo{})
				if ready {
					writeStop()
				} else {
					stopQueued = true
				}
			}
			if _, ok := e.(SaveRequested); ok {
				if writeStdin("save-all flush\n") == nil {
					if waitForLine(outCh, "Saved the game", 30*time.Second) {
						publish(bus, SaveCompleted{})
					}
				}
			}
			if ci, ok := e.(ConsoleInput); ok {
				line := strings.TrimRight(ci.Text, "\r\n")
				if strings.TrimSpace(line) != "" {
					// Echo only on a confirmed write (design-log/042 §Q8): the
					// GUI console is wire-driven, never optimistic.
					if writeStdin(line+"\n") == nil {
						publish(bus, ConsoleEchoInfo{Text: line})
					}
				}
			}
		}
	}
}

func scanOutput(r io.Reader, outCh chan<- string, bus ports.EventBus, stoppingDetectedCh chan<- struct{}) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		publish(bus, ServerOutputInfo{Line: line})
		if strings.Contains(line, "Stopping the server") {
			signal(stoppingDetectedCh)
		}
		select {
		case outCh <- line:
		default:
		}
	}
	close(outCh)
}

func waitForLine(outCh <-chan string, match string, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return false
		case line, ok := <-outCh:
			if !ok {
				return false
			}
			if strings.Contains(line, match) {
				return true
			}
		}
	}
}

// openPipes uses os.Pipe (not io.Pipe) so exec dups the fds directly —
// avoids the copier-goroutine trap where cmd.Wait blocks forever on a
// never-closed pipe.
func openPipes() (stdinR, stdinW, outR, outW *os.File, err error) {
	stdinR, stdinW, err = os.Pipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	outR, outW, err = os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return nil, nil, nil, nil, err
	}
	return
}

func signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
