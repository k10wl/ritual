package refs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/cespare/xxhash/v2"
)

// Applier materialises a ref's object map into the workdir.
// Blobs are read from the content-addressed blob store (already hydrated by
// Puller); files are written under workdir at the paths declared by the ref.
//
// ACID responsibilities owned by Applier (see §Apply — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md):
//   - Per-file placement: for every (path, Object) in ref.Objects, the blob
//     bytes land at workdir `<path>`.
//   - Idempotent skip: paths already present in workdir are not re-read from
//     blobs. (MVP skip gate is `workdir.Exists(path)`; full streaming xxhash
//     verify is a future enhancement — see §Integrity Model.)
//   - Scope invariant: walk(workdir) ∩ ref.Targets ⊆ ref.Objects. Files
//     inside Targets but no longer referenced are pruned. Files outside
//     Targets are never touched.
//
// Delegated elsewhere:
//   - Blob decompression + content-hash verify → CompressingStorage.
//   - Stale `.ritualapply.tmp` sweep, Windows rename guards, same-volume
//     tmp — deferred past MVP.
//   - Parallel worker pool across files — deferred past MVP; Apply is serial.
//   - Session lock + Minecraft-quiesce preconditions — orchestrator concern.
type Applier struct {
	blobs   ports.StorageRepository
	workdir ports.StorageRepository
}

// NewApplier wires an Applier. Blobs are read from `blobs` (the local
// content-addressed store populated by Puller, optionally wrapped with
// CompressingStorage); files are written to and pruned from `workdir`.
// Paths in the ref are root-relative — workdir IS the root, whatever it
// points at. Targets globs are interpreted relative to that same root.
//
// The ref itself also lives in `blobs` under `refs/{id}.json` — same layout
// as Pull's destination.
func NewApplier(blobs, workdir ports.StorageRepository) *Applier {
	return &Applier{blobs: blobs, workdir: workdir}
}

// Apply materialises every (path, Object) in the ref identified by id into
// workdir, then prunes workdir paths inside ref.Targets that the ref no
// longer references.
func (a *Applier) Apply(ctx context.Context, id domain.RefID) error {
	ref, err := a.loadRef(ctx, id)
	if err != nil {
		return err
	}
	for filePath, obj := range ref.Objects {
		err := a.placeObject(ctx, filePath, obj)
		if err != nil {
			return fmt.Errorf("apply %s: place %s (hash %s): %w", id, filePath, obj.Hash, err)
		}
	}
	err = a.prune(ctx, ref)
	if err != nil {
		return fmt.Errorf("apply %s: prune: %w", id, err)
	}
	return nil
}

func (a *Applier) loadRef(ctx context.Context, id domain.RefID) (*domain.Ref, error) {
	rc, err := a.blobs.GetStream(ctx, refKey(id))
	if err != nil {
		return nil, fmt.Errorf("apply %s: fetch ref: %w", id, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("apply %s: read ref: %w", id, err)
	}

	ref := &domain.Ref{}
	err = json.Unmarshal(raw, ref)
	if err != nil {
		return nil, fmt.Errorf("apply %s: parse ref: %w", id, err)
	}
	return ref, nil
}

func (a *Applier) placeObject(ctx context.Context, filePath string, obj domain.Object) error {
	skip, err := a.existingMatchesHash(ctx, filePath, obj.Hash)
	if err != nil {
		return fmt.Errorf("workdir skip-gate: %w", err)
	}
	if skip {
		return nil
	}

	rc, err := a.blobs.GetStream(ctx, blobKey(obj.Hash))
	if err != nil {
		return fmt.Errorf("fetch blob: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read blob: %w", err)
	}

	err = a.workdir.PutStream(ctx, filePath, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("write workdir: %w", err)
	}
	return nil
}

// existingMatchesHash implements §Apply step 3's skip predicate:
// `exists(workdir/<path>) AND xxhash(workdir/<path>) == ref.Object.Hash`.
// Returns true only when both halves hold — a mismatched file is treated
// as missing and gets overwritten by the blob write path.
func (a *Applier) existingMatchesHash(ctx context.Context, filePath, expectedHash string) (bool, error) {
	present, err := a.workdir.Exists(ctx, filePath)
	if err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}
	if !present {
		return false, nil
	}
	rc, err := a.workdir.GetStream(ctx, filePath)
	if err != nil {
		return false, fmt.Errorf("read existing: %w", err)
	}
	defer rc.Close()
	hasher := xxhash.New()
	_, err = io.Copy(hasher, rc)
	if err != nil {
		return false, fmt.Errorf("hash existing: %w", err)
	}
	return fmt.Sprintf("%016x", hasher.Sum64()) == expectedHash, nil
}

func (a *Applier) prune(ctx context.Context, ref *domain.Ref) error {
	keys, err := a.workdir.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list workdir: %w", err)
	}
	for _, key := range keys {
		_, referenced := ref.Objects[key]
		if referenced {
			continue
		}
		if !matchesAnyTarget(ref.Targets, key) {
			continue
		}
		err := a.workdir.Delete(ctx, key)
		if err != nil {
			return fmt.Errorf("delete stale %s: %w", key, err)
		}
	}
	return nil
}

// matchesAnyTarget returns true when candidate is inside any of the glob
// patterns in targets. MVP recognises two shapes:
//
//   - `<prefix>/**` — match everything under prefix/.
//   - literal path or `path.Match`-compatible single-segment wildcards.
//
// The spec calls for bmatcuk/doublestar/v4 to handle full doublestar syntax;
// adopting that dep is a post-MVP task. For `worlds/**` (the only pattern
// exercised today) this matcher is equivalent.
func matchesAnyTarget(targets []string, candidate string) bool {
	for _, pattern := range targets {
		if matchesTarget(pattern, candidate) {
			return true
		}
	}
	return false
}

func matchesTarget(pattern, candidate string) bool {
	prefix, ok := strings.CutSuffix(pattern, "/**")
	if ok {
		return candidate == prefix || strings.HasPrefix(candidate, prefix+"/")
	}
	if pattern == "**" {
		return true
	}
	matched, _ := path.Match(pattern, candidate)
	return matched
}

var _ ports.Applier = (*Applier)(nil)
