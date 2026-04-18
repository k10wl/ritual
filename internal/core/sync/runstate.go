// Package sync orchestrates one transfer from a source storage to a
// destination storage as a chain of strategies. The same engine runs both
// upload (local→remote) and download (remote→local) — direction is wiring,
// not type.
package sync

import (
	"fmt"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// Direction is the cleanup mode of a sync run. It is a wiring concern, not
// an event field — orphan cleanup deletes from dst while ghost cleanup
// deletes from dst as well, but in the context of a download direction.
// Encoded here so a single engine wires both directions without inspecting
// store identity.
type Direction int

const (
	DirectionUpload   Direction = iota // dst is the upstream side; orphan cleanup applies
	DirectionDownload                  // dst is the local side; ghost cleanup applies
)

// RunState is shared across strategies for a single sync run. Strategies
// produce fields top-to-bottom: Scanning fills SrcMap+DstMap, Planning
// fills Diff/TransferBytes/DeleteBytes, StageDirInit fills StagingID/
// StagingPath, the rest read those fields.
type RunState struct {
	// Wired at construction.
	Src       ports.StorageRepository
	Dst       ports.StorageRepository
	SrcLabel  string
	DstLabel  string
	Direction Direction
	Bus       ports.EventBus

	// Manifest plumbing — Scanning loads dst manifest via these.
	DstManifestRead func() (map[string]domain.FileEntry, error)

	// Started is set by Scanning; consumed by Done for total duration.
	Started time.Time

	// SrcMap is populated by Scanning.
	SrcMap map[string]domain.FileEntry
	// DstMap is populated by Scanning (read from manifest).
	DstMap map[string]domain.FileEntry

	// Diff and totals are populated by Planning.
	Diff          domain.DiffResult
	TransferBytes int64
	DeleteBytes   int64

	// StagingID + StagingPath are set by StageDirInit.
	StagingID   string
	StagingPath string

	// Err and FailedPhase are set by any phase failure path.
	Err         error
	FailedPhase string
}

// Publish is a nil-safe bus helper.
func (rs *RunState) Publish(evt ports.Event) {
	if rs.Bus != nil {
		rs.Bus.Publish(evt)
	}
}

// stringStore returns the adapter's String() label, falling back to the
// composition-time label captured in RunState.
func stringStore(s ports.StorageRepository, fallback string) string {
	if s == nil {
		return fallback
	}
	return fmt.Sprint(s)
}
