// Package committing snapshots the workdir into a new local ref. The
// ref id is written to rs.RefID so the downstream pushing stage can
// upload it. First failure records rs.Err and routes to onFail,
// mirroring pulling.Strategy. Per-blob observability is delegated to
// the storage-decorator stack; this stage only owns batch-level
// lifecycle.
package committing

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// CommittedInfo carries the RefID of a freshly minted local ref. Fires on the
// commit-success path so subsystems that need to know "the workdir was just
// captured into this ref" can react without poking RunState. The loadedref
// subsystem (design-log/044) consumes this + pulling.HeadResolvedInfo to keep
// settings.LoadedRefID in sync with what the workdir actually reflects.
//
// Published from both the Committing stage and the livesync ticker — every
// successful Commit (fresh or amend) emits exactly one CommittedInfo.
type CommittedInfo struct {
	RefID domain.RefID
}

func (c CommittedInfo) String() string { return "committed " + string(c.RefID) }

// OptsResolver produces the CommitOpts for this run. Composition-root
// convention, honoured by the canonical resolver:
//
//   - rs.RefID non-empty ⇒ Amend=rs.RefID. A live-ticker draft already
//     exists for this session; post-session commit collapses into it
//     rather than forking a sibling ref (see spec §1435 amend-push
//     collapse).
//   - rs.RefID empty ⇒ fresh commit. Parent is the pulled HEAD id
//     (injected into the resolver at composition). No ticker ever ran,
//     so there is no draft to amend.
//
// The stage itself does not enforce this; it just passes rs to the
// resolver. Tests assert the stage forwards rs correctly so the
// composition root can implement either branch.
type OptsResolver func(rs *ritual.RunState) ports.CommitOpts

// Strategy implements the Committing stage.
type Strategy struct {
	committer ports.Committer
	resolve   OptsResolver
	onOK      machine.Strategy[ritual.RunState]
	onFail    machine.Strategy[ritual.RunState]
}

// New builds a Committing Strategy.
func New(committer ports.Committer, resolve OptsResolver, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{committer: committer, resolve: resolve, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageCommitting }

// Run snapshots the workdir under the resolved CommitOpts. On success
// the freshly minted ref id lands on rs.RefID for pushing to consume.
// On entry-time ctx cancellation the stage short-circuits to onFail
// without invoking the Committer — wasted IO is forbidden, same rule
// as pulling.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "commit"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	opts := s.resolve(rs)
	id, err := s.committer.Commit(ctx, opts)
	if err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	rs.RefID = id
	publish(rs.Bus, CommittedInfo{RefID: id})
	publish(rs.Bus, ritual.FinishInfo{Operation: "commit"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
