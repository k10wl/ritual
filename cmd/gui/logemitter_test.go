package main

import (
	"context"
	"ritual/internal/gui/logsink"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batchSink records every flush the emitter produces, standing in for the
// window EmitEvent so the ring is observable without a live Wails window.
type batchSink struct {
	mu      sync.Mutex
	batches []logsink.ServerLogBatch
	calls   int32
}

func (s *batchSink) attach(e *batchingLogEmitter) {
	fn := func(b logsink.ServerLogBatch) {
		atomic.AddInt32(&s.calls, 1)
		s.mu.Lock()
		s.batches = append(s.batches, b)
		s.mu.Unlock()
	}
	e.out.Store(&fn)
}

func (s *batchSink) lines() []logsink.ServerLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []logsink.ServerLog
	for _, b := range s.batches {
		out = append(out, b.Lines...)
	}
	return out
}

func (s *batchSink) dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, b := range s.batches {
		total += b.Dropped
	}
	return total
}

func line(text string) logsink.ServerLog {
	return logsink.ServerLog{Ts: 1, Kind: "out", Text: text}
}

// A burst delivered with no flush window in between must arrive whole, in
// order, coalesced into far fewer IPC calls than the line count.
func TestBatchingEmitter_CoalescesBurstInOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := newBatchingLogEmitter()
	sink := &batchSink{}
	sink.attach(e)
	go e.loop(ctx)

	const n = 1000
	for i := 0; i < n; i++ {
		e.Emit(line("L" + itoa(i)))
	}

	require.Eventually(t, func() bool { return len(sink.lines()) == n },
		2*time.Second, 5*time.Millisecond, "all lines must drain")

	got := sink.lines()
	for i := 0; i < n; i++ {
		require.Equal(t, "L"+itoa(i), got[i].Text, "order must be preserved across batches")
	}
	assert.Zero(t, sink.dropped(), "1000 lines is well under capacity — no drops expected")
	assert.Less(t, int(atomic.LoadInt32(&sink.calls)), n,
		"coalescing must produce far fewer IPC calls than lines")
}

// The load-bearing assertion for the lazy-timer design (042 §Q5): with no
// Emit, the loop parks on `wake` — zero flushes, zero wakeups.
func TestBatchingEmitter_IdleQuiescence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := newBatchingLogEmitter()
	sink := &batchSink{}
	sink.attach(e)
	go e.loop(ctx)

	time.Sleep(200 * time.Millisecond)

	assert.Zero(t, atomic.LoadInt32(&sink.calls), "no Emit ⇒ no flush ⇒ no IPC at rest")
	assert.Len(t, e.wake, 0, "the wake channel must be empty — nothing armed the timer")
}

// Overflowing the ring while no flush has run drops oldest and surfaces the
// count on the next batch.
func TestBatchingEmitter_OverflowReportsDropped(t *testing.T) {
	e := newBatchingLogEmitter() // no loop running ⇒ ring fills, nothing drains
	const over = 1100            // capacity 1024
	for i := 0; i < over; i++ {
		e.Emit(line("L" + itoa(i)))
	}

	sink := &batchSink{}
	sink.attach(e)
	// Drain synchronously: flush repeatedly until the ring empties.
	for {
		before := len(sink.lines())
		e.flush()
		if len(sink.lines()) == before {
			break
		}
	}

	assert.Equal(t, over-e.cfg.Capacity, sink.dropped(), "the oldest %d lines must be reported dropped", over-e.cfg.Capacity)
	assert.Len(t, sink.lines(), e.cfg.Capacity, "the ring holds at most Capacity survivors")
	// The survivors are the most recent lines (oldest dropped).
	got := sink.lines()
	assert.Equal(t, "L"+itoa(over-1), got[len(got)-1].Text, "newest line must survive")
}

// Minimal int→string to avoid importing strconv just for labels.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
