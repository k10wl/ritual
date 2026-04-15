package statemachine_test

import (
	"errors"
	"testing"

	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	sm "ritual/internal/core/statemachine"
)

// TestFactory_WiresAllStates is a smoke test: every builder method must
// return a non-nil Handler reporting the correct state name. If any state's
// ctor changes in a way that breaks Deps wiring, this fails at compile time
// OR at runtime via a nil return.
func TestFactory_WiresAllStates(t *testing.T) {
	deps := sm.Deps{
		Bus:             adapters.NewEventBus(8),
		RunID:           "test-run",
		Server:          &domain.ServerRuntime{},
		LocalManifests:  &fakeManifestStore{},
		RemoteManifests: &fakeManifestStore{},
		ServerRunner:    fakeServerRunner{},
		Conditions:      []ports.ConditionService{fakeCond{}},
		Updaters:        []ports.UpdaterService{fakeUpd{}},
		ExitUpdaters:    []ports.UpdaterService{fakeUpd{}},
		Retentions:      []ports.RetentionService{fakeRetention{}},
	}
	f := sm.NewFactory(deps)

	cases := []struct {
		name string
		h    sm.Handler
		want sm.StateName
	}{
		{"Preparing", f.Preparing(), sm.Preparing},
		{"Locking", f.Locking(), sm.Locking},
		{"Running", f.Running(), sm.Running},
		{"Exiting", f.Exiting("lock-id", &domain.Manifest{}, &domain.Manifest{}), sm.Exiting},
		{"Unlocking", f.Unlocking("lock-id", errors.New("cause")), sm.Unlocking},
		{"Failed", f.Failed(sm.Preparing, errors.New("boom")), sm.Failed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.h == nil {
				t.Fatalf("%s() returned nil", tc.name)
			}
			if tc.h.Name() != tc.want {
				t.Fatalf("Name() = %v, want %v", tc.h.Name(), tc.want)
			}
		})
	}

	if f.RunID() != "test-run" {
		t.Fatalf("RunID() = %q", f.RunID())
	}
}
