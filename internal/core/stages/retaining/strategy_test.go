package retaining_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/retaining"
	"testing"
	"time"
)

type stubStrategy struct{ name string }

func (s *stubStrategy) Name() string { return s.name }
func (s *stubStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	return nil, nil //nolint:nilnil // terminal stub for tests
}

func TestStrategy_EmitsRetentionAndGCEventsPerJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	jobs := []retaining.Job{
		{Kind: retaining.KindRetention, Label: "refs-local", Run: func(_ context.Context) error { return nil }},
		{Kind: retaining.KindGC, Label: "gc-refs-local", Run: func(_ context.Context) error { return nil }},
		{Kind: retaining.KindRetention, Label: "logs-local", Run: func(_ context.Context) error { return nil }},
	}
	onOK := &stubStrategy{name: "Done"}
	s := retaining.New(jobs, bus, nil, onOK)

	rs := &ritual.RunState{RunID: "r-1", Bus: bus}
	next, err := s.Run(ctx, rs)
	if err != nil {
		t.Fatalf("healthy run must not error: %v", err)
	}
	if next != onOK {
		t.Fatalf("healthy run must advance to onOK")
	}

	got := drain(ch, 50*time.Millisecond)
	wantOrder := []string{
		"retention.start:refs-local",
		"retention.finish:refs-local:nil",
		"gc.start:gc-refs-local",
		"gc.finish:gc-refs-local:nil",
		"retention.start:logs-local",
		"retention.finish:logs-local:nil",
	}
	assertEventOrder(t, got, wantOrder)
}

func TestStrategy_FailedJob_EmitsFinishedWithErr_RoutesToOnFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	boom := errors.New("delete-boom")
	jobs := []retaining.Job{
		{Kind: retaining.KindRetention, Label: "refs-remote", Run: func(_ context.Context) error { return boom }},
		{Kind: retaining.KindGC, Label: "gc-refs-remote", Run: func(_ context.Context) error { return nil }},
	}
	onFail := &stubStrategy{name: "Failed"}
	s := retaining.New(jobs, bus, onFail, &stubStrategy{name: "Done"})

	rs := &ritual.RunState{RunID: "r-1", Bus: bus}
	next, err := s.Run(ctx, rs)
	if err != nil {
		t.Fatalf("strategy must record err on rs and return nil err: %v", err)
	}
	if next != onFail {
		t.Fatalf("a failing job must route to onFail")
	}
	if rs.Err == nil || !errors.Is(rs.Err, boom) {
		t.Fatalf("rs.Err must surface joined boom; got %v", rs.Err)
	}

	got := drain(ch, 50*time.Millisecond)
	if !containsEventTag(got, "retention.finish:refs-remote:err") {
		t.Errorf("must emit RetentionFinishedInfo with non-nil Err for failed retention job; got %v", got)
	}
	if !containsEventTag(got, "gc.finish:gc-refs-remote:nil") {
		t.Errorf("subsequent GC must still run + emit a clean finish; got %v", got)
	}
}

func drain(ch <-chan ports.Event, settle time.Duration) []ports.Event {
	deadline := time.NewTimer(settle)
	defer deadline.Stop()
	var out []ports.Event
	for {
		select {
		case <-deadline.C:
			return out
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		}
	}
}

func eventTag(e ports.Event) string {
	switch v := e.(type) {
	case retaining.RetentionStartedInfo:
		return "retention.start:" + v.Label
	case retaining.RetentionFinishedInfo:
		if v.Err != nil {
			return "retention.finish:" + v.Label + ":err"
		}
		return "retention.finish:" + v.Label + ":nil"
	case retaining.GCStartedInfo:
		return "gc.start:" + v.Label
	case retaining.GCFinishedInfo:
		if v.Err != nil {
			return "gc.finish:" + v.Label + ":err"
		}
		return "gc.finish:" + v.Label + ":nil"
	default:
		return ""
	}
}

func assertEventOrder(t *testing.T, got []ports.Event, want []string) {
	t.Helper()
	var tags []string
	for _, e := range got {
		if tag := eventTag(e); tag != "" {
			tags = append(tags, tag)
		}
	}
	if len(tags) != len(want) {
		t.Fatalf("event count mismatch:\n got=%v\nwant=%v", tags, want)
	}
	for i, w := range want {
		if tags[i] != w {
			t.Fatalf("event[%d] mismatch: got %q want %q (full got=%v)", i, tags[i], w, tags)
		}
	}
}

func containsEventTag(got []ports.Event, want string) bool {
	for _, e := range got {
		if eventTag(e) == want {
			return true
		}
	}
	return false
}
