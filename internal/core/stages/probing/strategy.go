// Package probing resolves the remote HEAD ref id without downloading any
// blobs or touching the workdir. It is the Upload flow's stand-in for
// pulling (design-log/031): the user's local files are authoritative, so
// the only thing Upload needs from the remote is the current HEAD to parent
// the new ref on (lineage). Empty remote (pulling.ErrNoHead) is the seeding
// case — Parent stays "" and the chain advances to write ref #1.
//
// Reuses pulling.HeadResolver verbatim so there is no new port and the
// composition root mocks one callable for both flows.
package probing

import (
	"context"
	"errors"

	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
)

// Strategy implements the Probing stage.
type Strategy struct {
	resolve pulling.HeadResolver
	onOK    machine.Strategy[ritual.RunState]
	onFail  machine.Strategy[ritual.RunState]
}

// New builds a Probing Strategy over the shared HeadResolver.
func New(resolve pulling.HeadResolver, onOK, onFail machine.Strategy[ritual.RunState]) *Strategy {
	return &Strategy{resolve: resolve, onOK: onOK, onFail: onFail}
}

// Name returns the stage name.
func (*Strategy) Name() string { return ritual.StageProbing }

// Run resolves the remote HEAD and records it as rs.ParentRefID. ErrNoHead
// means the remote carries no refs yet (seeding) — Parent stays empty and
// the chain advances so Commit+Push bootstraps the first ref. Any other
// resolver error records rs.Err and routes to onFail. Unlike Pulling there
// is no blob download and no Apply: local files are authoritative.
func (s *Strategy) Run(ctx context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	publish(rs.Bus, ritual.StartInfo{Operation: "probe"})
	if err := ctx.Err(); err != nil {
		rs.Err = err
		return s.onFail, nil //nolint:nilerr // error stored on RunState; onFail stage handles it
	}
	id, err := s.resolve(ctx)
	switch {
	case errors.Is(err, pulling.ErrNoHead):
		rs.ParentRefID = "" // seeding: empty remote, local files become ref #1
	case err != nil:
		rs.Err = err
		return s.onFail, nil
	default:
		rs.ParentRefID = id
	}
	publish(rs.Bus, ritual.FinishInfo{Operation: "probe"})
	return s.onOK, nil
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}
