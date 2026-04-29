package progress_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/adapters/progress"
	"ritual/internal/core/ports"
)

// fillPCG deterministically fills buf with bytes from a seeded PCG stream.
// Writes 8 bytes per iteration to keep prep cost off the test's wall budget.
func fillPCG(buf []byte, seed1, seed2 uint64) {
	rng := rand.New(rand.NewPCG(seed1, seed2))
	i := 0
	for ; i+8 <= len(buf); i += 8 {
		binary.LittleEndian.PutUint64(buf[i:i+8], rng.Uint64())
	}
	if i < len(buf) {
		var tail [8]byte
		binary.LittleEndian.PutUint64(tail[:], rng.Uint64())
		copy(buf[i:], tail[:len(buf)-i])
	}
}

// TestTicker_EmitsTicksAsCountersAdvance is the end-to-end realistic test the
// POC convention maps onto our stack. It runs concurrent workers pushing blobs
// through the same preinitialized CompressingStorage (single zstd encoder +
// mutex) wrapped by CounterStorage, and asserts the ticker reports progress
// while work is still in-flight — not only after completion.
func TestTicker_EmitsTicksAsCountersAdvance(t *testing.T) {
	fs := newFS(t)
	compressed, err := adapters.NewCompressingStorage(fs)
	require.NoError(t, err)

	counters := &adapters.StorageCounters{}
	metered := adapters.NewCounterStorage(compressed, counters)

	bus := adapters.NewEventBus(64)
	ch, cancelSub := bus.Subscribe()
	defer cancelSub()

	const interval = 40 * time.Millisecond
	ticker := progress.NewTicker(counters, bus, interval)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go ticker.Run(ctx)

	const workers = 6
	const perWorker = 8
	payload := bytes.Repeat([]byte("realistic-chunk-block-"), 512) // ~11 KB, compresses well

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("objects/%016x-%d-%d", xxhash.Sum64(payload), base, i)
				_ = metered.PutStream(ctx, key, bytes.NewReader(payload))
			}
		}(w)
	}
	wg.Wait()

	deadline := time.After(500 * time.Millisecond)
	var ticks []progress.Tick
collect:
	for {
		select {
		case evt := <-ch:
			if tick, ok := evt.(progress.Tick); ok {
				ticks = append(ticks, tick)
				if len(ticks) >= 3 {
					break collect
				}
			}
		case <-deadline:
			break collect
		}
	}
	cancel()

	require.GreaterOrEqual(t, len(ticks), 2,
		"at least two ticks must fire while concurrent workers drive the shared encoder")

	assert.Greater(t, ticks[len(ticks)-1].BytesOut, int64(0),
		"BytesOut must advance — shared zstd+mutex under load still produces live progress")

	lastOut := int64(0)
	for i, tick := range ticks {
		assert.GreaterOrEqual(t, tick.BytesOut, lastOut, "tick %d must be monotonic", i)
		lastOut = tick.BytesOut
	}
	assert.Equal(t, int64(workers*perWorker), counters.OpsComplete.Load(),
		"ops-complete counter must equal total PutStream calls across workers")
}

// TestTicker_SnapshotReflectsCounters is a non-timing test of the derived
// numbers. Drives counters directly, then asks for a Snapshot.
func TestTicker_SnapshotReflectsCounters(t *testing.T) {
	counters := &adapters.StorageCounters{}
	ticker := progress.NewTicker(counters, nil, 10*time.Millisecond)

	counters.BytesIn.Store(1_000_000)
	counters.BytesOut.Store(2_000_000)
	counters.OpsComplete.Store(5)

	start := time.Now().Add(-1 * time.Second)
	tick := ticker.Snapshot(start, start, 0, 0)

	assert.Equal(t, int64(1_000_000), tick.BytesIn)
	assert.Equal(t, int64(2_000_000), tick.BytesOut)
	assert.Equal(t, int64(5), tick.OpsComplete)
	assert.Greater(t, tick.AvgMbpsIn, 0.0, "avg Mbps in must be positive for a non-empty counter")
	assert.Greater(t, tick.AvgMbpsOut, tick.AvgMbpsIn, "avg out is 2x in for this fixture")
}

// TestTicker_StableCounters_NoTicks locks audit fix #9 (POC session
// docs/dev-session-2026-04-25-poc-setup.md): when no storage activity has
// occurred, the ticker must NOT emit. Pre-fix the ticker emitted on every
// interval regardless of counter movement, spamming the GUI log window
// and the on-disk <root>/logs/<ts>.log with zero-delta noise during idle
// stages (Acquiring, Running's body wait, Failed-with-retry-pending).
func TestTicker_StableCounters_NoTicks(t *testing.T) {
	counters := &adapters.StorageCounters{}
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	const interval = 20 * time.Millisecond
	ticker := progress.NewTicker(counters, bus, interval)
	ctx, cancel := context.WithTimeout(t.Context(), 6*interval)
	defer cancel()
	go ticker.Run(ctx)

	var ticks []progress.Tick
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			if tick, ok := evt.(progress.Tick); ok {
				ticks = append(ticks, tick)
			}
		case <-ctx.Done():
			goto done
		}
	}
done:

	assert.Empty(t, ticks,
		"audit fix #9 regression: with counters never touched the ticker must publish ZERO Tick events across the entire window — pre-fix one Tick per interval flooded the bus + on-disk log with zero-delta lines an operator had to scroll past to find anything signal")
}

// TestTicker_OneFinalZeroDeltaAfterActivityStops locks the second half of
// audit fix #9: after activity ceases the ticker must emit exactly one
// zero-delta tick — the "we're done" marker — then go silent. This is the
// signal a downstream reducer (projection) needs to flip the bar to 100%
// and clear the throughput label without polling.
func TestTicker_OneFinalZeroDeltaAfterActivityStops(t *testing.T) {
	counters := &adapters.StorageCounters{}
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	const interval = 20 * time.Millisecond
	ticker := progress.NewTicker(counters, bus, interval)
	ctx, cancel := context.WithTimeout(t.Context(), 8*interval)
	defer cancel()
	go ticker.Run(ctx)

	counters.BytesIn.Store(4096)
	counters.OpsComplete.Store(1)

	var ticks []progress.Tick
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			if tick, ok := evt.(progress.Tick); ok {
				ticks = append(ticks, tick)
			}
		case <-ctx.Done():
			goto done
		}
	}
done:

	require.NotEmpty(t, ticks,
		"audit fix #9 regression: with counters bumped before the first interval at least one active tick must publish — otherwise the projection never sees the in-flight bytes and the bar never moves")

	last := ticks[len(ticks)-1]
	assert.Equal(t, 0.0, last.NowMbpsIn,
		"audit fix #9 regression: the LAST tick after activity stops must be the zero-delta marker (NowMbpsIn==0) — projection uses this to flip the label off the throughput line and the bar to its final state")

	for i, tick := range ticks[:len(ticks)-1] {
		if tick.NowMbpsIn == 0 {
			t.Fatalf("audit fix #9 regression: only the FINAL tick (index %d) may be zero-delta — tick %d already had NowMbpsIn==0, meaning the gate is letting through interior idle ticks again",
				len(ticks)-1, i)
		}
	}
}

func newFS(t *testing.T) ports.StorageRepository {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	fs, err := adapters.NewFSRepository(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

// TestTicker_Two50MBWorkers pushes two concurrent 50 MB pseudo-random files
// (100 MB total) through one shared CompressingStorage instance. This exercises
// the intersection of scale and concurrency that neither the single-file 50 MB
// test nor the toy-payload concurrency tests reach on their own:
//
//   - Encoder mutex behaviour at realistic per-op wall (~70 ms compression each).
//   - Two bytes.Buffer sinks alive simultaneously — one mid-compression under
//     the encoder lock, the other past unlock holding its compressed blob.
//   - Counters aggregate correctly under concurrent big-size writes.
//   - Ticker still reports live BytesOut while both workers are active.
//   - Decoder pool serves parallel big-size reads on readback.
//
// Uses different PCG seeds per worker so hash keys differ → no dedup collapse.
func TestTicker_Two50MBWorkers(t *testing.T) {
	fs := newFS(t)
	compressed, err := adapters.NewCompressingStorage(fs)
	require.NoError(t, err)
	counters := &adapters.StorageCounters{}
	metered := adapters.NewCounterStorage(compressed, counters)

	bus := adapters.NewEventBus(512)
	obs := observed.NewStorage(metered, bus)
	ch, cancelSub := bus.Subscribe()
	defer cancelSub()

	ticker := progress.NewTicker(counters, bus, 20*time.Millisecond)
	ctx, cancelTicker := context.WithCancel(t.Context())
	defer cancelTicker()
	go ticker.Run(ctx)

	const size = 50 * 1024 * 1024
	type work struct {
		payload []byte
		key     string
	}
	makeWork := func(seed1, seed2 uint64) work {
		p := make([]byte, size)
		fillPCG(p, seed1, seed2)
		return work{payload: p, key: fmt.Sprintf("objects/%016x", xxhash.Sum64(p))}
	}
	a := makeWork(0xA11CE, 0xC0DE)
	b := makeWork(0xB0B, 0xFACE)
	require.NotEqual(t, a.key, b.key, "distinct payloads must hash to distinct keys")

	start := time.Now()
	var wg sync.WaitGroup
	for _, w := range []work{a, b} {
		wg.Add(1)
		go func(w work) {
			defer wg.Done()
			require.NoError(t, obs.PutStream(t.Context(), w.key, bytes.NewReader(w.payload)))
			rc, err := obs.GetStream(t.Context(), w.key)
			require.NoError(t, err)
			got, err := io.ReadAll(rc)
			require.NoError(t, err)
			require.NoError(t, rc.Close(), "integrity check on Close")
			require.Equal(t, len(w.payload), len(got))
			require.Equal(t, w.payload[:2048], got[:2048], "head bytes match")
			require.Equal(t, w.payload[len(w.payload)-2048:], got[len(got)-2048:], "tail bytes match")
		}(w)
	}
	wg.Wait()
	totalWall := time.Since(start)
	cancelTicker()

	var ticks []progress.Tick
	puts, gets := 0, 0
	drainDeadline := time.After(100 * time.Millisecond)
drain:
	for {
		select {
		case evt := <-ch:
			switch e := evt.(type) {
			case progress.Tick:
				ticks = append(ticks, e)
			case observed.StoragePutStreamInfo:
				assert.Equal(t, int64(size), e.Bytes, "each PutStream event must carry 50 MB")
				assert.NoError(t, e.Err)
				puts++
			case observed.StorageGetStreamInfo:
				assert.Equal(t, int64(size), e.Bytes, "each GetStream event must carry 50 MB")
				assert.NoError(t, e.Err)
				gets++
			}
		case <-drainDeadline:
			break drain
		}
	}

	assert.Equal(t, 2, puts, "one PutStreamInfo per worker")
	assert.Equal(t, 2, gets, "one GetStreamInfo per worker")
	require.GreaterOrEqual(t, len(ticks), 3,
		"two concurrent 50 MB pipelines (wall=%v, interval=20ms) must produce ≥3 ticks", totalWall)

	lastOut, lastIn := int64(0), int64(0)
	for i, tick := range ticks {
		assert.GreaterOrEqual(t, tick.BytesOut, lastOut, "tick %d BytesOut monotonic across aggregate 100 MB", i)
		assert.GreaterOrEqual(t, tick.BytesIn, lastIn, "tick %d BytesIn monotonic", i)
		lastOut, lastIn = tick.BytesOut, tick.BytesIn
	}
	assert.Greater(t, ticks[len(ticks)-1].BytesOut, int64(0),
		"final tick shows aggregate BytesOut > 0 — live visibility under concurrent big-size load")

	assert.Equal(t, int64(2*size), counters.BytesOut.Load(), "aggregate BytesOut == 100 MB")
	assert.Equal(t, int64(2*size), counters.BytesIn.Load(), "aggregate BytesIn == 100 MB after both readbacks")
	assert.Equal(t, int64(4), counters.OpsComplete.Load(), "four ops: 2 Put + 2 Get")
	assert.Equal(t, int64(0), counters.OpsFailed.Load(), "zero failures under concurrent big-size load")

	t.Logf("total_wall=%v ticks=%d final_avg_out=%.2fMbps",
		totalWall, len(ticks), ticks[len(ticks)-1].AvgMbpsOut)
}
