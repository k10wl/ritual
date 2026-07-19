// Package draining is the pre-stage between Running and Committing that
// blocks until the live-sync ticker (design-log/016 §Drain barrier) has
// finished any in-flight tick and the dispatcher has applied every
// LiveDraftCommitted into rs.RefID. Cap is 10s (OQ5 escalate-and-abandon);
// on timeout the post-session Committing proceeds with whatever rs.RefID
// is current and sweepSupersededSiblings on the NEXT session cleans the
// orphan tick draft (if any).
//
// When no live-sync subsystem is wired (drainable == nil) the stage is a
// no-op pass-through — keeps the pipeline composable in tests and
// non-GUI binaries (e.g. cmd/fakerun).
package draining

import (
	"context"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/subsystems/livesync"
)

// Strategy implements the Draining stage.
type Strategy struct {
	drainable livesync.Drainable
	onNext    machine.Strategy[ritual.RunState]
}

// New constructs the drain barrier. drainable may be nil.
func New(drainable livesync.Drainable, onNext machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{drainable: drainable, onNext: onNext}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageDraining }

// Run blocks on Drain(ctx) up to its internal ceiling. Timeouts are
// non-fatal: they publish ErrorInfo and proceed to onNext. The post-
// session Committing strategy reads whatever rs.RefID the dispatcher
// managed to apply.
func (s *Strategy) Run(parentCtx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	if s.drainable == nil {
		return s.onNext, nil
	}
	publish(rs.Bus, ritual.StartInfo{Operation: "drain"})
	if err := s.drainable.Drain(parentCtx); err != nil {
		publish(rs.Bus, ritual.ErrorInfo{Operation: "drain", Err: err})
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "drain"})
	return s.onNext, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
