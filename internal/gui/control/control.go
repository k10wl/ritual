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

// SyncStatus is the sync verdict surfaced to the IDLE screen + Sync pane
// (design-log/031, extended by /035). Heads are the canonical RefID timestamp
// strings, empty when that side has no refs yet.
//
//   - Behind   — remote HEAD newer than local HEAD (a teammate published); a
//     non-blocking warning when publishing (design-log/035 §Q4c).
//   - Unpushed — local HEAD newer than remote HEAD: committed work that never
//     reached the remote (a /036 skip-sync session, or a crashed push). One
//     tap publishes the existing ref — the castle-on-a-plane recovery (§Q7).
//   - Dirty    — workdir differs from the local HEAD ref: uncommitted edits.
//
// The Sync pane offers "Publish" on Dirty || Unpushed; the IDLE cue
// "Unpublished changes" shows on the same condition (design-log/035 §Q6).
type SyncStatus struct {
	Behind     bool   `json:"behind"`
	Unpushed   bool   `json:"unpushed"`
	Dirty      bool   `json:"dirty"`
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
			Unpushed:   local > remote,
			LocalHead:  string(local),
			RemoteHead: string(remote),
		}, nil
	}
}

// RefReader loads a ref by id from a store (refs/{id}.json → domain.Ref).
// Injected so control stays free of storage/json wiring (composition root
// supplies the closure over localStorage).
type RefReader func(ctx context.Context, id domain.RefID) (*domain.Ref, error)

// WorkdirScan hashes the workdir under the given scope, re-hashing only files
// modified after `since` and carrying `previous` forward (an mtime scanner).
// Injected so control owns no filesystem.
type WorkdirScan func(ctx context.Context, since time.Time, previous map[string]domain.FileEntry, targets []string) (map[string]domain.FileEntry, error)

// LocalDirtyProber reports whether the workdir differs from the local HEAD
// ref (design-log/035 §Q5). Injected at composition; nil ⇒ GetSyncStatus
// leaves Dirty false. Errors degrade silently (never surface to the IDLE
// screen — design-log/031 invariant).
type LocalDirtyProber func(ctx context.Context) (dirty bool, err error)

// NewLocalDirtyProber builds the dirty probe (design-log/035): resolve the
// local HEAD ref, scan the workdir against its Objects (mtime-bounded), and
// report any path added / removed / hash-changed. The RefID is the commit
// timestamp, so it seeds the scanner's `since`. ErrNoHead (no local ref yet)
// ⇒ any in-scope file on disk counts as dirty (the seed case). seedTargets is
// the commit scope used only when there is no ref to read its own Targets.
func NewLocalDirtyProber(localHead pulling.HeadResolver, readRef RefReader, scan WorkdirScan, seedTargets []string) LocalDirtyProber {
	return func(ctx context.Context) (bool, error) {
		head, err := localHead(ctx)
		if errors.Is(err, pulling.ErrNoHead) {
			files, serr := scan(ctx, time.Time{}, nil, seedTargets)
			if serr != nil {
				return false, serr
			}
			return len(files) > 0, nil
		}
		if err != nil {
			return false, err
		}
		ref, err := readRef(ctx, head)
		if err != nil {
			return false, err
		}
		since, perr := time.Parse(domain.RefIDFormat, string(head))
		if perr != nil {
			// Un-parseable id → re-hash everything (since = zero time). Safe:
			// over-hashing only costs CPU, never a false "clean".
			since = time.Time{}
		}
		prev := objectsToEntries(ref.Objects)
		cur, err := scan(ctx, since, prev, ref.Targets)
		if err != nil {
			return false, err
		}
		return entriesDiffer(cur, prev), nil
	}
}

// objectsToEntries adapts a ref's Objects (Hash+Size) to the FileEntry shape
// the scanner returns + carries forward.
func objectsToEntries(objs map[string]domain.Object) map[string]domain.FileEntry {
	out := make(map[string]domain.FileEntry, len(objs))
	for path, o := range objs {
		out[path] = domain.FileEntry{Hash: o.Hash, Size: o.Size}
	}
	return out
}

// entriesDiffer reports whether two path→entry maps describe different content
// by hash. Equal length plus every cur key present in prev with the same hash
// ⟹ identical sets (a removed path changes the length).
func entriesDiffer(cur, prev map[string]domain.FileEntry) bool {
	if len(cur) != len(prev) {
		return true
	}
	for path, c := range cur {
		p, ok := prev[path]
		if !ok || p.Hash != c.Hash {
			return true
		}
	}
	return false
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
	dirty    LocalDirtyProber
}

// NewControlService wires the service to the shared bus, projection, sync
// prober, dirty prober, and logs window control. Any of the arguments may be
// nil only in tests; a nil sync prober makes GetSyncStatus a zero status and a
// nil dirty prober leaves Dirty false.
func NewControlService(bus ports.EventBus, snapshot SnapshotSource, sync SyncProber, dirty LocalDirtyProber, logs WindowControl) *ControlService {
	return &ControlService{bus: bus, snapshot: snapshot, sync: sync, dirty: dirty, logs: logs}
}

// Start persists the user-supplied port + memory and publishes a
// StartRequested command on the bus. Validation mirrors domain.Settings
// so bad inputs never reach the Ritual orchestrator.
// skipSync selects the local-only session pipeline (design-log/036): the
// server runs on the on-disk worlds with all remote sync skipped (offline /
// R2-down / rollback / mod-testing). Transient — not persisted; the frontend
// resets the toggle OFF each launch.
func (c *ControlService) Start(port int, memoryMB int, skipSync bool) error {
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
	c.bus.Publish(ritual.StartRequested{SkipSync: skipSync})
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
	ctx, cancel := context.WithTimeout(context.Background(), syncProbeTimeout)
	defer cancel()
	var status SyncStatus
	// Head compare (Behind / Unpushed) and the workdir dirty scan degrade
	// independently — an offline remote still lets the local dirty check run,
	// and a ref-read failure still lets the head compare stand. Each error
	// leaves its half of the verdict false (design-log/031 OQ3, /035 §L1).
	if c.sync != nil {
		if s, err := c.sync(ctx); err == nil {
			status = s
		}
	}
	if c.dirty != nil {
		if d, err := c.dirty(ctx); err == nil {
			status.Dirty = d
		}
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
