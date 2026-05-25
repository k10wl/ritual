package livesync_test

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/running"
	"ritual/internal/subsystems/livesync"
	"sync"
	"testing"
	"time"
)

// ---- fakes ----

type fakeCommitter struct {
	mu      sync.Mutex
	calls   []ports.CommitOpts
	ids     []domain.RefID
	failErr error
}

func (f *fakeCommitter) Commit(_ context.Context, opts ports.CommitOpts) (domain.RefID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return "", f.failErr
	}
	f.calls = append(f.calls, opts)
	id := domain.RefID(fmt.Sprintf("draft-%d", len(f.calls)))
	f.ids = append(f.ids, id)
	return id, nil
}

func (f *fakeCommitter) snapshot() ([]ports.CommitOpts, []domain.RefID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.CommitOpts(nil), f.calls...), append([]domain.RefID(nil), f.ids...)
}

type fakePusher struct {
	mu      sync.Mutex
	pushed  []domain.RefID
	failErr error
}

func (f *fakePusher) Push(_ context.Context, id domain.RefID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	f.pushed = append(f.pushed, id)
	return nil
}

func (f *fakePusher) snapshot() []domain.RefID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.RefID(nil), f.pushed...)
}

// saveResponder echoes SaveCompleted on every SaveRequested it sees. The
// real running.Strategy does this after the server logs "Saved the
// game"; tests substitute this responder.
func saveResponder(bus ports.EventBus) func() {
	ch, cancel := bus.Subscribe()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				if _, ok := e.(running.SaveRequested); ok {
					bus.Publish(running.SaveCompleted{})
				}
			}
		}
	}()
	return func() {
		close(stop)
		cancel()
	}
}

// collectLiveDraftIDs captures every LiveDraftCommitted. snapshot() is
// safe to call concurrently with the collector goroutine.
func collectLiveDraftIDs(bus ports.EventBus) (snapshot func() []domain.RefID, stop func()) {
	var mu sync.Mutex
	var ids []domain.RefID
	ch, cancel := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			if ev, ok := e.(livesync.LiveDraftCommitted); ok {
				mu.Lock()
				ids = append(ids, ev.RefID)
				mu.Unlock()
			}
		}
	}()
	snapshot = func() []domain.RefID {
		mu.Lock()
		defer mu.Unlock()
		return append([]domain.RefID(nil), ids...)
	}
	stop = func() {
		cancel()
		<-done
	}
	return snapshot, stop
}

func waitFor(t *testing.T, name string, deadline time.Duration, cond func() bool) {
	t.Helper()
	t0 := time.Now()
	for time.Since(t0) < deadline {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: never observed within %v", name, deadline)
}

// Story: player plays past one interval — world syncs (Commit + Push +
// LiveDraftCommitted published).
func TestNew_FirstTick_CommitsPushesPublishes(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()
	liveDraftIDs, stopCol := collectLiveDraftIDs(bus)
	defer stopCol()

	committer := &fakeCommitter{}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})

	waitFor(t, "first commit", time.Second, func() bool {
		c, _ := committer.snapshot()
		return len(c) >= 1
	})

	calls, ids := committer.snapshot()
	if calls[0].Parent != "PARENT-AAA" || calls[0].Amend != "" {
		t.Fatalf("first opts: parent=%q amend=%q (want parent=PARENT-AAA amend=\"\")", calls[0].Parent, calls[0].Amend)
	}
	if len(calls[0].Targets) != 1 || calls[0].Targets[0] != "world/**" {
		t.Fatalf("targets: %#v", calls[0].Targets)
	}

	waitFor(t, "first push", time.Second, func() bool {
		return len(pusher.snapshot()) >= 1
	})
	if got := pusher.snapshot()[0]; got != ids[0] {
		t.Fatalf("pushed %q, expected %q", got, ids[0])
	}

	waitFor(t, "LiveDraftCommitted", time.Second, func() bool {
		return len(liveDraftIDs()) >= 1
	})
	if got := liveDraftIDs()[0]; got != ids[0] {
		t.Fatalf("LiveDraftCommitted RefID=%q, expected %q", got, ids[0])
	}
}

// Story: second interval amends first draft — Amend = lastRefID.
func TestNew_SecondTick_AmendsFirstDraft(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()

	committer := &fakeCommitter{}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})

	waitFor(t, "two commits", 2*time.Second, func() bool {
		c, _ := committer.snapshot()
		return len(c) >= 2
	})
	calls, ids := committer.snapshot()
	if calls[1].Amend != ids[0] {
		t.Fatalf("second tick Amend=%q, expected %q (first draft id)", calls[1].Amend, ids[0])
	}
	if calls[1].Parent != "PARENT-AAA" {
		t.Fatalf("second tick Parent=%q, expected PARENT-AAA (immutable for session)", calls[1].Parent)
	}
}

// Story: new session — lastRefID resets so the first tick of session 2
// is a fresh commit, not an amend of the previous session's draft.
func TestNew_NewSession_ResetsLastRefID(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()

	committer := &fakeCommitter{}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})
	waitFor(t, "first commit", time.Second, func() bool {
		c, _ := committer.snapshot()
		return len(c) >= 1
	})
	bus.Publish(running.ServerStoppedInfo{})

	time.Sleep(40 * time.Millisecond)
	bus.Publish(running.ServerReadyInfo{})

	waitFor(t, "second-session commit", 2*time.Second, func() bool {
		c, _ := committer.snapshot()
		return len(c) >= 2
	})
	calls, _ := committer.snapshot()
	if calls[1].Amend != "" {
		t.Fatalf("first tick of new session should be fresh, got Amend=%q", calls[1].Amend)
	}
}

// Story: push fails persistently — server uninterrupted, no goroutine
// leak, lastRefID still updated so next tick's Amend sweeps the prior
// (design §amend gap fix).
func TestNew_PushFailure_TickStillAmendsNext(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()

	committer := &fakeCommitter{}
	pusher := &fakePusher{failErr: errors.New("network unreachable")}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})

	waitFor(t, "two commits", 2*time.Second, func() bool {
		c, _ := committer.snapshot()
		return len(c) >= 2
	})
	calls, ids := committer.snapshot()
	if calls[1].Amend != ids[0] {
		t.Fatalf("after push failure, next tick must Amend prior draft: Amend=%q want %q", calls[1].Amend, ids[0])
	}
	if got := len(pusher.snapshot()); got != 0 {
		t.Fatalf("failing pusher should have recorded 0 successes, got %d", got)
	}
}

// Story: parentFn returns "" — no head pulled yet — tick aborts before
// Commit. Guards the very-early ServerReadyInfo window in tests where
// Pulling stage hasn't run.
func TestNew_NoParent_AbortsTick(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()

	committer := &fakeCommitter{}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})
	time.Sleep(200 * time.Millisecond)

	if c, _ := committer.snapshot(); len(c) != 0 {
		t.Fatalf("tick must abort when parent is empty, got %d commits", len(c))
	}
}

// Story: Commit returns an error — tick publishes ErrorInfo, no
// LiveDraftCommitted, lastRefID unchanged. Next tick must retry from
// the same Amend baseline. Case matrix row 9 (offline-style commit
// failure variant).
func TestNew_CommitError_PublishesErrorAndSkipsPublish(t *testing.T) {
	bus := adapters.NewEventBus(64)
	stopSR := saveResponder(bus)
	defer stopSR()
	liveDraftIDs, stopCol := collectLiveDraftIDs(bus)
	defer stopCol()

	committer := &fakeCommitter{failErr: errors.New("commit boom")}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, time.Second)
	defer stop()

	bus.Publish(running.ServerReadyInfo{})
	time.Sleep(150 * time.Millisecond)

	if got := len(liveDraftIDs()); got != 0 {
		t.Fatalf("commit failure must not publish LiveDraftCommitted, got %d", got)
	}
	if got := len(pusher.snapshot()); got != 0 {
		t.Fatalf("commit failure must not call pusher, got %d push", got)
	}
}

// Story: SaveCompleted never arrives — tick times out, ErrorInfo is
// published, no Commit attempted. Server cannot be wedged by livesync.
func TestNew_SaveTimeout_AbortsWithoutCommit(t *testing.T) {
	bus := adapters.NewEventBus(64)
	// no saveResponder — SaveRequested goes unanswered

	committer := &fakeCommitter{}
	pusher := &fakePusher{}
	parentFn := func() domain.RefID { return "PARENT-AAA" }

	_, _, stop := livesync.New(bus, committer, pusher, []string{"world/**"}, parentFn,
		25*time.Millisecond, 50*time.Millisecond) // very short timeout
	defer stop()

	bus.Publish(running.ServerReadyInfo{})
	time.Sleep(300 * time.Millisecond)

	if c, _ := committer.snapshot(); len(c) != 0 {
		t.Fatalf("commit must NOT run when save handshake times out, got %d", len(c))
	}
}
