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

// Entries holds the pipeline entry strategies the controller can drive
// (design-log/031, /038). Session is the play-a-world chain; Download, Upload,
// and Restore are the server-free flows. A nil entry means that gesture is not
// wired — startWith rejects it. The composition root builds them from
// pipeline.Build / BuildDownload / BuildUpload / BuildRestore.
type Entries struct {
	Session      machine.Strategy[ritual.RunState]
	LocalSession machine.Strategy[ritual.RunState]
	Download     machine.Strategy[ritual.RunState]
	Upload       machine.Strategy[ritual.RunState]
	Restore      machine.Strategy[ritual.RunState]
}

// Controller holds the per-run mutable state. The Attach goroutine is the
// only writer; bus subscribers see published events only.
type controller struct {
	bus          ports.EventBus
	entries      Entries
	runner       *ritual.Runner
	status       Outcome
	cancel       context.CancelFunc
	userStop     atomic.Bool
	sessionHooks []func(*ritual.RunState)
}

// SessionHook fires synchronously inside start() after a fresh RunState
// is constructed and before the runner goroutine is dispatched. Used by
// subsystems that need a stable per-session reference — e.g. the live-
// sync dispatcher (design-log/016) writing draft RefIDs into rs.RefID.
//
// Hooks run on the lifecycle consumer goroutine. Keep them cheap and
// non-blocking; long work belongs on the bus.
type SessionHook = func(*ritual.RunState)

// Attach subscribes the controller to bus and spawns a goroutine that
// dispatches command events until parent is cancelled or the channel
// closes. Returns a stop func that cancels the consumer + waits for it to
// drain.
//
// Subscription happens synchronously before Attach returns — callers that
// Publish on the bus immediately after Attach returns are guaranteed
// delivery. Variadic sessionHooks fire once per session start (see
// SessionHook).
func Attach(parent context.Context, bus ports.EventBus, entries Entries, sessionHooks ...SessionHook) func() {
	c := &controller{bus: bus, entries: entries, status: Idle, sessionHooks: sessionHooks}
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
				switch e := event.(type) {
				case ritual.StartRequested:
					// SkipSync selects the local-only pipeline (design-log/036).
					// Both run sessionHooks (livesync dispatcher binding) — the
					// ticker is structurally inert in the local flow (no Pulling
					// → empty parentFn), so the binding is a harmless no-op there.
					if e.SkipSync {
						c.startWith(ctx, c.entries.LocalSession, ritual.FlowLocalSession, true)
					} else {
						c.startWith(ctx, c.entries.Session, ritual.FlowSession, true)
					}
				case ritual.DownloadRequested:
					c.startWith(ctx, c.entries.Download, ritual.FlowDownload, false)
				case ritual.UploadRequested:
					c.startWith(ctx, c.entries.Upload, ritual.FlowUpload, false)
				case ritual.RestoreRequested:
					// Restore pins the chosen historical ref on the fresh RunState
					// before the runner spins; the Pulling stage reads it via
					// pulling.FromTarget() (design-log/038). No livesync hooks.
					refID := e.RefID
					c.startWith(ctx, c.entries.Restore, ritual.FlowRestore, false, func(rs *ritual.RunState) {
						rs.TargetRefID = refID
					})
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

// startWith drives entry as a fresh run. Shared by all three gestures
// (design-log/031): the session start runs sessionHooks (livesync
// dispatcher binding); Download/Upload pass runHooks=false because no
// livesync ticker runs outside a Running session. A nil entry means the
// gesture is not wired — reject rather than nil-panic. The single status
// guard gives free mutual exclusion: any gesture is refused while another
// run is in flight.
func (c *controller) startWith(ctx context.Context, entry machine.Strategy[ritual.RunState], flow ritual.Flow, runHooks bool, mutators ...func(*ritual.RunState)) {
	if entry == nil {
		c.bus.Publish(StatusChanged{Status: c.status, Err: fmt.Errorf("cannot start: gesture not configured")})
		return
	}
	if c.status == Running {
		c.bus.Publish(StatusChanged{Status: c.status, Err: fmt.Errorf("cannot start: already %s", c.status)})
		return
	}
	c.userStop.Store(false)
	c.setStatus(Running)
	// Announce which flow is in flight so the projection can render Download
	// and Upload with honest, direction-specific dial beats (design-log/031).
	c.bus.Publish(ritual.FlowStartedInfo{Flow: flow})
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	runState := &ritual.RunState{RunID: runID, Bus: c.bus}
	// Per-gesture run-state setup (e.g. restore pins rs.TargetRefID) runs before
	// the session hooks and the runner — design-log/038.
	for _, m := range mutators {
		m(runState)
	}
	if runHooks {
		for _, h := range c.sessionHooks {
			h(runState)
		}
	}
	c.runner = ritual.NewRunner(runState)

	go func() {
		err := c.runner.Run(runCtx, entry)
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
