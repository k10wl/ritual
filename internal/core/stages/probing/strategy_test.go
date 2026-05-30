package probing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/probing"
	"ritual/internal/core/stages/pulling"
)

type sentinelStrategy struct {
	name   string
	called bool
}

func (s *sentinelStrategy) Name() string { return s.name }
func (s *sentinelStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	s.called = true
	return nil, nil
}

func newRunState() *ritual.RunState {
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func resolverReturning(id domain.RefID, err error) pulling.HeadResolver {
	return func(_ context.Context) (domain.RefID, error) { return id, err }
}

func TestProbing_HeadPresent_RecordsParentRefIDAndRoutesToOnOK(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	rs := newRunState()

	stage := probing.New(resolverReturning("2026-05-29T10-00-00.000Z", nil), onOK, onFail)
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Probing must never return a Go error — failures travel via onFail")
	assert.Equal(t, domain.RefID("2026-05-29T10-00-00.000Z"), rs.ParentRefID,
		"Probing must record the resolved HEAD as ParentRefID so the fresh commit parents on it")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Probing must advance to onOK when a HEAD exists")
}

func TestProbing_NoHead_SeedingLeavesEmptyParentAndRoutesToOnOK(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	rs := newRunState()
	rs.ParentRefID = "stale" // prove the stage clears it on the seeding path

	stage := probing.New(resolverReturning("", pulling.ErrNoHead), onOK, onFail)
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Equal(t, domain.RefID(""), rs.ParentRefID,
		"ErrNoHead is the seeding case: ParentRefID must be empty so the commit writes ref #1 with no parent")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Seeding is a success — Probing must advance, not fail, on an empty remote")
}

func TestProbing_ResolverError_RecordsErrAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	rs := newRunState()
	boom := errors.New("r2: list refs/: connection refused")

	stage := probing.New(resolverReturning("", boom), onOK, onFail)
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "a real resolver error travels via onFail, not as a Go error from Run")
	assert.Equal(t, boom, rs.Err, "Probing must record the resolver error on RunState for the failed stage to report")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"a transport failure must route to onFail, not advance the chain")
}

func TestProbing_CancelledContext_RoutesToOnFailWithoutResolving(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	rs := newRunState()
	resolved := false
	resolver := func(_ context.Context) (domain.RefID, error) { resolved = true; return "", nil }

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	next, err := probing.New(resolver, onOK, onFail).Run(ctx, rs)

	require.NoError(t, err)
	assert.False(t, resolved, "a cancelled context must short-circuit before calling the resolver")
	assert.Equal(t, context.Canceled, rs.Err)
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next)
}
