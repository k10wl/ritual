// Package adapters_test — ParallelRunner story:
//
// ParallelRunner schedules per-item work across a bounded pool, surfaces the
// first non-nil error and cancels the rest, and propagates ctx cancellation
// to in-flight work. SerialRunner is the trivial baseline used everywhere by
// default; its contract is "run in input order until first error or ctx done".
package adapters_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/core/ports"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keys(items ...string) []ports.BlobItem {
	out := make([]ports.BlobItem, len(items))
	for i, k := range items {
		out[i] = ports.BlobItem{Key: k}
	}
	return out
}

func TestSerialRunner_RunsItemsInOrderUntilFirstError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	var seen []string
	boom := errors.New("boom")
	err := adapters.SerialRunner{}.Run(ctx, keys("a", "b", "c", "d"), func(_ context.Context, item string) error {
		seen = append(seen, item)
		if item == "c" {
			return boom
		}
		return nil
	})

	require.ErrorIs(t, err, boom,
		"SerialRunner: first non-nil fn error must surface verbatim — caller relies on errors.Is to classify the failing blob")
	assert.Equal(t, []string{"a", "b", "c"}, seen,
		"SerialRunner: items after the first failure must NOT execute — serial contract is 'stop at first error', not 'collect all errors'")
}

func TestParallelRunner_HonoursConcurrencyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	const limit = 3
	const items = 12
	var (
		mu       sync.Mutex
		inFlight int
		peak     int
	)
	work := make([]ports.BlobItem, items)
	for i := range work {
		work[i] = ports.BlobItem{Key: "item"}
	}
	runner := adapters.NewParallelRunner(limit)
	err := runner.Run(ctx, work, func(_ context.Context, _ string) error {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil
	})

	require.NoError(t, err,
		"ParallelRunner: a happy-path Run with no fn errors must return nil — the pool MUST NOT manufacture errors of its own")
	assert.LessOrEqual(t, peak, limit,
		"ParallelRunner: concurrent in-flight work must never exceed the configured limit — pool cap is the only knob protecting downstream resources from over-subscription")
	assert.Greater(t, peak, 1,
		"ParallelRunner: with 12 items and limit=3 the pool MUST actually run multiple workers concurrently — peak==1 means the pool is silently serial and the speedup is a lie")
}

func TestParallelRunner_FirstErrorCancelsRemainingWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	boom := errors.New("boom")
	work := make([]ports.BlobItem, 50)
	for i := range work {
		work[i] = ports.BlobItem{Key: "item"}
	}

	var executed atomic.Int32
	runner := adapters.NewParallelRunner(4)
	err := runner.Run(ctx, work, func(ctx context.Context, _ string) error {
		idx := executed.Add(1)
		if idx == 1 {
			return boom
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return nil
		}
	})

	require.ErrorIs(t, err, boom,
		"ParallelRunner: first non-nil fn error must surface verbatim — wrapping or substituting it would break errors.Is classification at the verb layer")
	assert.Less(t, executed.Load(), int32(len(work)),
		"ParallelRunner: after the first error the pool MUST cancel remaining work — running every item to completion turns 'first error wins' into 'all errors run, last error wins'")
}

func TestParallelRunner_PropagatesParentContextCancellation(t *testing.T) {
	parent, parentCancel := context.WithCancel(t.Context())
	t.Cleanup(parentCancel)

	work := make([]ports.BlobItem, 20)
	for i := range work {
		work[i] = ports.BlobItem{Key: "item"}
	}

	runner := adapters.NewParallelRunner(2)
	go func() {
		time.Sleep(10 * time.Millisecond)
		parentCancel()
	}()

	err := runner.Run(parent, work, func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})

	require.Error(t, err,
		"ParallelRunner: parent ctx cancellation must surface as an error — silent success on cancel hides whether the verb actually completed its blob set")
	assert.ErrorIs(t, err, context.Canceled,
		"ParallelRunner: cancellation error MUST be context.Canceled (not a wrapped 'pool closed' synonym) — verb error chains rely on the standard cancellation sentinel")
}

func TestParallelRunner_DispatchesHeaviestFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	work := []ports.BlobItem{
		{Key: "small", Weight: 1},
		{Key: "huge", Weight: 1_000_000},
		{Key: "medium", Weight: 500},
	}

	var (
		mu    sync.Mutex
		order []string
	)
	runner := adapters.NewParallelRunner(1)
	err := runner.Run(ctx, work, func(_ context.Context, key string) error {
		mu.Lock()
		order = append(order, key)
		mu.Unlock()
		return nil
	})

	require.NoError(t, err, "ParallelRunner: happy-path dispatch of weighted items must not error")
	assert.Equal(t, []string{"huge", "medium", "small"}, order,
		"ParallelRunner: items MUST enter the pipeline in Weight-descending order — size-desc stabilises ETA and shrinks tail stalls; random order regresses both (research §QUEUE §Size-desc sort)")
}

func TestSerialRunner_PreservesInputOrderRegardlessOfWeight(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	work := []ports.BlobItem{
		{Key: "a", Weight: 1},
		{Key: "b", Weight: 1_000_000},
		{Key: "c", Weight: 500},
	}

	var order []string
	err := adapters.SerialRunner{}.Run(ctx, work, func(_ context.Context, key string) error {
		order = append(order, key)
		return nil
	})

	require.NoError(t, err, "SerialRunner: happy-path dispatch must not error")
	assert.Equal(t, []string{"a", "b", "c"}, order,
		"SerialRunner: Weight MUST be ignored — tests depend on input-order determinism; re-ordering here would silently break every verb test that relies on predictable call sequencing")
}
