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
// MVP deferrals (follow-up stories once the session-lock port lands):
//   - Step 4 pre-PUT fence verify (lock sessionId check).
//   - Step 5 `If-None-Match: *` conditional PUT on first-ever push.
//   - Step 6 post-PUT fence verify + zombie-writer self-DELETE rollback.
//   - Parallel blob upload (10 workers) — serial upload is correct, just
//     slower; parallelism is a performance concern, not a correctness one.
//
// Delegated elsewhere:
//   - Blob compression + hash verification → CompressingStorage.
//   - Session lock → orchestrator (once the fence story lands).
//   - FlushFileBuffers durability → FSRepository.
type Pusher struct {
	from   ports.StorageRepository
	to     ports.StorageRepository
	runner ports.BlobRunner
}

// NewPusher wires a Pusher. Push reads from `from` and writes to `to`.
// In normal composition `from` is local FS and `to` is remote R2, but
// Pusher is direction-agnostic — swap them for a local-to-local mirror
// and the verb still works. The composition root decides whether either
// side is wrapped with CompressingStorage. runner schedules per-blob
// upload concurrency; pass a serial runner for deterministic order or a
// bounded-pool runner to saturate the upload pipe.
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
		if err := p.uploadBlob(ctx, hash); err != nil {
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

func (p *Pusher) uploadBlob(ctx context.Context, hash string) error {
	key := blobKey(hash)

	present, err := p.to.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("exists check: %w", err)
	}
	if present {
		return nil
	}

	rc, err := p.from.GetStream(ctx, key)
	if err != nil {
		return fmt.Errorf("read blob: %w", err)
	}
	putErr := p.to.PutStream(ctx, key, rc)
	closeErr := rc.Close()
	if putErr != nil {
		return fmt.Errorf("upload blob: %w", putErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close source blob %s: %w", key, closeErr)
	}
	return nil
}

var _ ports.Pusher = (*Pusher)(nil)
