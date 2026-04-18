package sync

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"ritual/internal/core/machine"
)

// StagingPrefix is the conventional root under which per-run UUIDv4
// directories are created. Adapters interpret it as a key prefix (R2) or
// a directory name (local FS).
const StagingPrefix = ".staging"

// StageDirInit allocates a UUIDv4 staging directory for the run and
// records it on the RunState. Emits SyncStagingDirCreatedInfo.
//
// The actual filesystem/key creation is deferred to the first Put — for R2
// there is no directory concept, and for local FS the destination Put will
// MkdirAll the staging path when needed.
type StageDirInit struct {
	onOK   machine.Strategy[RunState]
	onFail machine.Strategy[RunState]
}

// NewStageDirInit constructs a StageDirInit strategy.
func NewStageDirInit(onOK, onFail machine.Strategy[RunState]) *StageDirInit {
	return &StageDirInit{onOK: onOK, onFail: onFail}
}

// Run allocates the staging path and announces it.
func (s *StageDirInit) Run(_ context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	rs.StagingID = uuid.NewString()
	rs.StagingPath = fmt.Sprintf("%s/%s", StagingPrefix, rs.StagingID)

	rs.Publish(SyncStagingDirCreatedInfo{
		syncBase:    rs.envelope(),
		StagingPath: rs.StagingPath,
	})
	return s.onOK, nil
}
