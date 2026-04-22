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
	"github.com/cespare/xxhash/v2"
)

// Committer snapshots a workdir into a content-addressed
// blob store plus a new refs/{id}.json, per §Commit — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md.
//
// ports.CommitOpts lives in `ports` so Committer satisfies `ports.Committer` for
// the build-time assertion at the bottom of this file.
//
// Amend semantics (MVP): deletes the old draft's ref JSON from the blob
// store after writing the new ref. The remote-presence check (HeadObject
// 404) belongs to Pusher and is out of scope here.
//
// "From workdir, to blobs" mirrors Puller's from/to semantics: workdir is
// the read side (files on disk), blobs is the write side (objects/{hash}
// + refs/{id}.json keyspace).
//
// MVP scope (see per-simplification notes below):
//   - Serial walk (spec calls for 10 workers).
//   - Full walk only; mtime-based incremental walk deferred.
//   - Quiescer port not wired; live-ticker save-off/save-on handled
//     elsewhere for now.
//   - tick-mutex not wired; single-commit tests only.
//   - Amend only deletes the old local draft — no remote HeadObject check
//     (Pusher's concern).
//
// Empty-match policy: a commit whose Targets match zero workdir files
// returns an error. A zero-object ref is almost certainly a glob bug and
// masking it as a valid snapshot hides data loss.
type Committer struct {
	workdir ports.StorageRepository
	blobs   ports.StorageRepository
	now     func() time.Time
}

// NewCommitter wires a Committer. Commit reads from workdir and writes to
// blobs. The composition root decides whether blobs is wrapped with
// CompressingStorage (normal production path).
func NewCommitter(workdir, blobs ports.StorageRepository) *Committer {
	return &Committer{workdir: workdir, blobs: blobs, now: time.Now}
}

// WithClock overrides the internal clock used for RefID minting. Tests use
// this for deterministic timestamps; production code leaves the default.
func (c *Committer) WithClock(now func() time.Time) *Committer {
	c.now = now
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

	if opts.Amend != "" && opts.Amend != id {
		err = c.blobs.Delete(ctx, refKey(opts.Amend))
		if err != nil {
			return "", fmt.Errorf("refs.Committer.Commit: delete amended draft %s: %w", opts.Amend, err)
		}
	}
	return id, nil
}

// walkMatches enumerates workdir keys and keeps those matching any target
// glob via doublestar semantics.
func (c *Committer) walkMatches(ctx context.Context, targets []string) ([]string, error) {
	keys, err := c.workdir.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("refs.Committer.Commit: list workdir: %w", err)
	}
	matched := make([]string, 0, len(keys))
	for _, key := range keys {
		hit, err := anyGlobMatches(targets, key)
		if err != nil {
			return nil, err
		}
		if !hit {
			continue
		}
		matched = append(matched, key)
	}
	return matched, nil
}

// storeBlobs hashes each matched workdir file's raw bytes with xxhash64,
// writes objects/{hash} to the blob store when absent, and returns the
// (path → Object) map for the ref.
func (c *Committer) storeBlobs(ctx context.Context, matched []string) (map[string]domain.Object, error) {
	objects := make(map[string]domain.Object, len(matched))
	for _, path := range matched {
		obj, err := c.storeOneBlob(ctx, path)
		if err != nil {
			return nil, err
		}
		objects[path] = obj
	}
	return objects, nil
}

func (c *Committer) storeOneBlob(ctx context.Context, path string) (domain.Object, error) {
	rc, err := c.workdir.GetStream(ctx, path)
	if err != nil {
		return domain.Object{}, fmt.Errorf("refs.Committer.Commit: read %s: %w", path, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return domain.Object{}, fmt.Errorf("refs.Committer.Commit: drain %s: %w", path, err)
	}

	h := xxhash.New()
	_, _ = h.Write(raw)
	hash := fmt.Sprintf("%016x", h.Sum64())
	key := blobKey(hash)

	present, err := c.blobs.Exists(ctx, key)
	if err != nil {
		return domain.Object{}, fmt.Errorf("refs.Committer.Commit: exists check %s: %w", key, err)
	}
	if !present {
		err = c.blobs.PutStream(ctx, key, bytes.NewReader(raw))
		if err != nil {
			return domain.Object{}, fmt.Errorf("refs.Committer.Commit: write blob %s: %w", key, err)
		}
	}
	return domain.Object{Hash: hash, Size: int64(len(raw))}, nil
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
