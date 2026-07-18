package livesync_test

import (
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/livesync"
	"testing"
	"time"
)

// Story: ParentFromBus initially returns "" — nothing has been pulled.
// First HeadResolvedInfo populates the closure. Second overrides — a
// fresh session's pull replaces stale state without explicit reset.
func TestParentFromBus_TracksLatestHead(t *testing.T) {
	bus := adapters.NewEventBus(8)
	parentFn, stop := livesync.ParentFromBus(bus)
	defer stop()

	if got := parentFn(); got != "" {
		t.Fatalf("initial parentFn=%q, expected empty", got)
	}

	bus.Publish(pulling.HeadResolvedInfo{RefID: domain.RefID("HEAD-1")})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if parentFn() == "HEAD-1" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := parentFn(); got != "HEAD-1" {
		t.Fatalf("after first event, parentFn=%q, expected HEAD-1", got)
	}

	bus.Publish(pulling.HeadResolvedInfo{RefID: domain.RefID("HEAD-2")})
	for time.Now().Before(deadline) {
		if parentFn() == "HEAD-2" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := parentFn(); got != "HEAD-2" {
		t.Fatalf("after second event, parentFn=%q, expected HEAD-2", got)
	}
}

// Story: stop() is idempotent — second call doesn't deadlock.
func TestParentFromBus_StopIsIdempotent(t *testing.T) {
	bus := adapters.NewEventBus(8)
	_, stop := livesync.ParentFromBus(bus)
	stop()
	stop()
}
