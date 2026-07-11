package refs

import (
	"context"
	"encoding/json"
	"fmt"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// sweepSuperseded deletes every refs/*.json on `store` whose Parent
// matches newRef.Parent AND whose Timestamp is strictly older than
// newRef. Unparseable refs are skipped — the object-GC pass handles
// their orphan blobs.
//
// Shared by Committer (local-side sweep on amend) and Pusher (remote-
// side mirror after upload). Same predicate on both sides keeps the
// design-log/016 single-draft-per-session invariant honest end-to-end.
func sweepSuperseded(ctx context.Context, store ports.StorageRepository, newRef *domain.Ref) error {
	keys, err := store.List(ctx, "refs/")
	if err != nil {
		return fmt.Errorf("list refs for sweep: %w", err)
	}
	newKey := refKey(newRef.Timestamp)
	for _, key := range keys {
		if key == newKey {
			continue
		}
		sibling, err := readRefAt(ctx, store, key)
		if err != nil {
			continue
		}
		if sibling.Parent != newRef.Parent {
			continue
		}
		if sibling.Timestamp >= newRef.Timestamp {
			continue
		}
		if err := store.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete superseded sibling %s: %w", sibling.Timestamp, err)
		}
	}
	return nil
}

func readRefAt(ctx context.Context, store ports.StorageRepository, key string) (*domain.Ref, error) {
	rc, err := store.GetStream(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	ref := &domain.Ref{}
	if err := json.NewDecoder(rc).Decode(ref); err != nil {
		return nil, err
	}
	return ref, nil
}
