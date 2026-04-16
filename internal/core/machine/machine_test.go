package machine_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/machine"
)

type counter struct{ n int }

type step struct {
	tag  string
	next machine.Strategy[counter]
	err  error
}

func (s *step) Run(_ context.Context, c *counter) (machine.Strategy[counter], error) {
	c.n++
	if s.err != nil {
		return nil, s.err
	}
	return s.next, nil
}

func TestDriveVisitsChainInOrder(t *testing.T) {
	third := &step{tag: "c"}
	second := &step{tag: "b", next: third}
	first := &step{tag: "a", next: second}

	c := &counter{}
	if err := machine.Drive(context.Background(), c, first); err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if c.n != 3 {
		t.Fatalf("visits: got %d want 3", c.n)
	}
}

func TestDriveStopsOnNilSuccessor(t *testing.T) {
	only := &step{tag: "solo"}
	c := &counter{}
	if err := machine.Drive(context.Background(), c, only); err != nil {
		t.Fatal(err)
	}
	if c.n != 1 {
		t.Fatalf("visits: %d", c.n)
	}
}

func TestDrivePropagatesError(t *testing.T) {
	boom := errors.New("boom")
	fail := &step{tag: "fail", err: boom}
	unreached := &step{tag: "never"}
	first := &step{tag: "a", next: fail}
	fail.next = unreached

	c := &counter{}
	err := machine.Drive(context.Background(), c, first)
	if !errors.Is(err, boom) {
		t.Fatalf("want %v got %v", boom, err)
	}
	if c.n != 2 {
		t.Fatalf("visits past failure: %d", c.n)
	}
}

func TestDriveAcceptsNilStart(t *testing.T) {
	c := &counter{}
	var start machine.Strategy[counter]
	if err := machine.Drive(context.Background(), c, start); err != nil {
		t.Fatal(err)
	}
	if c.n != 0 {
		t.Fatalf("no steps expected, got %d", c.n)
	}
}
