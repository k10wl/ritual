package statemachine_test

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

// step is a canned Handler used across state tests as the return value for
// stubFactory methods. name drives Name(), next wires the transition.
type stepStub struct {
	n    sm.StateName
	next sm.Handler
	err  error
}

func (s *stepStub) Name() sm.StateName                           { return s.n }
func (s *stepStub) Handle(_ context.Context) (sm.Handler, error) { return s.next, s.err }

// stubFactory records which builder was invoked and returns canned steps.
type stubFactory struct {
	last        sm.StateName
	failedFrom  sm.StateName
	failedErr   error
	lockID      string
	unlockCause error
}

func (s *stubFactory) Preparing() sm.Handler { s.last = sm.Preparing; return &stepStub{n: sm.Preparing} }
func (s *stubFactory) Locking() sm.Handler   { s.last = sm.Locking; return &stepStub{n: sm.Locking} }
func (s *stubFactory) Running() sm.Handler   { s.last = sm.Running; return &stepStub{n: sm.Running} }
func (s *stubFactory) Exiting(lockID string, _, _ *domain.Manifest) sm.Handler {
	s.last = sm.Exiting
	s.lockID = lockID
	return &stepStub{n: sm.Exiting}
}
func (s *stubFactory) Unlocking(lockID string, cause error) sm.Handler {
	s.last = sm.Unlocking
	s.lockID = lockID
	s.unlockCause = cause
	return &stepStub{n: sm.Unlocking}
}
func (s *stubFactory) Failed(from sm.StateName, err error) sm.Handler {
	s.last = sm.Failed
	s.failedFrom = from
	s.failedErr = err
	return &stepStub{n: sm.Failed}
}
func (s *stubFactory) RunID() string { return "test-run" }

// fakeCond is a ConditionService stub — returns preconfigured error.
type fakeCond struct{ err error }

func (f fakeCond) Check(_ context.Context) error { return f.err }

// fakeUpd is an UpdaterService stub — returns preconfigured error.
type fakeUpd struct{ err error }

func (f fakeUpd) Run(_ context.Context) error { return f.err }

// fakeServerRunner is a ServerRunner stub — returns preconfigured error.
type fakeServerRunner struct{ err error }

func (f fakeServerRunner) Run(_ context.Context, _ *domain.ServerRuntime) error { return f.err }

// fakeRetention is a RetentionService stub — returns preconfigured error.
type fakeRetention struct{ err error }

func (f fakeRetention) Apply(_ context.Context) error { return f.err }

// fakeManifestStore is a minimal ManifestStore stub.
type fakeManifestStore struct {
	m       *domain.Manifest
	getErr  error
	saveErr error

	saved *domain.Manifest
}

func (f *fakeManifestStore) Get(_ context.Context) (*domain.Manifest, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.m == nil {
		return nil, nil
	}
	// Return a shallow clone so mutations don't leak.
	clone := *f.m
	return &clone, nil
}

func (f *fakeManifestStore) Save(_ context.Context, m *domain.Manifest) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = m
	return nil
}

var _ ports.ManifestStore = (*fakeManifestStore)(nil)
