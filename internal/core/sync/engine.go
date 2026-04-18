package sync

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Run wires the sync state machine for a single direction and drives it.
// scanner produces SrcMap; dstManifestRead produces DstMap; direction
// selects orphan vs ghost cleanup wiring.
func Run(
	ctx context.Context,
	src, dst ports.StorageRepository,
	scanner ports.DirectoryScanner,
	dstManifestRead func() (map[string]domain.FileEntry, error),
	direction Direction,
	bus ports.EventBus,
) (*RunState, error) {
	rs := &RunState{
		Src:             src,
		Dst:             dst,
		SrcLabel:        labelOf(src),
		DstLabel:        labelOf(dst),
		Direction:       direction,
		Bus:             bus,
		DstManifestRead: dstManifestRead,
	}

	failed := NewFailed()
	done := NewDone()
	cleanup := NewStagingDirCleanup(done, failed)

	var deleteCleanup machine.Strategy[RunState]
	switch direction {
	case DirectionDownload:
		deleteCleanup = NewGhostCleanup(cleanup, failed)
	default:
		deleteCleanup = NewOrphanCleanup(cleanup, failed)
	}

	committing := NewCommitting(deleteCleanup, failed)
	staging := NewStaging(committing, failed)
	stageDirInit := NewStageDirInit(staging, failed)
	planning := NewPlanning(stageDirInit, deleteCleanup) // empty-diff still cleans
	scanning := NewScanning(scanner, planning, failed)

	err := machine.Drive(ctx, rs, scanning)
	return rs, err
}

func labelOf(s ports.StorageRepository) string {
	if s == nil {
		return ""
	}
	type stringer interface{ String() string }
	if x, ok := s.(stringer); ok {
		return x.String()
	}
	return ""
}
