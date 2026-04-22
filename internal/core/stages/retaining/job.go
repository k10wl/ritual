package retaining

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// Job runs one keyspace's full retention cycle: select, delete, and (where
// applicable) sweep orphan blobs. A Job is a plain function so tests can
// pass closures directly; stage orchestration stays trivial.
type Job func(ctx context.Context) error

// NewRefsJob assembles the refs-side cycle for one storage. Errors from
// each step accumulate via errors.Join so a delete failure does not block
// the collector. Select failures propagate but still allow collect to run
// (GC is independent of whether retention chose anything).
func NewRefsJob(ret services.Retention, storage ports.StorageRepository, collector ports.Collector) Job {
	return func(ctx context.Context) error {
		var errs []error

		keys, err := ret.Select(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("select: %w", err))
		} else if len(keys) > 0 {
			if err := storage.DeleteBatch(ctx, keys); err != nil {
				errs = append(errs, fmt.Errorf("delete: %w", err))
			}
		}

		if err := collector.Collect(ctx); err != nil {
			errs = append(errs, fmt.Errorf("collect: %w", err))
		}

		return errors.Join(errs...)
	}
}

// NewLogsJob assembles the logs-side cycle. No collector — logs have no
// content-addressed blob store behind them.
func NewLogsJob(ret services.Retention, storage ports.StorageRepository) Job {
	return func(ctx context.Context) error {
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
	}
}
