package sync

import (
	"context"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
)

// Run drives a single sync from src to dst using pre-built file maps.
// Both maps must be relative-keyed identically — the caller is responsible
// for any path-prefix mapping before invoking Run.
//
// direction selects orphan cleanup (Upload — dst is upstream) vs ghost
// cleanup (Download — dst is local).
func Run(
	ctx context.Context,
	src, dst ports.StorageRepository,
	srcMap map[string]domain.FileEntry,
	dstMap map[string]domain.FileEntry,
	direction Direction,
	bus ports.EventBus,
) (*RunState, error) {
	rs := &RunState{
		Src:       src,
		Dst:       dst,
		SrcLabel:  labelOf(src),
		DstLabel:  labelOf(dst),
		Direction: direction,
		Bus:       bus,
		SrcMap:    srcMap,
		DstMap:    dstMap,
		Started:   time.Now(),
	}

	rs.Publish(SyncStartedInfo{syncBase: rs.envelope()})

	failed := NewFailed()
	done := NewDone()
	cleanup := NewStagingDirCleanup(done, failed)

	var deleteCleanup machine.Strategy[RunState]
	switch direction {
	case DirectionDownload:
		deleteCleanup = NewGhostCleanup(cleanup, failed)
	case DirectionUpload:
		deleteCleanup = NewOrphanCleanup(cleanup, failed)
	}

	committing := NewCommitting(deleteCleanup, failed)
	staging := NewStaging(committing, failed)
	stageDirInit := NewStageDirInit(staging, failed)
	planning := NewPlanning(stageDirInit, deleteCleanup) // empty-diff still cleans

	err := machine.Drive(ctx, rs, planning)
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
