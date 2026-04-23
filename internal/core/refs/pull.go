package refs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

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
// Delegated elsewhere:
//   - Blob integrity (decompression + hash verify) → CompressingStorage.
//   - Session lock → orchestrator.
//   - FlushFileBuffers durability → FSRepository.
type Puller struct {
	from ports.StorageRepository
	to   ports.StorageRepository
}

// NewPuller wires a Puller. Pull reads from `from` and writes to `to`.
// In normal composition `from` is remote R2 and `to` is local FS, but
// Puller is direction-agnostic — swap them for a local-to-local clone
// and the verb still works. The composition root decides whether either
// side is wrapped with CompressingStorage.
func NewPuller(from, to ports.StorageRepository) *Puller {
	return &Puller{from: from, to: to}
}

// Pull materialises the ref identified by id at the destination:
// refs/{id}.json plus every referenced objects/{hash}.
func (p *Puller) Pull(ctx context.Context, id domain.RefID) error {
	ref, err := p.fetchRef(ctx, id)
	if err != nil {
		return err
	}
	for path, obj := range ref.Objects {
		err := p.fetchBlob(ctx, obj.Hash)
		if err != nil {
			return fmt.Errorf("pull %s: blob %s (%s): %w", id, obj.Hash, path, err)
		}
	}
	return nil
}

func (p *Puller) fetchRef(ctx context.Context, id domain.RefID) (*domain.Ref, error) {
	key := refKey(id)
	rc, err := p.from.GetStream(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("pull %s: fetch ref: %w", id, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("pull %s: read ref: %w", id, err)
	}

	err = p.to.PutStream(ctx, key, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("pull %s: write ref: %w", id, err)
	}

	ref := &domain.Ref{}
	err = json.Unmarshal(raw, ref)
	if err != nil {
		_ = p.to.Delete(ctx, key)
		return nil, fmt.Errorf("pull %s: parse ref (destination copy deleted, next pull refetches): %w", id, err)
	}
	return ref, nil
}

// fetchBlob materialises a single objects/{hash} at the destination. Flow
// is a single linear pass — no retries inside refs, no loops, no tmp
// files. When the blob is already present the Exists-gate skips; when
// the download itself fails, partial bytes are scrubbed so the next Pull
// sees Exists == false. Hash verification is deferred to Apply per spec
// §Integrity Model (CompressingStorage.GetStream catches mismatches at
// read time), so Puller treats existing bytes as trustworthy: the spec's
// recovery contract is "corrupt local blob detected on next read →
// delete + refetch", which Apply executes.
//
// Failure classification uses the package sentinels. ErrBlobDownload
// wraps any Get/Put/Close failure; ErrBlobCleanup wraps any Delete
// failure during scrub. Both surface through errors.Join so errors.Is /
// errors.As reach each concrete cause. Cleanup failures are never
// hidden — only fs.ErrNotExist on the scrub Delete is tolerated (no
// bytes landed → nothing to remove).
func (p *Puller) fetchBlob(ctx context.Context, hash string) error {
	key := blobKey(hash)

	present, err := p.to.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("exists %s: %w", key, err)
	}
	if present {
		return nil
	}

	return downloadBlob(ctx, p.from, p.to, key)
}

// downloadBlob streams source → destination, then scrubs the destination
// on any failure so the next Pull starts from a clean slate. Single
// attempt. Retries happen at the verb level — the caller reruns Pull.
func downloadBlob(ctx context.Context, from, to ports.StorageRepository, key string) error {
	rc, err := from.GetStream(ctx, key)
	if err != nil {
		return fmt.Errorf("%w (%s): %w", ErrBlobDownload, key, err)
	}
	putErr := to.PutStream(ctx, key, rc)
	closeErr := rc.Close()
	writeErr := errors.Join(putErr, closeErr)
	if writeErr == nil {
		return nil
	}

	delErr := to.Delete(ctx, key)
	if errors.Is(delErr, fs.ErrNotExist) {
		delErr = nil
	}

	download := fmt.Errorf("%w (%s): %w", ErrBlobDownload, key, writeErr)
	if delErr == nil {
		return download
	}
	cleanup := fmt.Errorf("%w (%s): %w", ErrBlobCleanup, key, delErr)
	return errors.Join(download, cleanup)
}

func refKey(id domain.RefID) string { return "refs/" + string(id) + ".json" }
func blobKey(hash string) string    { return "objects/" + hash }

var _ ports.Puller = (*Puller)(nil)
