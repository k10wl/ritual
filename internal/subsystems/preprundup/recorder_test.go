package preprundup_test

import (
	"ritual/internal/adapters"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/preprundup"
	"sync"
	"testing"
	"time"
)

type fakeAppender struct {
	mu    sync.Mutex
	calls []preprundup.Sample
	ch    chan struct{}
}

func newFakeAppender() *fakeAppender { return &fakeAppender{ch: make(chan struct{}, 16)} }

func (f *fakeAppender) Append(s preprundup.Sample) error {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
	f.ch <- struct{}{}
	return nil
}

func (f *fakeAppender) snapshot() []preprundup.Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]preprundup.Sample(nil), f.calls...)
}

func (f *fakeAppender) waitN(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(f.snapshot()) >= n {
			return
		}
		select {
		case <-f.ch:
		case <-deadline:
			t.Fatalf("only %d/%d appends arrived: %+v", len(f.snapshot()), n, f.snapshot())
		}
	}
}

func (f *fakeAppender) assertSilent(t *testing.T) {
	t.Helper()
	select {
	case <-f.ch:
		t.Fatalf("unexpected append: %+v", f.snapshot())
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAttach_FullSessionRecordsOneSample(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFakeAppender()
	stop := preprundup.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(ritual.StateChangedInfo{From: "Checking", To: ritual.StageAcquiring, RunID: "run-1"})
	time.Sleep(5 * time.Millisecond) // ensure prepMs/wrapMs come out non-zero
	bus.Publish(running.ServerReadyInfo{})
	bus.Publish(running.ServerStoppingInfo{})
	time.Sleep(5 * time.Millisecond)
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})

	f.waitN(t, 1)
	got := f.snapshot()[0]
	if got.RunID != "run-1" {
		t.Fatalf("RunID = %q, want %q", got.RunID, "run-1")
	}
	if got.PrepMs <= 0 {
		t.Fatalf("PrepMs = %d, want > 0", got.PrepMs)
	}
	if got.WrapMs <= 0 {
		t.Fatalf("WrapMs = %d, want > 0", got.WrapMs)
	}
}

func TestAttach_FailureMidPrepRecordsNothing(t *testing.T) {
	bus := adapters.NewEventBus(16)
	f := newFakeAppender()
	stop := preprundup.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowSession})
	bus.Publish(ritual.StateChangedInfo{From: "Checking", To: ritual.StageAcquiring, RunID: "run-1"})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Failed})

	f.assertSilent(t)
}

func TestAttach_UploadFlowAcquiringDoesNotStartPrepTimer(t *testing.T) {
	// design-log/058 §Q4: FlowUpload's own Probing->Acquiring chain must not
	// be misrecorded as session prep timing.
	bus := adapters.NewEventBus(16)
	f := newFakeAppender()
	stop := preprundup.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowUpload})
	bus.Publish(ritual.StateChangedInfo{From: "Probing", To: ritual.StageAcquiring, RunID: "run-1"})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})

	f.assertSilent(t)
}

func TestAttach_LocalSessionNeverReachesAcquiring_RecordsNothing(t *testing.T) {
	// design-log/036: FlowLocalSession is Checking -> Running -> Done, no
	// Acquiring anchor at all, so no partial sample must land.
	bus := adapters.NewEventBus(16)
	f := newFakeAppender()
	stop := preprundup.Attach(t.Context(), bus, f)
	defer stop()

	bus.Publish(ritual.FlowStartedInfo{Flow: ritual.FlowLocalSession})
	bus.Publish(running.ServerReadyInfo{})
	bus.Publish(running.ServerStoppingInfo{})
	bus.Publish(lifecycle.StatusChanged{Status: lifecycle.Done})

	f.assertSilent(t)
}
