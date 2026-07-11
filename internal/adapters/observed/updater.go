// observed/updater.go wraps a ports.UpdaterService with bus-backed event
// publishing. Mirrors observed.Locker/Retention/Check: the inner service stays
// bus-free and unit-testable; observability is a decorator concern. The
// published Update* events are the single stream the dial and the on-disk log
// both consume (design-log/037 §Q8).
package observed

import (
	"context"
	"errors"

	"ritual/internal/core/ports"
)

// Updater decorates a ports.UpdaterService, publishing one entry event + one
// outcome event per verb. From is the running binary's version, carried into
// UpdateCheckInfo so the log line self-describes ("2.0.0 → 2.1.0").
type Updater struct {
	inner ports.UpdaterService
	from  string
	bus   ports.EventBus
}

// NewUpdater decorates inner with bus-backed event publishing. from is the
// running version (config.AppVersion).
func NewUpdater(inner ports.UpdaterService, from string, bus ports.EventBus) *Updater {
	return &Updater{inner: inner, from: from, bus: bus}
}

var _ ports.UpdaterService = (*Updater)(nil)

// Check publishes UpdateCheckStarted, runs the inner check, then publishes
// UpdateCheckInfo (and UpdateFailed on error). The Started event is the
// Preflight entry signal for launch and manual re-check alike.
func (u *Updater) Check(ctx context.Context) (ports.Update, bool, error) {
	u.publish(UpdateCheckStarted{})
	up, outdated, err := u.inner.Check(ctx)
	u.publish(UpdateCheckInfo{From: u.from, To: up.Version, Outdated: outdated, Candidates: up.Candidates, Err: err})
	// Context cancellation/deadline means the caller aborted the check (e.g.
	// the 10s launch timeout) — not a user-visible failure. Let the projection
	// wake to IDLE via UpdateCheckInfo. Real errors (network, R2) still route
	// through UpdateFailed → PhaseFailed so the user sees the retry hint.
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		u.publish(UpdateFailed{Stage: "check", Err: err})
	}
	return up, outdated, err
}

// Apply publishes UpdateApplyStarted (the Updating entry signal), runs the
// inner apply, and — only when it returns, which on success it does not (the
// process is replaced) — publishes the outcome. A non-nil error routes to
// PhaseFailed via UpdateFailed.
func (u *Updater) Apply(ctx context.Context, up ports.Update) error {
	u.publish(UpdateApplyStarted{Version: up.Version})
	err := u.inner.Apply(ctx, up)
	u.publish(UpdateApplyInfo{Version: up.Version, Err: err})
	if err != nil {
		u.publish(UpdateFailed{Stage: "apply", Err: err})
	}
	return err
}

func (u *Updater) publish(evt ports.Event) {
	if u.bus != nil {
		u.bus.Publish(evt)
	}
}
