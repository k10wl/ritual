// Package services exposes Wails-bound service types for the GUI frontend.
// Composition root wires the bus, projection snapshot source, and logs window
// handle; services are pure method surfaces that translate JS calls into
// bus events or snapshot lookups. No business logic.
package services

import (
	"errors"
	"fmt"
	"os/exec"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/stages/running"
	"ritual/internal/gui/projection"
	"runtime"
)

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

// ControlService is the Wails service the main window binds to. It exposes
// the user commands (Start/Stop/Retry), the initial snapshot, and the
// "show logs window" action.
type ControlService struct {
	bus      ports.EventBus
	snapshot SnapshotSource
	logs     WindowControl
}

// NewControlService wires the service to the shared bus, projection, and
// logs window control. Any of the arguments may be nil only in tests.
func NewControlService(bus ports.EventBus, snapshot SnapshotSource, logs WindowControl) *ControlService {
	return &ControlService{bus: bus, snapshot: snapshot, logs: logs}
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
	c.bus.Publish(app.StartRequested{})
	return nil
}

// Stop publishes a StopRequested command. The Ritual orchestrator
// decides whether it is a legal stop at this moment.
func (c *ControlService) Stop() {
	c.bus.Publish(app.StopRequested{})
}

// Retry publishes a RetryRequested command; the Ritual orchestrator
// rejects the request if status is not Failed.
func (c *ControlService) Retry() {
	c.bus.Publish(app.RetryRequested{})
}

// GetSnapshot returns the current GUI view model. The frontend calls this
// once on mount so the first render has real state before the first Emit
// arrives.
func (c *ControlService) GetSnapshot() projection.ViewModel {
	return c.snapshot.Snapshot()
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
