package ritual_test

import (
	"context"
	"fmt"
	"testing"

	"ritual/internal/core/machine"
	"ritual/internal/core/ritual"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingStrategy struct {
	name  string
	count *int
	next  machine.Strategy[ritual.RunState]
}

func (s *countingStrategy) Name() string { return s.name }

func (s *countingStrategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	*s.count++
	return s.next, nil
}

type failStrategy struct {
	from string
}

func (s *failStrategy) Name() string { return ritual.StageFailed }

func (s *failStrategy) Run(_ context.Context, rs *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	rs.FailedStage = s.from
	return nil, rs.Err
}

func TestRunner_RunCurrent_ReentersAtFailedStage(t *testing.T) {
	checkCount := 0
	fetchCount := 0

	fail := &failStrategy{from: ritual.StageFetching}
	fetch := &countingStrategy{name: "Fetching", count: &fetchCount, next: fail}
	check := &countingStrategy{name: "Checking", count: &checkCount, next: fetch}

	rs := &ritual.RunState{Bus: nil, Err: fmt.Errorf("network error")}
	runner := ritual.NewRunner(rs)

	// First run: check → fetch → fail
	err := runner.Run(context.Background(), check)
	require.Error(t, err)
	assert.Equal(t, 1, checkCount)
	assert.Equal(t, 1, fetchCount)

	// Fix the error, retry from failed stage
	rs.Err = nil
	done := &countingStrategy{name: "Done", count: new(int), next: nil}
	fetch.next = done

	err = runner.RunCurrent(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 1, checkCount, "check must NOT run again")
	assert.Equal(t, 2, fetchCount, "fetch must run again")
}
