package ritual_test

import (
	"context"
	"errors"
	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	name  string
	count *int
	next  machine.Strategy[ritual.RunState]
}

func (s *countingStrategy) Name() string { return s.name }

func (s *countingStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	*s.count++
	return s.next, nil
}

// failThenRetry terminates with error on first call, follows onRetry on second.
type failThenRetry struct {
	from    string
	onRetry machine.Strategy[ritual.RunState]
	fired   bool
}

func (s *failThenRetry) Name() string { return ritual.StageFailed }

func (s *failThenRetry) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	if s.fired && rs.Err == nil && s.onRetry != nil {
		s.fired = false
		return s.onRetry, nil
	}
	rs.FailedStage = s.from
	s.fired = true
	return nil, rs.Err
}

func TestRunner_RunCurrent_ReentersAtFailedStage(t *testing.T) {
	checkCount := 0
	fetchCount := 0

	fetch := &countingStrategy{name: "Pulling", count: &fetchCount}
	check := &countingStrategy{name: "Checking", count: &checkCount, next: fetch}
	fail := &failThenRetry{from: ritual.StagePulling, onRetry: fetch}
	fetch.next = fail

	rs := &ritual.RunState{Bus: nil, Err: errors.New("network error")}
	runner := ritual.NewRunner(rs)

	// First run: check → fetch → fail (error)
	err := runner.Run(context.Background(), check)
	require.Error(t, err)
	assert.Equal(t, 1, checkCount)
	assert.Equal(t, 1, fetchCount)

	// Clear error, retry — RunCurrent re-enters at fail strategy,
	// which follows onRetry back to fetch
	rs.Err = nil
	fetch.next = nil // fetch now terminates successfully

	err = runner.RunCurrent(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, checkCount, "check must NOT run again")
	assert.Equal(t, 2, fetchCount, "fetch must run again via retry back-edge")
}

func TestRunner_RunCurrent_NilCurrent(t *testing.T) {
	rs := &ritual.RunState{}
	runner := ritual.NewRunner(rs)

	err := runner.RunCurrent(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no current strategy")
}

func TestRun_ConvenienceWrapper(t *testing.T) {
	count := 0
	stage := &countingStrategy{name: "Only", count: &count, next: nil}
	rs := &ritual.RunState{}

	err := ritual.Run(context.Background(), rs, stage)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}
