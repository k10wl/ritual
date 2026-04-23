package adapters

import (
	"context"
	"ritual/internal/core/ports"
	"sort"
	"sync"
)

// ParallelRunner schedules fn over items across a bounded pool of goroutines.
// First non-nil error cancels remaining work; that error is returned. The
// supplied ctx is used both for fn invocation and for cancellation propagation.
//
// Items are dispatched in Weight-descending order so the heaviest blobs enter
// the pipeline first — research §QUEUE/§Size-desc sort: total wall time is
// unchanged but ETA stabilises immediately and the tail stall (last-blob
// straggler) shrinks because no big blob is left to start late. Same-weight
// items keep input order via sort.SliceStable. ParallelRunner mutates a local
// copy; callers' slice is untouched.
//
// limit caps in-flight goroutines. POC measured 10 saturates 20 Mbps → 1 Gbps
// links (spec §FAST-SYNC v2.1).
type ParallelRunner struct {
	limit int
}

// NewParallelRunner returns a runner with the given concurrency cap. Caller
// supplies the cap so different verbs can be tuned independently if ever
// needed; production wiring uses 10 per the spec.
func NewParallelRunner(limit int) *ParallelRunner {
	if limit < 1 {
		limit = 1
	}
	return &ParallelRunner{limit: limit}
}

func (p *ParallelRunner) Run(ctx context.Context, items []ports.BlobItem, fn func(context.Context, string) error) error {
	if len(items) == 0 {
		return nil
	}
	ordered := make([]ports.BlobItem, len(items))
	copy(ordered, items)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Weight > ordered[j].Weight })

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, p.limit)
	var wg sync.WaitGroup
	var (
		errMu    sync.Mutex
		firstErr error
	)
	captureErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errMu.Unlock()
	}

dispatch:
	for _, item := range ordered {
		select {
		case <-runCtx.Done():
			break dispatch
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(runCtx, key); err != nil {
				captureErr(err)
			}
		}(item.Key)
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

var _ ports.BlobRunner = (*ParallelRunner)(nil)
