// Package lifecycle owns the run-level controller: subscribes to the
// command bus (ritual.StartRequested / StopRequested / DismissRequested),
// drives the FSM via ritual.Runner over the pipeline entry strategy, and
// publishes StatusChanged as Outcome transitions.
//
// The chain topology lives in subsystems/pipeline. Locker concrete + its
// method values are wired by the composition root and passed in through
// pipeline.Deps; lifecycle does not own them.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ritual/internal/config"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"sync/atomic"
	"time"
)

// StatusChanged is published on every Outcome transition. Err is non-nil
// only on Failed or when an invalid command was rejected.
type StatusChanged struct {
	Status Outcome
	Err    error
}

func (s StatusChanged) String() string {
	if s.Err != nil {
		return fmt.Sprintf("status: %s err: %v", s.Status, s.Err)
	}
	return fmt.Sprintf("status: %s", s.Status)
}

// Controller holds the per-run mutable state. The Attach goroutine is the
// only writer; bus subscribers see published events only.
type controller struct {
	bus      ports.EventBus
	entry    machine.Strategy[ritual.RunState]
	runner   *ritual.Runner
	status   Outcome
	cancel   context.CancelFunc
	userStop atomic.Bool
}

// Attach subscribes the controller to bus and spawns a goroutine that
// dispatches command events until parent is cancelled or the channel
// closes. Returns a stop func that cancels the consumer + waits for it to
// drain.
//
// Subscription happens synchronously before Attach returns — callers that
// Publish on the bus immediately after Attach returns are guaranteed
// delivery.
func Attach(parent context.Context, bus ports.EventBus, entry machine.Strategy[ritual.RunState]) func() {
	c := &controller{bus: bus, entry: entry, status: Idle}
	bus.Publish(StatusChanged{Status: Idle})

	ch, unsub := bus.Subscribe()
	done := make(chan struct{})
	ctx, cancelLoop := context.WithCancel(parent)
	go func() {
		defer close(done)
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				switch event.(type) {
				case ritual.StartRequested:
					c.start(ctx)
				case ritual.StopRequested:
					c.stop()
				case ritual.DismissRequested:
					c.dismiss()
				}
			}
		}
	}()
	return func() {
		cancelLoop()
		<-done
	}
}

func (c *controller) start(ctx context.Context) {
	if c.status == Running {
		c.bus.Publish(StatusChanged{Status: c.status, Err: fmt.Errorf("cannot start: already %s", c.status)})
		return
	}
	c.userStop.Store(false)
	c.setStatus(Running)
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	runState := &ritual.RunState{RunID: runID, Bus: c.bus}
	c.runner = ritual.NewRunner(runState)

	go func() {
		err := c.runner.Run(runCtx, c.entry)
		if err == nil {
			err = runState.Err
		}
		c.resolveStatus(runCtx, err)
	}()
}

// stop flags userStop so resolveStatus classifies a clean exit as Done
// rather than Failed. It deliberately does NOT cancel runCtx — audit fix #4
// (docs/dev-session-2026-04-25-poc-setup.md): cancelling here propagated
// past the running stage and aborted Committing on its first storage call,
// silently dropping the user's session. The running stage now subscribes
// to ritual.StopRequested directly and writes stop\n itself; runCtx stays
// alive so Committing+Pushing+Retaining+Unlocking complete.
//
// Trade-off captured in the audit: bus-driven stop only acts during the
// running stage. A user stop during Pulling/Pushing is a no-op until
// parent ctx ends (e.g., window-close budget). Acceptable per POC.
func (c *controller) stop() {
	if c.status != Running {
		return
	}
	c.userStop.Store(true)
}

// dismiss acknowledges a Failed outcome and returns the lifecycle to Idle.
// The Failed→Dismissed→Idle path replaces retry-from-failed (see
// design-log/017): users dismiss a failure to clear UI state, then
// re-engage with a fresh Start. Rejecting dismiss outside Failed keeps the
// API honest — Done/Idle/Running have no failure to acknowledge.
func (c *controller) dismiss() {
	if c.status != Failed {
		c.bus.Publish(StatusChanged{Status: c.status, Err: fmt.Errorf("cannot dismiss: status is %s", c.status)})
		return
	}
	c.setStatus(Dismissed)
	c.setStatus(Idle)
}

// resolveStatus maps runner exit into Done/Failed. A user-initiated stop
// is graceful: errors downstream of the cancelled ctx (e.g. cmd.Build
// returning context.Canceled mid-boot) are expected, not real failures,
// so they resolve to Done. Non-cancel errors from stages still propagate
// as Failed even during a user stop.
func (c *controller) resolveStatus(ctx context.Context, runErr error) {
	if c.userStop.Load() && (runErr == nil || errors.Is(runErr, context.Canceled)) {
		c.setStatus(Done)
		return
	}
	if runErr != nil || ctx.Err() != nil {
		c.setStatus(Failed)
		return
	}
	c.setStatus(Done)
}

func (c *controller) setStatus(status Outcome) {
	c.status = status
	c.bus.Publish(StatusChanged{Status: status})
}
