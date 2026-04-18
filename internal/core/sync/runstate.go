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

// Direction is the cleanup mode of a sync run.
type Direction int

// Direction values select the cleanup wiring of a sync run.
const (
	// DirectionUpload — dst is the upstream side; orphan cleanup applies.
	DirectionUpload Direction = iota
	// DirectionDownload — dst is the local side; ghost cleanup applies.
	DirectionDownload
)

// RunState is shared across strategies for a single sync run. The engine
// constructs it with src/dst stores and pre-built file maps; strategies
// fill Diff, StagingID, StagingPath, and the failure fields as they run.
type RunState struct {
	// Wired at construction.
	Src       ports.StorageRepository
	Dst       ports.StorageRepository
	SrcLabel  string
	DstLabel  string
	Direction Direction
	Bus       ports.EventBus

	// Pre-built by the caller before drive.
	SrcMap map[string]domain.FileEntry
	DstMap map[string]domain.FileEntry

	// Started is set by the engine before the chain runs.
	Started time.Time

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
