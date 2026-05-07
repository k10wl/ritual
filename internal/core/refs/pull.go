package refs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// PlanFn observes the byte/file budget of a transfer batch before any
// blob streams. The composition root wires it to bus.Publish so a GUI
// projection can populate the progress-bar denominator on the first
// frame.
type PlanFn func(ritual.PlanInfo)

// Puller fetches a ref and every blob it references FROM one storage TO
// another. Blobs already present at the destination are skipped. Pull is
// idempotent: re-running on a complete destination is a no-op on blobs
// and re-reads the ref only.
//
// ACID responsibilities owned by Puller (see §Pull — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md):
//   - Step 1: stream ref JSON from→to, validate, delete-on-parse-fail.
//   - Step 2: per-blob Exists gate on destination + fetch missing.
//   - Step 3: barrier — every referenced hash present at destination on success.
//
// Per-blob transfer (Exists gate + stream + scrub-on-failure) is delegated
// to transferBlob — same primitive used by Pusher, so both verbs share
// the same error sentinels (ErrBlobTransfer, ErrBlobCleanup) and the
// same recovery contract.
//
// Delegated elsewhere:
//   - Blob integrity (decompression + hash verify) → CompressingStorage.
//   - Isolation → storage decorator or the composition root.
//   - Write durability → the storage adapter.
type Puller struct {
	from   ports.StorageRepository
	to     ports.StorageRepository
	runner ports.BlobRunner
	onPlan PlanFn
}

// NewPuller wires a Puller. Pull reads from `from` and writes to `to`,
// both satisfying ports.StorageRepository. runner schedules per-blob
// transfer concurrency.
func NewPuller(from, to ports.StorageRepository, runner ports.BlobRunner) *Puller {
	return &Puller{from: from, to: to, runner: runner}
}

// OnPlan registers a callback that fires once per Pull, after the ref is
// fetched and before the first blob streams. Wired by the composition
// root to bus.Publish; tests pass a slice-appending closure. nil-callback
// is the default — Pull stays silent. Idempotent: a second call replaces
// the prior callback.
func (p *Puller) OnPlan(fn PlanFn) { p.onPlan = fn }

// Pull materialises the ref identified by id at the destination:
// refs/{id}.json plus every referenced objects/{hash}. The destination
// ref is written LAST — only after every referenced blob has landed — so
// a crashed or failed pull leaves no live ref on disk. This mirrors
// Commit's ref-last barrier: retention cannot observe a ref pointing at
// missing blobs, and apply cannot be invoked against a half-pulled ref.
func (p *Puller) Pull(ctx context.Context, id domain.RefID) error {
	ref, raw, err := p.fetchRef(ctx, id)
	if err != nil {
		return err
	}
	items, pathByHash := collectHashes(ref.Objects)
	if p.onPlan != nil {
		p.onPlan(ritual.PlanInfo{
			Operation:  "pull",
			BytesTotal: sumSizes(ref.Objects),
			FilesTotal: len(items),
		})
	}
	err = p.runner.Run(ctx, items, func(ctx context.Context, hash string) error {
		if err := transferBlob(ctx, p.from, p.to, blobKey(hash)); err != nil {
			return fmt.Errorf("pull %s: blob %s (%s): %w", id, hash, pathByHash[hash], err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := p.to.PutStream(ctx, refKey(id), bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("pull %s: commit ref: %w", id, err)
	}
	return nil
}

func (p *Puller) fetchRef(ctx context.Context, id domain.RefID) (*domain.Ref, []byte, error) {
	rc, err := p.from.GetStream(ctx, refKey(id))
	if err != nil {
		return nil, nil, fmt.Errorf("pull %s: fetch ref: %w", id, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("pull %s: read ref: %w", id, err)
	}

	ref := &domain.Ref{}
	if err := json.Unmarshal(raw, ref); err != nil {
		return nil, nil, fmt.Errorf("pull %s: parse ref: %w", id, err)
	}
	return ref, raw, nil
}

// sumSizes totals the byte size of every object in a ref. Same-hash
// duplicates count once because the unique-blob view is what hits the
// network — the file count surfaced via FilesTotal matches.
func sumSizes(objects map[string]domain.Object) int64 {
	seen := make(map[string]struct{}, len(objects))
	var total int64
	for _, obj := range objects {
		if _, ok := seen[obj.Hash]; ok {
			continue
		}
		seen[obj.Hash] = struct{}{}
		total += obj.Size
	}
	return total
}

func refKey(id domain.RefID) string { return "refs/" + string(id) + ".json" }
func blobKey(hash string) string    { return "objects/" + hash }

// collectHashes flattens a ref.Objects map into BlobItems (Key=hash,
// Weight=size) plus a reverse index hash → first-seen path. The reverse
// index preserves the original error-message context (which path triggered
// the blob upload) when the runner reports per-hash failures. Map iteration
// order is randomised; schedulers re-order via Weight anyway. Same-hash
// duplicates collapse to one item.
func collectHashes(objects map[string]domain.Object) ([]ports.BlobItem, map[string]string) {
	items := make([]ports.BlobItem, 0, len(objects))
	pathByHash := make(map[string]string, len(objects))
	for path, obj := range objects {
		if _, seen := pathByHash[obj.Hash]; seen {
			continue
		}
		pathByHash[obj.Hash] = path
		items = append(items, ports.BlobItem{Key: obj.Hash, Weight: obj.Size})
	}
	return items, pathByHash
}

var _ ports.Puller = (*Puller)(nil)
