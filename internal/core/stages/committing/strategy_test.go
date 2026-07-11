package committing_test

import (
	"context"
	"errors"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/committing"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type recordingCommitter struct {
	calls []ports.CommitOpts
	id    domain.RefID
	err   error
}

func (c *recordingCommitter) Commit(_ context.Context, opts ports.CommitOpts) (domain.RefID, error) {
	c.calls = append(c.calls, opts)
	return c.id, c.err
}

func staticResolver(opts ports.CommitOpts) committing.OptsResolver {
	return func(_ *ritual.RunState) ports.CommitOpts { return opts }
}

func newRunState() *ritual.RunState {
	return &ritual.RunState{Bus: adapters.NewEventBus(16)}
}

func TestCommitting_WritesRefIDAndRoutesToOnOKOnSuccess(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	opts := ports.CommitOpts{Parent: "2026-04-23T09-00-00.000Z", Targets: []string{"world/**"}}
	committer := &recordingCommitter{id: "2026-04-23T10-00-00.000Z"}

	stage := committing.New(committer, staticResolver(opts), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err, "Committing stage must never return a Go error — failures travel via onFail")
	assert.Equal(t, []ports.CommitOpts{opts}, committer.calls,
		"Committing stage must call Committer.Commit exactly once with the resolved opts so the new ref snapshots the configured targets under the right parent")
	assert.Equal(t, domain.RefID("2026-04-23T10-00-00.000Z"), rs.RefID,
		"Committing stage must write the freshly minted ref id onto rs.RefID so the downstream pushing stage knows which ref to upload")
	assert.Same(t, machine.Strategy[ritual.RunState](onOK), next,
		"Committing stage must route to onOK after Commit succeeds so the chain advances toward pushing")
}

func TestCommitting_RecordsCommitterErrorLeavesRefIDEmptyAndRoutesToOnFail(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	boom := errors.New("commit: snapshot world/level.dat: read workdir: disk full")
	committer := &recordingCommitter{err: boom}

	stage := committing.New(committer, staticResolver(ports.CommitOpts{Targets: []string{"world/**"}}), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	assert.Same(t, boom, rs.Err,
		"Committing stage must record Committer.Commit's error verbatim on rs.Err so the operator sees which file failed to snapshot")
	assert.Equal(t, domain.RefID(""), rs.RefID,
		"Committing stage must leave rs.RefID empty on failure so downstream pushing cannot upload a half-formed ref")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Committing stage must route to onFail when Commit errors so the failure propagates up the chain")
}

func TestCommitting_RoutesToOnFailWhenContextAlreadyCancelledBeforeCommit(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	committer := &recordingCommitter{id: "unused"}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	stage := committing.New(committer, staticResolver(ports.CommitOpts{Targets: []string{"world/**"}}), onOK, onFail)

	rs := newRunState()
	next, err := stage.Run(ctx, rs)

	require.NoError(t, err)
	assert.ErrorIs(t, rs.Err, context.Canceled,
		"Committing stage must record context.Canceled when entered with a cancelled ctx so cancellation surfaces as the failure cause")
	assert.Empty(t, committer.calls,
		"Committing stage must not invoke Committer.Commit when ctx is cancelled on entry — protects the workdir from partial snapshot IO")
	assert.Equal(t, domain.RefID(""), rs.RefID,
		"Committing stage must not touch rs.RefID on entry-time cancellation so no stale id leaks to later stages")
	assert.Same(t, machine.Strategy[ritual.RunState](onFail), next,
		"Committing stage must route to onFail on entry-time cancellation so the chain does not advance")
}

func TestCommitting_ResolverAmendsLiveTickerDraftWhenRefIDPresent(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	committer := &recordingCommitter{id: "final"}
	resolver := func(rs *ritual.RunState) ports.CommitOpts {
		if rs.RefID != "" {
			return ports.CommitOpts{Amend: rs.RefID, Targets: []string{"world/**"}}
		}
		return ports.CommitOpts{Parent: "pulled-head", Targets: []string{"world/**"}}
	}

	stage := committing.New(committer, resolver, onOK, onFail)

	rs := newRunState()
	rs.RefID = "draft-from-ticker"
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	require.Len(t, committer.calls, 1)
	assert.Equal(t, domain.RefID("draft-from-ticker"), committer.calls[0].Amend,
		"Committing stage must honour the Amend chosen by the resolver so a live-ticker draft collapses into the final ref rather than forking a sibling")
	assert.Equal(t, domain.RefID(""), committer.calls[0].Parent,
		"Committing stage must not carry Parent when Amend is set — the resolver's amend branch means rs.RefID already encodes the prior state, so passing Parent too would double-reference history")
}

func TestCommitting_ResolverCreatesFreshCommitWhenRefIDEmpty(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	committer := &recordingCommitter{id: "fresh"}
	resolver := func(rs *ritual.RunState) ports.CommitOpts {
		if rs.RefID != "" {
			return ports.CommitOpts{Amend: rs.RefID, Targets: []string{"world/**"}}
		}
		return ports.CommitOpts{Parent: "pulled-head", Targets: []string{"world/**"}}
	}

	stage := committing.New(committer, resolver, onOK, onFail)

	rs := newRunState()
	_, err := stage.Run(t.Context(), rs)

	require.NoError(t, err)
	require.Len(t, committer.calls, 1)
	assert.Equal(t, domain.RefID(""), committer.calls[0].Amend,
		"Committing stage must leave Amend empty when no ticker draft exists so the Committer forks a new ref rather than rewriting a non-existent draft")
	assert.Equal(t, domain.RefID("pulled-head"), committer.calls[0].Parent,
		"Committing stage must carry the pulled-head Parent the resolver chose so the fresh ref's lineage links back to the remote HEAD this session synced from")
	assert.Equal(t, domain.RefID("fresh"), rs.RefID,
		"Committing stage must overwrite the empty rs.RefID with the fresh ref id so pushing knows which ref to upload")
}

func TestCommitting_PublishesBatchLifecycleEventsOnTheBus(t *testing.T) {
	onOK := &sentinelStrategy{name: "ok"}
	onFail := &sentinelStrategy{name: "fail"}
	bus := adapters.NewEventBus(16)
	ch, unsub := bus.Subscribe()
	defer unsub()

	committer := &recordingCommitter{id: "id"}
	stage := committing.New(committer, staticResolver(ports.CommitOpts{Targets: []string{"world/**"}}), onOK, onFail)
	rs := &ritual.RunState{Bus: bus}

	_, err := stage.Run(t.Context(), rs)
	require.NoError(t, err)

	deadline := time.After(time.Second)
	var sawStart, sawFinish bool
	for !sawStart || !sawFinish {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for batch lifecycle events: start=%v finish=%v", sawStart, sawFinish)
		case e := <-ch:
			if s, ok := e.(ritual.StartInfo); ok && s.Operation == "commit" {
				sawStart = true
			}
			if f, ok := e.(ritual.FinishInfo); ok && f.Operation == "commit" {
				sawFinish = true
			}
		}
	}
}

var _ ports.Committer = (*recordingCommitter)(nil)
