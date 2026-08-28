package refs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// Collector implements §Retention and GC — GC algorithm portion. It
// builds the live set by scanning every surviving refs/*.json, then
// deletes every objects/{hash} whose hash is not live.
//
// Retention (which refs to keep) is orthogonal: the caller deletes refs
// it wants pruned BEFORE invoking Collect, and Collect reaps whatever
// blobs the deleted refs were exclusively referencing.
//
// Fail-continue semantics (per §Retention and GC):
//   - A refs/*.json that fails to read or parse is skipped; the sweep
//     proceeds with the remaining refs. A malformed ref's exclusive
//     blobs (if any) become unreachable orphans and are swept — Pull's
//     parse-fail-delete path is the upstream recovery that prevents a
//     malformed ref from lingering in the first place.
//   - An objects/{hash} delete that fails is skipped; the sweep proceeds
//     with the remaining orphans. The stuck orphan retries on the next
//     GC cycle; blobs are content-addressed and harmless on disk.
//
// Both behaviors are intentional: GC is eventually-consistent cleanup,
// not a transaction. A flaky delete must not block the remaining sweep.
//
// Orphan deletes fan out through runner (the same ports.BlobRunner used by
// Pull/Push/Commit/Apply) instead of one-at-a-time — a session with dozens
// of orphaned blobs was previously paying a full network round-trip per
// key, serially.
type Collector struct {
	store  ports.StorageRepository
	runner ports.BlobRunner
}

// NewCollector wires a Collector. `store` is the side to sweep — local
// for local-GC-after-amend, remote for remote-GC-after-push. Collector
// is direction-agnostic; the composition root decides which side to wire.
// runner schedules orphan-delete concurrency.
func NewCollector(store ports.StorageRepository, runner ports.BlobRunner) *Collector {
	return &Collector{store: store, runner: runner}
}

// Collect removes every objects/{hash} not referenced by any refs/*.json
// in the store.
func (c *Collector) Collect(ctx context.Context) error {
	live, err := c.buildLiveSet(ctx)
	if err != nil {
		return err
	}
	blobKeys, err := c.store.List(ctx, "objects/")
	if err != nil {
		return fmt.Errorf("refs.Collector.Collect: list objects: %w", err)
	}
	var orphans []ports.BlobItem
	for _, key := range blobKeys {
		hash := path.Base(key)
		if _, isLive := live[hash]; isLive {
			continue
		}
		orphans = append(orphans, ports.BlobItem{Key: key})
	}
	// fn always returns nil so one flaky delete never trips runner's
	// first-error cancellation — fail-continue is per-key, not per-batch.
	return c.runner.Run(ctx, orphans, func(ctx context.Context, key string) error {
		_ = c.store.Delete(ctx, key)
		return nil
	})
}

func (c *Collector) buildLiveSet(ctx context.Context) (map[string]struct{}, error) {
	refKeys, err := c.store.List(ctx, "refs/")
	if err != nil {
		return nil, fmt.Errorf("refs.Collector.Collect: list refs: %w", err)
	}
	live := map[string]struct{}{}
	for _, key := range refKeys {
		rc, err := c.store.GetStream(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("refs.Collector.Collect: read ref %s: %w", key, err)
		}
		ref, parseErr := decodeRefBody(rc)
		_ = rc.Close()
		if parseErr != nil {
			continue
		}
		for _, obj := range ref.Objects {
			live[obj.Hash] = struct{}{}
		}
	}
	return live, nil
}

var _ ports.Collector = (*Collector)(nil)

// decodeRefBody reads a ref JSON document from r into a new domain.Ref.
func decodeRefBody(r io.Reader) (*domain.Ref, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	ref := &domain.Ref{}
	err = json.Unmarshal(raw, ref)
	if err != nil {
		return nil, err
	}
	return ref, nil
}
