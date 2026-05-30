// Package control is the driving adapter for the GUI: Wails-bound method
// surfaces that translate frontend (JS) calls into bus commands or
// snapshot lookups. No business logic.
package control

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"ritual/internal/core/ritual"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/core/stages/running"
	"ritual/internal/gui/projection"
	"runtime"
	"time"
)

// syncProbeTimeout bounds the launch staleness check so an offline or slow
// remote can't hang the IDLE screen. On timeout/error GetSyncStatus
// degrades to a zero status (Behind:false) — design-log/031 OQ3.
const syncProbeTimeout = 5 * time.Second

// SnapshotSource is the read-side of the GUI projection. Bound at wiring
// time to the *projection.Projection produced in cmd/gui. Narrow interface
// so tests stub it cheaply.
type SnapshotSource interface {
	Snapshot() projection.ViewModel
}

// WindowControl is the tiny slice of Wails' webview window API the logs
// button uses. Kept minimal so cmd/gui can wrap a Wails window without
// this package importing Wails.
type WindowControl interface {
	Show()
	Focus()
}

// SyncStatus is the launch staleness verdict surfaced to the IDLE screen
// (design-log/031). Behind is true when the remote HEAD is newer than the
// local HEAD — a passive cue, not a count (no sequence ids; see OQ2). Heads
// are the canonical RefID timestamp strings, empty when that side has no
// refs yet.
type SyncStatus struct {
	Behind     bool   `json:"behind"`
	LocalHead  string `json:"localHead"`
	RemoteHead string `json:"remoteHead"`
}

// SyncProber resolves local + remote HEAD and reports staleness. Injected
// at composition over the two HeadResolvers; nil in tests (GetSyncStatus
// then returns a zero status). Errors (offline, list failure) propagate so
// GetSyncStatus can degrade silently.
type SyncProber func(ctx context.Context) (SyncStatus, error)

// NewHeadSyncProber builds the launch staleness prober (design-log/031):
// resolve both heads, report Behind when remote > local. RefID is a
// timestamp so the lexical compare is chronological. An empty side
// (pulling.ErrNoHead) reads as "" — a fresh local is behind any non-empty
// remote, and nothing is behind an empty remote. A real listing error on
// either side propagates (GetSyncStatus then degrades to zero status).
func NewHeadSyncProber(localHead, remoteHead pulling.HeadResolver) SyncProber {
	return func(ctx context.Context) (SyncStatus, error) {
		remote, err := resolveHeadOrEmpty(ctx, remoteHead)
		if err != nil {
			return SyncStatus{}, fmt.Errorf("resolve remote head: %w", err)
		}
		local, err := resolveHeadOrEmpty(ctx, localHead)
		if err != nil {
			return SyncStatus{}, fmt.Errorf("resolve local head: %w", err)
		}
		return SyncStatus{
			Behind:     remote > local,
			LocalHead:  string(local),
			RemoteHead: string(remote),
		}, nil
	}
}

// resolveHeadOrEmpty maps the empty-storage sentinel (pulling.ErrNoHead) to
// "" so the staleness compare treats an empty side as "no refs". Real
// listing failures propagate.
func resolveHeadOrEmpty(ctx context.Context, resolve pulling.HeadResolver) (domain.RefID, error) {
	id, err := resolve(ctx)
	if errors.Is(err, pulling.ErrNoHead) {
		return "", nil
	}
	return id, err
}

// ControlService is the Wails service the main window binds to. It exposes
// the user commands (Start/Stop/Dismiss/Download/Upload), the initial
// snapshot, the launch staleness check, and the "show logs window" action.
type ControlService struct {
	bus      ports.EventBus
	snapshot SnapshotSource
	logs     WindowControl
	sync     SyncProber
}

// NewControlService wires the service to the shared bus, projection, sync
// prober, and logs window control. Any of the arguments may be nil only in
// tests; a nil sync prober makes GetSyncStatus a no-op zero status.
func NewControlService(bus ports.EventBus, snapshot SnapshotSource, sync SyncProber, logs WindowControl) *ControlService {
	return &ControlService{bus: bus, snapshot: snapshot, sync: sync, logs: logs}
}

// Start persists the user-supplied port + memory and publishes a
// StartRequested command on the bus. Validation mirrors domain.Settings
// so bad inputs never reach the Ritual orchestrator.
func (c *ControlService) Start(port int, memoryMB int) error {
	if port <= 0 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if memoryMB <= 0 {
		return errors.New("memory must be positive")
	}
	settings, err := domain.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	settings.Port = port
	settings.Memory = memoryMB
	if err := settings.Save(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	c.bus.Publish(ritual.StartRequested{})
	return nil
}

// Stop publishes a StopRequested command. The Ritual orchestrator
// decides whether it is a legal stop at this moment.
func (c *ControlService) Stop() {
	c.bus.Publish(ritual.StopRequested{})
}

// Dismiss publishes a DismissRequested command; the Ritual orchestrator
// rejects the request if status is not Failed. Replaces the prior
// retry-from-failed flow (see design-log/017): the user acknowledges the
// failure, the UI returns to Idle, and a subsequent Start begins fresh.
func (c *ControlService) Dismiss() {
	c.bus.Publish(ritual.DismissRequested{})
}

// Download publishes a DownloadRequested command — the server-free,
// lockless refresh of the local workdir from the remote HEAD
// (design-log/031). The lifecycle rejects it while another flow is Running.
func (c *ControlService) Download() {
	c.bus.Publish(ritual.DownloadRequested{})
}

// Upload publishes an UploadRequested command — the server-free publish of
// the local worlds as a new remote ref parented on the current remote HEAD
// (design-log/031). The lifecycle rejects it while another flow is Running.
func (c *ControlService) Upload() {
	c.bus.Publish(ritual.UploadRequested{})
}

// GetSyncStatus runs the launch staleness check: resolve local + remote
// HEAD and report whether the remote is newer (design-log/031). The
// frontend calls this once on mount to drive the IDLE "Remote is newer"
// caption. A nil prober or any error (offline, list failure) collapses to a
// zero status so the IDLE screen simply shows nothing — never an error.
func (c *ControlService) GetSyncStatus() SyncStatus {
	if c.sync == nil {
		return SyncStatus{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncProbeTimeout)
	defer cancel()
	status, err := c.sync(ctx)
	if err != nil {
		return SyncStatus{}
	}
	return status
}

// GetSnapshot returns the current GUI view model. The frontend calls this
// once on mount so the first render has real state before the first Emit
// arrives.
func (c *ControlService) GetSnapshot() projection.ViewModel {
	return c.snapshot.Snapshot()
}

// Prep is the bind-time server parameters surfaced to the IDLE-screen
// advanced-settings disclosure. Falls back to DefaultSettings values if
// the settings file is missing or malformed.
type Prep struct {
	Port     int `json:"port"`
	MemoryMB int `json:"memoryMB"`
}

// GetPrep returns the persisted port + memory so the frontend can render
// the prep-settings disclosure with the user's last-saved values. Errors
// during load collapse to defaults — the disclosure always renders.
func (c *ControlService) GetPrep() Prep {
	defaults := domain.DefaultSettings()
	settings, err := domain.LoadSettings()
	if err != nil || settings == nil {
		return Prep{Port: defaults.Port, MemoryMB: defaults.Memory}
	}
	return Prep{Port: settings.Port, MemoryMB: settings.Memory}
}

// SendConsole forwards a user-typed line from the logs window to the
// running server's stdin via a ConsoleInput bus event. No-op when no
// server is running — the running-stage coordinator is the sole consumer.
func (c *ControlService) SendConsole(line string) {
	c.bus.Publish(running.ConsoleInput{Text: line})
}

// ShowLogs reveals the logs console window. Preloaded at startup, so
// Show() is near-instant. No-op until SetLogsWindow has been called — the
// logs window is created after services.NewControlService in the Wails
// composition root.
func (c *ControlService) ShowLogs() {
	if c.logs == nil {
		return
	}
	c.logs.Show()
	c.logs.Focus()
}

// SetLogsWindow attaches the logs window control after construction. The
// composition root calls this once the Wails WebviewWindow exists.
func (c *ControlService) SetLogsWindow(logs WindowControl) { c.logs = logs }

// OpenRootFolder reveals the Ritual working root (config.RootPath) in the
// OS file manager. Used by the "Show folder" button in the main window so
// users can reach synced worlds, logs, and settings without knowing the
// path.
func (c *ControlService) OpenRootFolder() error {
	return revealFolder(config.RootPath)
}

func revealFolder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path) //nolint:gosec // path is config.RootPath, not user input
	case "darwin":
		cmd = exec.Command("open", path) //nolint:gosec // path is config.RootPath, not user input
	default:
		cmd = exec.Command("xdg-open", path) //nolint:gosec // path is config.RootPath, not user input
	}
	return cmd.Start()
}
