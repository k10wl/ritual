package preprundup

import (
	"context"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
	"time"
)

// HistoryAppender is the write side Attach depends on — satisfied by *Store.
type HistoryAppender interface {
	Append(Sample) error
}

// recorderState tracks one in-flight FlowSession run's timing anchors.
// Scope: FlowSession only (design-log/058 §Q4), gated explicitly via
// FlowStartedInfo rather than inferred from which stages fire — Acquiring
// also happens during FlowUpload (design-log/031's Probing→Acquiring chain)
// and would misrecord upload timing as prep timing if the gate weren't
// explicit. A sample is appended only when both PrepMs and WrapMs came out
// non-zero in the same run; a partial beat (e.g. FlowLocalSession, which
// never reaches Acquiring at all) records nothing.
type recorderState struct {
	store HistoryAppender

	inSession bool
	runID     string
	startedAt time.Time
	prepStart time.Time
	wrapStart time.Time
	prepMs    int64
	wrapMs    int64
}

func (s *recorderState) reset() {
	s.inSession = false
	s.runID = ""
	s.prepStart, s.wrapStart = time.Time{}, time.Time{}
	s.prepMs, s.wrapMs = 0, 0
}

// handle folds one bus event into the recorder's state, appending a sample
// to store when a FlowSession run completes with both beats timed.
func (s *recorderState) handle(e ports.Event) {
	switch ev := e.(type) {
	case ritual.FlowStartedInfo:
		s.reset()
		s.inSession = ev.Flow == ritual.FlowSession
		if s.inSession {
			s.startedAt = time.Now()
		}
	case ritual.StateChangedInfo:
		if s.inSession && ev.To == ritual.StageAcquiring {
			s.runID = ev.RunID
			s.prepStart = time.Now()
		}
	case running.ServerReadyInfo:
		if s.inSession && !s.prepStart.IsZero() {
			s.prepMs = time.Since(s.prepStart).Milliseconds()
		}
	case running.ServerStoppingInfo:
		if s.inSession {
			s.wrapStart = time.Now()
		}
	case lifecycle.StatusChanged:
		s.handleStatusChanged(ev)
	}
}

func (s *recorderState) handleStatusChanged(ev lifecycle.StatusChanged) {
	switch ev.Status {
	case lifecycle.Done:
		s.recordIfComplete()
		s.reset()
	case lifecycle.Failed, lifecycle.Idle, lifecycle.Dismissed:
		s.reset()
	case lifecycle.Running:
		// No-op: Running doesn't carry new timing information of its own —
		// the beat anchors are set by StateChangedInfo/ServerReadyInfo/
		// ServerStoppingInfo above, so state must survive this transition
		// untouched.
	}
}

// recordIfComplete appends a sample only when both beats of the current
// FlowSession run were actually timed — a partial beat records nothing
// (design-log/058 §Q4).
func (s *recorderState) recordIfComplete() {
	if s.inSession && !s.wrapStart.IsZero() {
		s.wrapMs = time.Since(s.wrapStart).Milliseconds()
	}
	if !s.inSession || s.prepMs <= 0 || s.wrapMs <= 0 {
		return
	}
	_ = s.store.Append(Sample{
		RunID:     s.runID,
		StartedAt: s.startedAt.UTC().Format(time.RFC3339),
		PrepMs:    s.prepMs,
		WrapMs:    s.wrapMs,
	})
}

// Attach subscribes a goroutine that watches the FlowSession beats and
// records completed prep/wrap timings until ctx is cancelled. Returns an
// idempotent stop func that cancels the subscription and waits for the
// consumer to drain — same shape as internal/subsystems/notify.Attach.
func Attach(ctx context.Context, bus ports.EventBus, store HistoryAppender) func() {
	ch, cancel := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := &recorderState{store: store}
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				s.handle(e)
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
