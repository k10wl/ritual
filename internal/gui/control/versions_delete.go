package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ritual/internal/core/domain"
)

// deleteTimeout bounds the local refs/+GC delete so a slow disk can't hang the
// Versions screen. A local refs delete + a Collector mark-sweep over objects/
// is fast (sub-second on healthy disks); ten seconds is a comfortable ceiling.
const deleteTimeout = 10 * time.Second

// LocalDeleter is the per-version delete + GC closure (design-log/045 §A). It
// deletes refs/<id>.json from local storage, then runs the orphan-blob
// collector. Returns nil on success; any error surfaces to the UI as a row
// retry prompt (no dial takeover — delete stays quiet).
type LocalDeleter func(ctx context.Context, id domain.RefID) error

// SettingsClearer clears settings.LoadedRefID when the deleted ref is the one
// the workdir was anchored to (design-log/044 + /045 §Q2). The lister's
// fallback then degrades the "current" badge to IsHead instead of pointing at
// a deleted ghost. Best-effort — a save failure here only degrades the badge,
// it does not roll back the delete.
type SettingsClearer func() error

// DeleteLocalVersion deletes a local version ref + its orphan blobs
// (design-log/045 §A). The user is allowed to delete any row including the
// loaded one and HEAD — the confirm copy spells out the sharp edge before
// the call lands.
//
// Validation matches Restore: empty + malformed ids are rejected before any
// storage touch. A nil deleter is a wiring bug (returns an explicit error
// rather than silently no-oping). If the deleted id equals
// settings.LoadedRefID, the loaded pointer is cleared so the lister's
// fallback to IsHead kicks in — keeps the "current" badge honest.
func (c *ControlService) DeleteLocalVersion(refID string) error {
	if refID == "" {
		return errors.New("delete local version: empty ref id")
	}
	if _, err := time.Parse(domain.RefIDFormat, refID); err != nil {
		return fmt.Errorf("delete local version: invalid ref id %q: %w", refID, err)
	}
	if c.localDeleter == nil {
		return errors.New("delete local version: deleter not wired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	if err := c.localDeleter(ctx, domain.RefID(refID)); err != nil {
		return fmt.Errorf("delete local version %s: %w", refID, err)
	}
	// If the deleted ref was the loaded one, clear the pointer so the lister
	// falls back to IsHead. Read settings.LoadedRefID directly via the loaded-
	// id closure to keep this method free of settings I/O.
	if c.loadedRefID != nil && c.loadedRefID() == domain.RefID(refID) && c.clearLoadedRefID != nil {
		_ = c.clearLoadedRefID() // best-effort; badge fallback still honest if this fails
	}
	c.invalidateStats() // delete dropped a ref + GC'd orphans → on-disk number changed
	return nil
}
