// Package relocating moves the CONTENT set (objects/refs/server/worlds)
// from the current work root to a user-chosen destination, then atomically
// swaps every downstream consumer's storage facade to point at it
// (design-log/055). Unlike its sibling packages under internal/core/stages,
// it drives the generic machine.Strategy[State]/machine.Drive directly
// instead of ritual.RunState/ritual.Runner — a relocate has no
// resumability (no persisted state, no auto-retry, stale destination files
// are explicitly not cleaned up on failure), so Run returns a plain error
// on failure rather than storing it on shared run state for a later
// re-entry.
package relocating

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// State is the per-run value shared across relocating's single stage.
type State struct {
	Dst      string
	Refs     WorkRootRefs
	Settings *domain.Settings
	Bus      ports.EventBus
	Err      error
}

// Strategy implements the Relocating operation as one stage with a
// sequential body — mirrors checking.Strategy's "one stage, internal loop"
// shape rather than a multi-state sub-machine, since this is a single
// fixed-order sequence with no branching beyond validate/copy/verify/commit
// each either succeeding or failing outright.
type Strategy struct {
	onOK   machine.Strategy[State]
	onFail machine.Strategy[State]
}

// New builds a Relocating Strategy. onOK/onFail are accepted for
// constructor symmetry with the other stage packages and to keep the door
// open for future composition, even though the current only call site
// (ControlService.ChangeWorkRoot) passes nil, nil and relies on the
// returned error instead.
func New(onOK, onFail machine.Strategy[State]) *Strategy {
	return &Strategy{onOK: onOK, onFail: onFail}
}

// Name returns the stage name. Not a ritual.StageX constant — relocating
// never runs through ritual.RunState/the dial/projection; it is a
// standalone settings-API operation, not a session flow.
func (*Strategy) Name() string { return "relocate" }

// Run validates the destination, copies the CONTENT set into a fresh
// os.Root, verifies it, atomically swaps every storage facade to the new
// root, durably commits settings.WorkRoot, then best-effort cleans up the
// old root. See design-log/055 Design § and Crash safety § for the
// ACID reasoning behind this exact step order.
func (s *Strategy) Run(ctx context.Context, st *State) (machine.Strategy[State], error) {
	publish(st.Bus, RelocateStarted{})
	if err := ctx.Err(); err != nil {
		return s.fail(st, err)
	}

	if err := validate(st.Dst, st.Refs); err != nil {
		return s.fail(st, err)
	}

	newRoot, newLocal, newWorkdir, err := buildNewRoot(st.Dst)
	if err != nil {
		return s.fail(st, err)
	}

	stopCtx, stopCancel := watchStop(ctx, st.Bus)
	defer stopCancel()

	total, files, err := planCopy(st.Refs)
	if err != nil {
		_ = newRoot.Close()
		return s.fail(st, err)
	}
	publish(st.Bus, RelocatePlanned{BytesTotal: total, FilesTotal: files})

	if err := copyContent(stopCtx, st.Refs, newLocal, newWorkdir, st.Bus); err != nil {
		_ = newRoot.Close()
		return s.fail(st, err)
	}

	publish(st.Bus, RelocateVerifying{})
	if err := verify(stopCtx, newLocal, newWorkdir); err != nil {
		_ = newRoot.Close()
		return s.fail(st, err)
	}

	old := st.Refs.snapshot()
	st.Refs.store(newRoot, newLocal, newWorkdir)
	publish(st.Bus, RelocateCommitting{})
	if err := commit(st.Settings, st.Dst); err != nil {
		st.Refs.store(old.root, old.local, old.workdir)
		_ = newRoot.Close()
		return s.fail(st, err)
	}

	cleanup(old.root, old.root.Name())
	publish(st.Bus, RelocateFinished{})
	return s.onOK, nil
}

// fail publishes RelocateFailed and, deliberately, does NOT store st.Err the
// way checking/pulling store rs.Err before returning (onFail, nil) — the
// rest of internal/core/stages does that so ritual.Runner.RunCurrent/Resume
// can re-enter a stopped chain at the failed stage. relocating has nothing
// to re-enter, so a caller with no onFail wired gets the real error back
// directly instead of a silent false "success" from machine.Drive.
func (s *Strategy) fail(st *State, err error) (machine.Strategy[State], error) {
	st.Err = err
	publish(st.Bus, RelocateFailed{Err: err})
	if s.onFail != nil {
		return s.onFail, nil
	}
	return nil, err
}

func publish(bus ports.EventBus, e ports.Event) {
	if bus != nil {
		bus.Publish(e)
	}
}

// watchStop is relocating's own copy of pulling.Strategy's unexported
// helper (internal/core/stages/pulling/strategy.go) — that function is
// package-private so it cannot be imported directly. Identical body:
// cancels the returned context the moment a ritual.StopRequested event
// arrives on the bus, so an in-flight copy can be interrupted between
// files without the caller blocking for the whole transfer.
func watchStop(parent context.Context, bus ports.EventBus) (context.Context, context.CancelFunc) {
	stopCtx, cancel := context.WithCancel(parent)
	if bus == nil {
		return stopCtx, cancel
	}
	sub, unsub := bus.Subscribe()
	go func() {
		defer unsub()
		for {
			select {
			case <-stopCtx.Done():
				return
			case e, ok := <-sub:
				if !ok {
					return
				}
				if _, ok := e.(ritual.StopRequested); ok {
					cancel()
					return
				}
			}
		}
	}()
	return stopCtx, cancel
}
