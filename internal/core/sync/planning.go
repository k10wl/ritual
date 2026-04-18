package sync

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
)

// Planning computes the diff, populates per-action byte totals on the
// run state, emits SyncPlanInfo + per-file SyncPlanFileInfo, and routes
// to onContinue (when there is work) or onEmpty (when diff is empty).
type Planning struct {
	onContinue machine.Strategy[RunState]
	onEmpty    machine.Strategy[RunState]
}

// NewPlanning constructs a Planning strategy.
func NewPlanning(onContinue, onEmpty machine.Strategy[RunState]) *Planning {
	return &Planning{onContinue: onContinue, onEmpty: onEmpty}
}

// Run computes diff totals and emits the plan events. Never fails — diff
// is a pure function over already-populated maps.
func (p *Planning) Run(_ context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	rs.Diff = domain.ComputeDiff(rs.SrcMap, rs.DstMap)

	addCount, updateCount := splitAddVsUpdate(rs.Diff.Upload, rs.DstMap)
	addBytes, updateBytes := splitAddVsUpdateBytes(rs.Diff.Upload, rs.SrcMap, rs.DstMap)
	deleteBytes := sumSizes(rs.Diff.Delete, rs.DstMap, rs.SrcMap)

	rs.TransferBytes = addBytes + updateBytes
	rs.DeleteBytes = deleteBytes

	env := rs.envelope()
	rs.Publish(SyncPlanInfo{
		syncBase:    env,
		Adds:        addCount,
		Updates:     updateCount,
		Deletes:     len(rs.Diff.Delete),
		AddBytes:    addBytes,
		UpdateBytes: updateBytes,
		DeleteBytes: deleteBytes,
	})

	for _, path := range rs.Diff.Upload {
		action := ActionAdd
		if _, exists := rs.DstMap[path]; exists {
			action = ActionUpdate
		}
		rs.Publish(SyncPlanFileInfo{
			syncBase: env,
			Path:     path,
			Action:   action,
			Size:     rs.SrcMap[path].Size,
		})
	}
	for _, path := range rs.Diff.Delete {
		rs.Publish(SyncPlanFileInfo{
			syncBase: env,
			Path:     path,
			Action:   ActionDelete,
			Size:     rs.DstMap[path].Size,
		})
	}

	// No file transfers → skip StageDirInit. Pure-delete runs go straight
	// to the cleanup branch so we never allocate a staging dir we'd then
	// have to clean up empty-handed.
	if len(rs.Diff.Upload) == 0 {
		return p.onEmpty, nil
	}
	return p.onContinue, nil
}

// sumSizes totals sizes of paths from primary map, falling back to fallback.
func sumSizes(paths []string, primary, fallback map[string]domain.FileEntry) int64 {
	var total int64
	for _, p := range paths {
		if entry, ok := primary[p]; ok {
			total += entry.Size
			continue
		}
		total += fallback[p].Size
	}
	return total
}

func splitAddVsUpdate(paths []string, dst map[string]domain.FileEntry) (adds, updates int) {
	for _, p := range paths {
		if _, exists := dst[p]; exists {
			updates++
			continue
		}
		adds++
	}
	return adds, updates
}

func splitAddVsUpdateBytes(paths []string, src, dst map[string]domain.FileEntry) (addBytes, updateBytes int64) {
	for _, p := range paths {
		size := src[p].Size
		if _, exists := dst[p]; exists {
			updateBytes += size
			continue
		}
		addBytes += size
	}
	return addBytes, updateBytes
}
