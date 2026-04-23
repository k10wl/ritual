package refs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"

	"github.com/bmatcuk/doublestar/v4"
)

// Committer snapshots a workdir into a content-addressed
// blob store plus a new refs/{id}.json, per §Commit — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md.
//
// ports.CommitOpts lives in `ports` so Committer satisfies `ports.Committer` for
// the build-time assertion at the bottom of this file.
//
// Amend semantics (MVP): deletes the old draft's ref JSON from the blob
// store after writing the new ref, then invokes the injected local GC
// closure to sweep blobs the superseded draft was exclusively
// referencing — per §Retention and GC trigger `amend → localGC` in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md. The
// remote-presence check (HeadObject 404) belongs to Pusher and is out
// of scope here.
//
// "From workdir, to blobs" mirrors Puller's from/to semantics: workdir is
// the read side (files on disk), blobs is the write side (objects/{hash}
// + refs/{id}.json keyspace). The scanner walks the workdir filesystem
// recursively and supplies per-file Hash+Size so Committer never rehashes
// inline.
//
// MVP scope (see per-simplification notes below):
//   - Serial walk (spec calls for 10 workers).
//   - Full walk only; mtime-based incremental walk deferred.
//   - Live-ticker `save-all flush` lives in the ticker loop (caller
//     side); Committer does not own Minecraft console interaction.
//     Boot-time `save-off` already fires once at readiness inside
//     the running-stage strategy and stays off for the session.
//   - tick-mutex not wired; single-commit tests only.
//   - Amend only deletes the old local draft — no remote HeadObject check
//     (Pusher's concern).
//   - Local GC after amend is delegated to an injected closure
//     (`WithLocalGC`); when the composition root leaves it unwired the
//     sweep is a no-op and orphan blobs wait for the next GC cycle.
//
// Empty-match policy: a commit whose Targets match zero workdir files
// returns an error. A zero-object ref is almost certainly a glob bug and
// masking it as a valid snapshot hides data loss.
type Committer struct {
	scanner ports.DirectoryScanner
	workdir ports.StorageRepository
	blobs   ports.StorageRepository
	now     func() time.Time
	localGC func(ctx context.Context) error
}

// NewCommitter wires a Committer. Commit walks the workdir via scanner (which
// owns the recursive walk + xxhash), reads bytes to upload through workdir,
// and writes objects/{hash} + refs/{id}.json to blobs. The composition root
// decides whether blobs is wrapped with CompressingStorage (normal production
// path).
func NewCommitter(scanner ports.DirectoryScanner, workdir, blobs ports.StorageRepository) *Committer {
	return &Committer{scanner: scanner, workdir: workdir, blobs: blobs, now: time.Now}
}

// WithClock overrides the internal clock used for RefID minting. Tests use
// this for deterministic timestamps; production code leaves the default.
func (c *Committer) WithClock(now func() time.Time) *Committer {
	c.now = now
	return c
}

// WithLocalGC wires the orphan-blob sweeper invoked immediately after an
// amend deletes the superseded draft. Composition root passes
// `refs.NewCollector(localStorage).Collect`; leaving it unset keeps the
// amend path a no-op for GC and defers cleanup to the next push-chain
// cycle.
func (c *Committer) WithLocalGC(fn func(ctx context.Context) error) *Committer {
	c.localGC = fn
	return c
}

// Commit snapshots the workdir into the blob store and writes a new ref.
// Returns the new RefID.
func (c *Committer) Commit(ctx context.Context, opts ports.CommitOpts) (domain.RefID, error) {
	if len(opts.Targets) == 0 {
		return "", fmt.Errorf("refs.Committer.Commit: at least one target glob is required")
	}

	matched, err := c.walkMatches(ctx, opts.Targets)
	if err != nil {
		return "", err
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("refs.Committer.Commit: no workdir files matched targets %v — zero-object ref rejected", opts.Targets)
	}

	objects, err := c.storeBlobs(ctx, matched)
	if err != nil {
		return "", err
	}

	id := domain.RefID(c.now().UTC().Format(domain.RefIDFormat))
	parent, err := c.resolveParent(ctx, opts)
	if err != nil {
		return "", err
	}

	ref := &domain.Ref{
		Timestamp:     id,
		Parent:        parent,
		RitualVersion: "2.1.0",
		Targets:       opts.Targets,
		Objects:       objects,
	}
	err = c.writeRef(ctx, ref)
	if err != nil {
		return "", err
	}

	err = c.finalizeAmend(ctx, opts.Amend, id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// finalizeAmend deletes the superseded draft ref and sweeps its
// now-orphan blobs via the injected local GC closure — per
// §Retention and GC trigger `amend → localGC`. Fresh commits short-
// circuit; amend with unset localGC still deletes the draft (GC-as-
// no-op).
func (c *Committer) finalizeAmend(ctx context.Context, amend, current domain.RefID) error {
	if amend == "" || amend == current {
		return nil
	}
	err := c.blobs.Delete(ctx, refKey(amend))
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: delete amended draft %s: %w", amend, err)
	}
	if c.localGC == nil {
		return nil
	}
	err = c.localGC(ctx)
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: local GC after amend %s: %w", amend, err)
	}
	return nil
}

// walkMatches delegates the workdir walk to the injected DirectoryScanner —
// the scanner owns recursion and per-file xxhashing — then filters the
// resulting map by any Targets glob via doublestar.Match.
func (c *Committer) walkMatches(ctx context.Context, targets []string) (map[string]domain.FileEntry, error) {
	scanned, err := c.scanner.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("refs.Committer.Commit: scan workdir: %w", err)
	}
	matched := make(map[string]domain.FileEntry, len(scanned))
	for path, entry := range scanned {
		hit, err := anyGlobMatches(targets, path)
		if err != nil {
			return nil, err
		}
		if !hit {
			continue
		}
		matched[path] = entry
	}
	return matched, nil
}

// storeBlobs uploads each matched file's bytes into the content-addressed
// blob store (skipping hashes already present) and builds the ref's
// (path → Object) map from the scanner's Hash+Size.
func (c *Committer) storeBlobs(ctx context.Context, matched map[string]domain.FileEntry) (map[string]domain.Object, error) {
	objects := make(map[string]domain.Object, len(matched))
	for path, entry := range matched {
		err := c.storeOneBlob(ctx, path, entry)
		if err != nil {
			return nil, err
		}
		objects[path] = domain.Object{Hash: entry.Hash, Size: entry.Size}
	}
	return objects, nil
}

func (c *Committer) storeOneBlob(ctx context.Context, path string, entry domain.FileEntry) error {
	key := blobKey(entry.Hash)
	present, err := c.blobs.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: exists check %s: %w", key, err)
	}
	if present {
		return nil
	}

	rc, err := c.workdir.GetStream(ctx, path)
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: read %s: %w", path, err)
	}
	putErr := c.blobs.PutStream(ctx, key, rc)
	closeErr := rc.Close()
	if putErr != nil {
		return fmt.Errorf("refs.Committer.Commit: write blob %s: %w", key, putErr)
	}
	if closeErr != nil {
		return fmt.Errorf("refs.Committer.Commit: close %s: %w", path, closeErr)
	}
	return nil
}

// resolveParent honours the amend rule: the new ref inherits the old
// draft's parent (no chain lengthening). Fresh commits use opts.Parent
// verbatim.
func (c *Committer) resolveParent(ctx context.Context, opts ports.CommitOpts) (domain.RefID, error) {
	if opts.Amend == "" {
		return opts.Parent, nil
	}
	rc, err := c.blobs.GetStream(ctx, refKey(opts.Amend))
	if err != nil {
		return "", fmt.Errorf("refs.Committer.Commit: load amended draft %s: %w", opts.Amend, err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("refs.Committer.Commit: read amended draft %s: %w", opts.Amend, err)
	}
	old := &domain.Ref{}
	err = json.Unmarshal(raw, old)
	if err != nil {
		return "", fmt.Errorf("refs.Committer.Commit: parse amended draft %s: %w", opts.Amend, err)
	}
	return old.Parent, nil
}

// writeRef marshals and streams the new ref JSON into the blob store under
// refs/{id}.json.
func (c *Committer) writeRef(ctx context.Context, ref *domain.Ref) error {
	body, err := json.Marshal(ref)
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: marshal ref %s: %w", ref.Timestamp, err)
	}
	err = c.blobs.PutStream(ctx, refKey(ref.Timestamp), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("refs.Committer.Commit: write ref %s: %w", ref.Timestamp, err)
	}
	return nil
}

func anyGlobMatches(globs []string, key string) (bool, error) {
	for _, g := range globs {
		ok, err := doublestar.Match(g, key)
		if err != nil {
			return false, fmt.Errorf("refs.Committer.Commit: invalid glob %q: %w", g, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

var _ ports.Committer = (*Committer)(nil)
