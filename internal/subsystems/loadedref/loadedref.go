// Package loadedref keeps settings.LoadedRefID in sync with what the workdir
// actually reflects (design-log/044). The Versions section in Advanced uses
// LoadedRefID to badge "current" on the row that was restored — without it,
// the badge sticks to HEAD even when the workdir holds an older ref (the
// untruthy state we set out to fix).
//
// Two bus events feed the field:
//
//   - pulling.HeadResolvedInfo — workdir was overwritten by an Apply
//     (Download / Restore / Session pull).
//   - committing.CommittedInfo — workdir state was captured into a new local
//     ref (Publish / Session end / livesync amend). Published from both the
//     Committing stage and the livesync ticker so amend chains are tracked
//     without the subsystem caring where the Commit came from.
//
// Writes go through Loader/Saver function values; the composition root supplies
// domain.LoadSettings / Settings.Save by default. Failures are best-effort —
// LoadedRefID is a hint for the badge, never a load-bearing invariant.
package loadedref

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/committing"
	"ritual/internal/core/stages/pulling"
)

// Loader reads the current settings. The composition root wires
// domain.LoadSettings; tests substitute an in-memory store.
type Loader func() (*domain.Settings, error)

// Saver persists settings after the field is updated.
type Saver func(*domain.Settings) error

// Attach subscribes a goroutine that listens for HeadResolvedInfo and
// CommittedInfo and writes settings.LoadedRefID. Returns a stop func that
// cancels the subscription and waits for the consumer to drain. Idempotent
// stop.
//
// Empty RefIDs are ignored — a defensive guard so a buggy publisher cannot
// blank the field. Read/save errors are swallowed (best-effort).
func Attach(ctx context.Context, bus ports.EventBus, load Loader, save Saver) func() {
	ch, cancel := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				id := refIDFor(e)
				if id == "" {
					continue
				}
				apply(load, save, id)
			}
		}
	}()
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
}

func refIDFor(e ports.Event) domain.RefID {
	switch ev := e.(type) {
	case pulling.HeadResolvedInfo:
		return ev.RefID
	case committing.CommittedInfo:
		return ev.RefID
	}
	return ""
}

func apply(load Loader, save Saver, id domain.RefID) {
	s, err := load()
	if err != nil || s == nil {
		return
	}
	if s.LoadedRefID == id {
		return
	}
	s.LoadedRefID = id
	_ = save(s)
}
