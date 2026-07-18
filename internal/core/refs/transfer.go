package refs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"ritual/internal/core/ports"
)

// ErrBlobTransfer classifies a failure while streaming a blob between two
// storages. Pull and Push share this sentinel — both verbs mirror bytes
// across the same boundary, so callers filter on one category.
var ErrBlobTransfer = errors.New("refs: blob transfer failed")

// ErrBlobCleanup classifies a failed Delete during scrub. The destination
// may still hold stale or partial bytes; rerunning the verb alone may not
// resolve it. Surface for operator attention — never suppress.
var ErrBlobCleanup = errors.New("refs: blob cleanup failed")

// transferBlob mirrors a single content-addressed key from one storage
// to another. Behaviour:
//
//   - Stream source → destination. The pre-flight List in
//     collectKnownHashes is the single filter (design-log/025); a concurrent
//     landing between List and transfer results in a byte-identical
//     duplicate upload, which the destination accepts as a no-op.
//   - On any GetStream/PutStream/Close failure, scrub the destination
//     under `key` so the next call starts fresh.
//     Scrub Delete that hits fs.ErrNotExist is tolerated (nothing landed).
//   - Errors classify via ErrBlobTransfer; cleanup failures add
//     ErrBlobCleanup through errors.Join so callers can distinguish
//     transport failure from recovery failure.
//
// Direction-agnostic — the caller decides which side is remote and which
// is local. Used by both Puller (remote→local) and Pusher (local→remote).
func transferBlob(ctx context.Context, from, to ports.StorageRepository, key string) error {
	rc, err := from.GetStream(ctx, key)
	if err != nil {
		return fmt.Errorf("%w (%s): %w", ErrBlobTransfer, key, err)
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

	transfer := fmt.Errorf("%w (%s): %w", ErrBlobTransfer, key, writeErr)
	if delErr == nil {
		return transfer
	}
	cleanup := fmt.Errorf("%w (%s): %w", ErrBlobCleanup, key, delErr)
	return errors.Join(transfer, cleanup)
}
