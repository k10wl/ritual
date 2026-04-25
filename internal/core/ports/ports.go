package ports

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"ritual/internal/core/domain"
)

// StorageRepository defines the interface for storage operations.
// Embeds fmt.Stringer so adapters self-describe in observability events
// (e.g. "fs::./worlds", "r2::bucket/prefix").
//
// Delete semantics: tree-delete. If key matches a single object, that object
// is removed. If key is a prefix of multiple objects (or a directory on local
// FS), the entire subtree is removed in one logical operation.
//
// V2 streaming methods (`GetStream`, `PutStream`, `Exists`) are the migration
// target described in docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md
// (Storage V2 Migration). V1 buffered methods (`Get`, `Put`, `Rename`) are
// deprecated and scheduled for removal in STORAGE-16.
type StorageRepository interface {
	fmt.Stringer

	// GetStream opens an object for streaming read. Caller must Close the body.
	GetStream(ctx context.Context, key string) (io.ReadCloser, error)
	// PutStream writes body under key. Body is a plain io.Reader: adapters
	// that need rewind (R2/S3 retry middleware) type-assert to io.Seeker and
	// error loudly if the caller broke the contract. In real composition the
	// only caller that reaches R2.PutStream is Push, whose source is FS
	// (*os.File — natively seekable), so the assertion never trips.
	PutStream(ctx context.Context, key string, body io.Reader) error
	// Exists reports whether key is present without transferring bytes.
	Exists(ctx context.Context, key string) (bool, error)

	// Deprecated: use GetStream. Removed in STORAGE-16.
	Get(ctx context.Context, key string) ([]byte, error)
	// Deprecated: use PutStream. Removed in STORAGE-16.
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Copy(ctx context.Context, sourceKey string, destKey string) error
	// Deprecated: no V2 equivalent. Removed in STORAGE-16.
	Rename(ctx context.Context, sourceKey string, destKey string) error
}

// Puller fetches a ref and every blob it references from one storage side
// into another. Implemented by refs.Puller. See §Pull — ACID in
// docs/superpowers/specs/2026-04-19-fast-sync-v2.1-design.md.
type Puller interface {
	Pull(ctx context.Context, id domain.RefID) error
}

// Applier materialises a ref into a workdir (instance tree), skipping files
// already present and pruning paths out-of-ref but in-scope. See §Apply —
// ACID in the v2.1 spec.
type Applier interface {
	Apply(ctx context.Context, id domain.RefID) error
}

// CommitOpts parameterises a Commit call. Parent is the prior HEAD's ID (empty
// for the initial commit). Targets is the glob set recorded on the new ref;
// at least one glob is required. Amend, when non-empty, replaces an existing
// local draft whose contents are NOT yet on remote.
type CommitOpts struct {
	Parent  domain.RefID
	Targets []string
	Amend   domain.RefID
}

// Committer snapshots a workdir into a content-addressed blob store plus a
// new ref. See §Commit — ACID in the v2.1 spec.
type Committer interface {
	Commit(ctx context.Context, opts CommitOpts) (domain.RefID, error)
}

// Pusher uploads a ref and every blob it references from one storage side
// to another. See §Push — ACID in the v2.1 spec.
type Pusher interface {
	Push(ctx context.Context, id domain.RefID) error
}

// Collector sweeps orphan blobs — every `objects/*` whose hash is not
// referenced by any surviving `refs/*.json`. Retention (which refs to
// keep) is a separate concern: the caller deletes refs first, then runs
// Collect to reap their now-unreferenced blobs. See §Retention and GC
// in the v2.1 spec.
type Collector interface {
	Collect(ctx context.Context) error
}

// CmdBuilder lazily creates the *exec.Cmd for the server process.
// Caller provides IO interfaces for stdin/stdout wiring. Builder assigns
// cmd.Stdin = stdin, cmd.Stdout = stdout, cmd.Stderr = stdout (merged).
type CmdBuilder interface {
	Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error)
}

// ReadinessCheck waits until the server is ready to accept connections.
// Implementation decides the mechanism (TCP dial, HTTP, etc).
type ReadinessCheck interface {
	Wait(ctx context.Context) error
}

// DirectoryScanner produces a file map keyed by relative path with content
// hash + size for every file in the worlds directory. Implementations
// determine scanning strategy (full walk vs mtime-filtered).
type DirectoryScanner interface {
	Scan(ctx context.Context) (map[string]domain.FileEntry, error)
}

// BlobItem is one unit of work handed to a BlobRunner. Key is the opaque
// identifier passed back to fn (usually an objects/{hash} key or a workdir
// path). Weight is a per-item ordering hint — typically Object.Size — that
// schedulers MAY use to optimise scheduling order; impls that don't care
// (SerialRunner) MUST ignore it. Callers supply data, not policy.
type BlobItem struct {
	Key    string
	Weight int64
}

// BlobRunner schedules per-item work across a slice of items. Implementations
// decide concurrency policy (serial, bounded pool, ...) and ordering policy
// (input order, weight-desc, ...). The function is invoked once per item key;
// first non-nil error cancels remaining work and is returned.
type BlobRunner interface {
	Run(ctx context.Context, items []BlobItem, fn func(ctx context.Context, key string) error) error
}

