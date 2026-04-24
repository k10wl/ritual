package refs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// Pusher uploads a ref and every blob it references FROM one storage TO
// another. Blobs already present at the destination are skipped via an
// Exists gate. Push is idempotent: re-running on a complete destination
// replays the same bytes harmlessly.
//
// ACID responsibilities owned by Pusher (see §Push — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md):
//   - Step 1: load local ref from refs/{id}.json + validate JSON.
//   - Step 2: per-blob Exists gate at destination + upload missing blobs.
//   - Step 3: barrier — every referenced hash present at destination
//     before the ref PUT.
//   - Step 5: ref PUT is the single commit point for the push.
//
// Per-blob transfer (Exists gate + stream + scrub-on-failure) is delegated
// to transferBlob — the same primitive used by Puller. Pusher inherits
// Pull's scrub contract: a failed PutStream deletes any partial bytes at
// destination so the next Push sees Exists == false and retries cleanly.
//
// Delegated elsewhere:
//   - Blob compression + hash verification → CompressingStorage decorator.
//   - Isolation and any conditional-write semantics → storage decorator
//     or the composition root. Pusher works against the storage port alone.
//   - Write durability → the storage adapter.
type Pusher struct {
	from   ports.StorageRepository
	to     ports.StorageRepository
	runner ports.BlobRunner
}

// NewPusher wires a Pusher. Push reads from `from` and writes to `to`,
// both satisfying ports.StorageRepository. runner schedules per-blob
// transfer concurrency; a serial runner yields deterministic order, a
// bounded-pool runner widens the pipe.
func NewPusher(from, to ports.StorageRepository, runner ports.BlobRunner) *Pusher {
	return &Pusher{from: from, to: to, runner: runner}
}

// Push uploads the ref identified by id to the destination: every
// referenced objects/{hash} first (step 3 barrier), then refs/{id}.json
// (step 5 commit).
func (p *Pusher) Push(ctx context.Context, id domain.RefID) error {
	refRaw, ref, err := p.loadRef(ctx, id)
	if err != nil {
		return err
	}
	items, pathByHash := collectHashes(ref.Objects)
	err = p.runner.Run(ctx, items, func(ctx context.Context, hash string) error {
		if err := transferBlob(ctx, p.from, p.to, blobKey(hash)); err != nil {
			return fmt.Errorf("push %s: blob %s (%s): %w", id, hash, pathByHash[hash], err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = p.to.PutStream(ctx, refKey(id), bytes.NewReader(refRaw))
	if err != nil {
		return fmt.Errorf("push %s: upload ref: %w", id, err)
	}
	return nil
}

func (p *Pusher) loadRef(ctx context.Context, id domain.RefID) ([]byte, *domain.Ref, error) {
	key := refKey(id)
	rc, err := p.from.GetStream(ctx, key)
	if err != nil {
		return nil, nil, fmt.Errorf("push %s: load ref: %w", id, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("push %s: read ref: %w", id, err)
	}

	ref := &domain.Ref{}
	err = json.Unmarshal(raw, ref)
	if err != nil {
		return nil, nil, fmt.Errorf("push %s: parse ref: %w", id, err)
	}
	return raw, ref, nil
}

var _ ports.Pusher = (*Pusher)(nil)
