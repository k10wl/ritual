package retaining

import (
	"context"
	"fmt"
	"ritual/internal/core/ports"
	"ritual/internal/core/retention"
)

// Kind classifies a Job for typed event emission. Retention selects and
// deletes manifest keys; GC mark-sweeps content-addressed blobs. Stage
// strategy iterates Jobs and emits Retention*/GC* events keyed off Kind.
type Kind int

const (
	// KindRetention runs a select+delete cycle over a manifest keyspace.
	KindRetention Kind = iota
	// KindGC runs a mark-sweep over the content-addressed blob store.
	KindGC
)

// Job is one retention or GC unit of work. Label identifies the side and
// keyspace (e.g. "refs-local", "gc-refs-local", "logs-local",
// "refs-remote", "gc-refs-remote") for events and logs. Run is the closure
// the strategy invokes; errors are joined across all Jobs in the slice.
type Job struct {
	Kind  Kind
	Label string
	Run   func(ctx context.Context) error
}

// NewRetentionRefsJob assembles the refs-side retention cycle (select +
// batch-delete) for one storage. Label is set by the caller to encode side
// (e.g. "refs-local").
func NewRetentionRefsJob(label string, ret retention.Retention, storage ports.StorageRepository) Job {
	return Job{
		Kind:  KindRetention,
		Label: label,
		Run: func(ctx context.Context) error {
			keys, err := ret.Select(ctx)
			if err != nil {
				return fmt.Errorf("select: %w", err)
			}
			if len(keys) == 0 {
				return nil
			}
			if err := storage.DeleteBatch(ctx, keys); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			return nil
		},
	}
}

// NewGCRefsJob assembles the refs-side GC cycle (mark-sweep over
// objects/) for one storage. Runs after retention so newly orphaned
// blobs are reaped in the same stage invocation.
func NewGCRefsJob(label string, collector ports.Collector) Job {
	return Job{
		Kind:  KindGC,
		Label: label,
		Run: func(ctx context.Context) error {
			if err := collector.Collect(ctx); err != nil {
				return fmt.Errorf("collect: %w", err)
			}
			return nil
		},
	}
}

// NewLogsJob assembles the logs-side retention cycle. No GC counterpart —
// logs have no content-addressed blob store behind them.
func NewLogsJob(label string, ret retention.Retention, storage ports.StorageRepository) Job {
	return Job{
		Kind:  KindRetention,
		Label: label,
		Run: func(ctx context.Context) error {
			keys, err := ret.Select(ctx)
			if err != nil {
				return fmt.Errorf("select: %w", err)
			}
			if len(keys) == 0 {
				return nil
			}
			if err := storage.DeleteBatch(ctx, keys); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			return nil
		},
	}
}
