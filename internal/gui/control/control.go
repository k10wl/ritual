// Package control is the driving adapter for the GUI: Wails-bound method
// surfaces that translate frontend (JS) calls into bus commands or
// snapshot lookups. No business logic.
package control

import (
	"errors"
	"fmt"
	"os/exec"
	"ritual/internal/core/ritual"
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
	c.bus.Publish(ritual.StartRequested{})
	return nil
}

// Stop publishes a StopRequested command. The Ritual orchestrator
// decides whether it is a legal stop at this moment.
func (c *ControlService) Stop() {
	c.bus.Publish(ritual.StopRequested{})
}

// Retry publishes a RetryRequested command; the Ritual orchestrator
// rejects the request if status is not Failed.
func (c *ControlService) Retry() {
	c.bus.Publish(ritual.RetryRequested{})
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
