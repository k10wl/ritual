package running

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

type Strategy struct {
	cmd       ports.CmdBuilder
	readiness ports.ReadinessCheck
	onNext    machine.Strategy[ritual.RunState]
	onCrash   machine.Strategy[ritual.RunState]
}

func New(
	cmd ports.CmdBuilder,
	readiness ports.ReadinessCheck,
	onNext, onCrash machine.Strategy[ritual.RunState],
) *Strategy {
	return &Strategy{cmd: cmd, readiness: readiness, onNext: onNext, onCrash: onCrash}
}

func (*Strategy) Name() string { return ritual.StageRunning }

func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	stdinR, stdinW := io.Pipe()
	outR, outW := io.Pipe()

	cmd, err := s.cmd.Build(ctx, stdinR, outW)
	if err != nil {
		rs.Err = err
		publish(rs.Bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.onCrash, nil
	}

	publish(rs.Bus, ports.StartInfo{Operation: "server"})
	if err := cmd.Start(); err != nil {
		rs.Err = err
		publish(rs.Bus, ports.ErrorInfo{Operation: "server", Err: err})
		return s.onCrash, nil
	}

	outCh := make(chan string, 64)
	go scanOutput(outR, outCh, rs.Bus)
	go s.waitReady(ctx, rs.Bus)
	go listenCommands(ctx, stdinW, outCh, rs.Bus)

	publish(rs.Bus, ports.ServerStartingInfo{})

	waitErr := cmd.Wait()
	stdinR.Close()
	outW.Close()

	if waitErr != nil {
		rs.Err = waitErr
		publish(rs.Bus, ports.ServerCrashedInfo{Err: waitErr})
		return s.onCrash, nil
	}
	publish(rs.Bus, ports.ServerStoppedInfo{})
	publish(rs.Bus, ports.FinishInfo{Operation: "server"})
	return s.onNext, nil
}

func (s *Strategy) waitReady(ctx context.Context, bus ports.EventBus) {
	if err := s.readiness.Wait(ctx); err == nil {
		publish(bus, ports.ServerReadyInfo{})
	}
}

func scanOutput(r io.Reader, outCh chan<- string, bus ports.EventBus) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		publish(bus, ports.ServerOutputInfo{Line: line})
		select {
		case outCh <- line:
		default:
		}
	}
	close(outCh)
}

func listenCommands(ctx context.Context, stdin io.WriteCloser, outCh <-chan string, bus ports.EventBus) {
	ch, unsub := bus.Subscribe()
	defer unsub()
	defer stdin.Close()
	for {
		select {
		case <-ctx.Done():
			io.WriteString(stdin, "stop\n")
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if _, ok := e.(ports.SaveRequested); ok {
				io.WriteString(stdin, "save-all flush\n")
				if waitForLine(outCh, "Saved the game", 30*time.Second) {
					publish(bus, ports.SaveCompleted{})
				}
			}
		}
	}
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

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
