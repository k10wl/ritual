package lifecycle_test

import (
	"context"
	"ritual/internal/adapters"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"ritual/internal/subsystems/lifecycle"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordStrategy terminates the chain immediately, recording that it ran.
type recordStrategy struct {
	name    string
	entered chan struct{}
}

func newRecordStrategy(name string) *recordStrategy {
	return &recordStrategy{name: name, entered: make(chan struct{}, 1)}
}
func (s *recordStrategy) Name() string { return s.name }
func (s *recordStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	s.entered <- struct{}{}
	return nil, nil
}

func (s *recordStrategy) ran() bool {
	select {
	case <-s.entered:
		return true
	default:
		return false
	}
}

// blockingStrategy holds the run goroutine (status stays Running) until
// release is closed, so a second gesture can be tested against a busy
// controller.
type blockingStrategy struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingStrategy() *blockingStrategy {
	return &blockingStrategy{entered: make(chan struct{}), release: make(chan struct{})}
}
func (*blockingStrategy) Name() string { return "Blocking" }
func (s *blockingStrategy) Run(_ context.Context, _ *ritual.RunState) (machine.Strategy[ritual.RunState], error) {
	close(s.entered)
	<-s.release
	return nil, nil
}

func waitForStatus(t *testing.T, ch <-chan ports.Event, want lifecycle.Outcome) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status %s", want)
		case e := <-ch:
			if sc, ok := e.(lifecycle.StatusChanged); ok && sc.Status == want && sc.Err == nil {
				return
			}
		}
	}
}

func waitForRejection(t *testing.T, ch <-chan ports.Event) lifecycle.StatusChanged {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for a rejection StatusChanged (Err set)")
		case e := <-ch:
			if sc, ok := e.(lifecycle.StatusChanged); ok && sc.Err != nil {
				return sc
			}
		}
	}
}

func TestRouting_DownloadRequested_DrivesDownloadEntryOnly(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	session := newRecordStrategy("Session")
	download := newRecordStrategy("Download")
	upload := newRecordStrategy("Upload")
	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: session, Download: download, Upload: upload,
	})
	defer stop()

	bus.Publish(ritual.DownloadRequested{})
	waitForStatus(t, ch, lifecycle.Done)

	assert.True(t, download.ran(), "DownloadRequested must drive the Download entry strategy")
	assert.False(t, session.ran(), "DownloadRequested must not drive the Session entry")
	assert.False(t, upload.ran(), "DownloadRequested must not drive the Upload entry")
}

func TestRouting_UploadRequested_DrivesUploadEntryOnly(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	session := newRecordStrategy("Session")
	download := newRecordStrategy("Download")
	upload := newRecordStrategy("Upload")
	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: session, Download: download, Upload: upload,
	})
	defer stop()

	bus.Publish(ritual.UploadRequested{})
	waitForStatus(t, ch, lifecycle.Done)

	assert.True(t, upload.ran(), "UploadRequested must drive the Upload entry strategy")
	assert.False(t, session.ran(), "UploadRequested must not drive the Session entry")
	assert.False(t, download.ran(), "UploadRequested must not drive the Download entry")
}

func TestRouting_GestureRejectedWhileAnotherFlowRunning(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	session := newBlockingStrategy()
	download := newRecordStrategy("Download")
	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: session, Download: download,
	})
	defer stop()

	bus.Publish(ritual.StartRequested{})
	<-session.entered

	bus.Publish(ritual.DownloadRequested{})
	rejection := waitForRejection(t, ch)

	assert.Equal(t, lifecycle.Running, rejection.Status,
		"a gesture during a Running flow must be rejected with the current Running status")
	assert.False(t, download.ran(),
		"the shared status field must give free mutual exclusion — Download must not start while the session runs")

	close(session.release)
	waitForStatus(t, ch, lifecycle.Done)
}

func TestRouting_UnwiredGesture_RejectedNotPanicked(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: newRecordStrategy("Session"),
	})
	defer stop()

	bus.Publish(ritual.UploadRequested{})
	rejection := waitForRejection(t, ch)

	require.Error(t, rejection.Err, "an unwired Upload gesture must publish a rejection, not nil-panic the controller goroutine")
}

func TestRouting_StartRequestedSkipSync_DrivesLocalSessionEntryOnly(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	session := newRecordStrategy("Session")
	local := newRecordStrategy("LocalSession")
	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: session, LocalSession: local,
	})
	defer stop()

	bus.Publish(ritual.StartRequested{SkipSync: true})
	waitForStatus(t, ch, lifecycle.Done)

	assert.True(t, local.ran(), "StartRequested{SkipSync:true} must drive the LocalSession entry (design-log/036)")
	assert.False(t, session.ran(), "the skip-sync gesture must not drive the full Session entry")
}

func TestRouting_StartRequested_DrivesSessionEntryOnly(t *testing.T) {
	bus := adapters.NewEventBus(64)
	ch, unsub := bus.Subscribe()
	defer unsub()

	session := newRecordStrategy("Session")
	local := newRecordStrategy("LocalSession")
	stop := lifecycle.Attach(context.Background(), bus, lifecycle.Entries{
		Session: session, LocalSession: local,
	})
	defer stop()

	bus.Publish(ritual.StartRequested{})
	waitForStatus(t, ch, lifecycle.Done)

	assert.True(t, session.ran(), "StartRequested{} (SkipSync false) must still drive the full Session entry")
	assert.False(t, local.ran(), "the normal start gesture must not drive the LocalSession entry")
}
