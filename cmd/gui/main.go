// Package main launches the Wails GUI with the embedded frontend.
//
// This is a POC composition root. The pipeline is wired against the
// fakerun test fixture (cmd/fakerun) and a local-filesystem "remote"
// store so the GUI is driveable end-to-end without a real Minecraft
// server or R2 credentials. Every POC-only line is tagged
// // TODO(ritual-gui-poc): so it is trivial to grep-and-swap when the
// real cmd builder / R2 storage wiring lands.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	ritualassets "ritual"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/adapters/progress"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/pipeline"
	"ritual/internal/subsystems/retention"
	"ritual/internal/gui/control"
	"ritual/internal/gui/logsink"
	"ritual/internal/gui/netinfo"
	"ritual/internal/gui/projection"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func init() {
	application.RegisterEvent[projection.ViewModel]("ritual:view")
	application.RegisterEvent[logsink.LogLine]("log:line")
}

func main() {
	runtime, err := buildRuntime()
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}

	controlSvc := control.NewControlService(runtime.bus, runtime.projection, nil)

	wailsApp := application.New(application.Options{
		Name:        config.ProductName,
		Description: "Ritual — Minecraft server manager (POC)",
		Services: []application.Service{
			application.NewService(controlSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(ritualassets.GUIAssets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	var shuttingDown atomic.Bool

	logsWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "logs",
		Title:            config.ProductName + " — Logs",
		Width:            960,
		Height:           640,
		Hidden:           true,
		BackgroundColour: application.NewRGB(16, 20, 28),
		URL:              "/logs.html",
	})
	logsWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if shuttingDown.Load() {
			return
		}
		e.Cancel()
		logsWindow.Hide()
	})

	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:  "main",
		Title: config.ProductName,
		Width: 560, Height: 720,
		MinWidth: 420, MinHeight: 560,
		BackgroundColour: application.NewRGB(27, 38, 54),
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})

	runtime.viewEmitter.bind(wailsApp)
	runtime.logEmitter.bind(logsWindow)
	controlSvc.SetLogsWindow(&wailsWindowControl{win: logsWindow})

	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if shuttingDown.Load() {
			return
		}
		shuttingDown.Store(true)
		e.Cancel()
		go func() {
			runtime.bus.Publish(ritual.StopRequested{})
			waitTerminal(runtime.bus, 20*time.Second)
			wailsApp.Quit()
		}()
	})

	ctx := wailsApp.Context()
	stopLifecycle := lifecycle.Attach(ctx, runtime.bus, runtime.entry)
	defer stopLifecycle()
	defer runtime.stopLogFile()
	go runtime.projection.Run(ctx)
	go runtime.logsink.Run(ctx)
	go runtime.ticker.Run(ctx)

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

// guiRuntime bundles everything the Wails app needs after composition.
// Kept internal to cmd/gui — not a public API.
type guiRuntime struct {
	bus         ports.EventBus
	entry       machine.Strategy[ritual.RunState]
	locker      *observed.Locker
	projection  *projection.Projection
	logsink     *logsink.Sink
	viewEmitter *wailsViewEmitter
	logEmitter  *wailsLogEmitter
	ticker      *progress.Ticker
	stopLogFile func()
}

func buildRuntime() (*guiRuntime, error) {
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		return nil, fmt.Errorf("create root: %w", err)
	}
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}

	// TODO(ritual-gui-poc): remote storage is a local sibling directory so
	// the whole pipeline is driveable without R2 credentials. Swap for a
	// real remote adapter (adapters.NewR2Repository) when productionizing.
	mockRemoteDir := filepath.Join(config.RootPath, "remote-mock")
	if err := os.MkdirAll(mockRemoteDir, config.DirPermission); err != nil {
		return nil, fmt.Errorf("create mock remote: %w", err)
	}
	remoteRoot, err := os.OpenRoot(mockRemoteDir)
	if err != nil {
		return nil, fmt.Errorf("open mock remote: %w", err)
	}

	bus := adapters.NewEventBus(4096)

	rawLocal, err := adapters.NewFSRepository(workRoot, "local")
	if err != nil {
		return nil, fmt.Errorf("local storage: %w", err)
	}
	rawRemote, err := adapters.NewFSRepository(remoteRoot, "remote")
	if err != nil {
		return nil, fmt.Errorf("mock remote storage: %w", err)
	}

	// Blob-store decorator stack: raw FS → compressing (silent, integrity
	// verified) → counter (byte/op tap for the progress ticker) → observed
	// (per-op lifecycle events). Observed is outermost so GUI events carry
	// raw byte sizes; the ticker reads counter atomics to emit live Mbps.
	localCompressed, err := adapters.NewCompressingStorage(rawLocal)
	if err != nil {
		return nil, fmt.Errorf("local compressing storage: %w", err)
	}
	remoteCompressed, err := adapters.NewCompressingStorage(rawRemote)
	if err != nil {
		return nil, fmt.Errorf("remote compressing storage: %w", err)
	}
	localCounters := &adapters.StorageCounters{}
	remoteCounters := &adapters.StorageCounters{}
	localStorage := observed.NewStorage(adapters.NewCounterStorage(localCompressed, localCounters), bus)
	remoteStorage := observed.NewStorage(adapters.NewCounterStorage(remoteCompressed, remoteCounters), bus)

	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	// Workdir is the project root. Scope is data-driven by commitTargets
	// below — operational dirs (refs/, objects/, logs/, remote-mock/),
	// settings.json, and server/.cache live under root but are absent from
	// the allowlist, so the scanner ignores them and Apply never prunes
	// them. Audit fix #8 (docs/dev-session-2026-04-25-poc-setup.md):
	// pre-fix workdir was <root>/worlds and a fresh host could not pull-
	// and-run because nothing under server/ was tracked.
	rawWorkdir, err := adapters.NewFSRepository(workRoot, "workdir")
	if err != nil {
		return nil, fmt.Errorf("workdir storage: %w", err)
	}
	workdirStorage := observed.NewStorage(rawWorkdir, bus)
	scanner := adapters.NewFullScanner(os.DirFS(config.RootPath))

	// Refs V2 pipeline: ParallelRunner(10) shared by Pull (remote → local
	// blob download concurrency) and Apply (local blob → workdir placement).
	// Weight-desc dispatch: heaviest blobs start first so ETA stabilises and
	// the tail-blob straggler shrinks (spec §2695).
	const pullConcurrency = 10
	runner := adapters.NewParallelRunner(pullConcurrency)
	puller := refs.NewPuller(remoteStorage, localStorage, runner)
	applier := refs.NewApplier(localStorage, workdirStorage, scanner, runner)
	headResolver := pulling.NewHeadResolver(remoteStorage)
	committer := refs.NewCommitter(scanner, workdirStorage, localStorage, runner)
	pusher := refs.NewPusher(localStorage, remoteStorage, runner)
	commitTargets := config.DefaultCommitTargets

	// TODO(ritual-gui-poc): fakerun stands in for the Minecraft server so
	// the GUI loop can be exercised without a JRE. Replace with
	// adapters.NewServerCmdBuilder once wiring is proven.
	fakerunBin, err := locateFakerun()
	if err != nil {
		return nil, fmt.Errorf("locate fakerun: %w", err)
	}
	cmdBuilder := &fakerunCmdBuilder{bin: fakerunBin, root: config.RootPath}

	// TODO(ritual-gui-poc): fakerun has no TCP listener, so readiness is
	// declared immediately. Swap for adapters.NewTCPReadinessCheck bound
	// to 127.0.0.1:<settings.Port> when the real server wires in.
	readiness := immediateReady{}

	localRets, remoteRets, err := retention.Build(localStorage, remoteStorage, bus)
	if err != nil {
		return nil, fmt.Errorf("retention: %w", err)
	}

	host, _ := os.Hostname()
	locker := observed.NewLocker(lock.New(remoteStorage, host), bus)
	entry := pipeline.Build(pipeline.Deps{
		Bus:               bus,
		Checks:            nil, // no conditions for POC
		Puller:            puller,
		Applier:           applier,
		HeadResolver:      headResolver,
		Committer:         committer,
		CommitOpts:        ritual.NewCommitOptsResolver(commitTargets),
		Pusher:            pusher,
		LocalRetentions:   localRets,
		RemoteRetentions:  remoteRets,
		CmdBuilder:        cmdBuilder,
		Readiness:         readiness,
		AcquireFn:         locker.Acquire,
		InspectFn:         locker.Inspect,
		ReleaseFn:         locker.Release,
		HeartbeatInterval: locker.HeartbeatInterval(),
	})

	// Progress ticker over remote counters — downloads dominate user-visible
	// throughput during Pull. Local counters stay wired for a future ticker
	// when Apply/Commit traffic surfacing is added.
	_ = localCounters
	remoteTicker := progress.NewTicker(remoteCounters, bus, time.Second)

	viewEmitter := newWailsViewEmitter()
	logEmitter := &wailsLogEmitter{}

	addresses := netinfo.NewAddressProvider(settings.Port, netinfo.NewSysInterfaceLister())
	proj := projection.New(bus, viewEmitter, addresses)
	sink := logsink.New(bus, logEmitter)

	// Audit fix #6 (docs/dev-session-2026-04-25-poc-setup.md): the
	// in-memory logsink above feeds the GUI logs window only. Persist
	// the same bus stream to <root>/logs/<ts>.log so a session leaves an
	// on-disk record an operator can inspect after the GUI window is gone.
	stopLogFile, err := logging.Build(bus, workRoot)
	if err != nil {
		return nil, fmt.Errorf("logging build: %w", err)
	}

	return &guiRuntime{
		bus:         bus,
		entry:       entry,
		locker:      locker,
		projection:  proj,
		logsink:     sink,
		viewEmitter: viewEmitter,
		logEmitter:  logEmitter,
		ticker:      remoteTicker,
		stopLogFile: stopLogFile,
	}, nil
}

func waitTerminal(bus ports.EventBus, budget time.Duration) {
	ch, unsub := bus.Subscribe()
	defer unsub()
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			sc, ok := evt.(lifecycle.StatusChanged)
			if !ok {
				continue
			}
			if sc.Status == lifecycle.Done || sc.Status == lifecycle.Failed || sc.Status == lifecycle.Idle {
				return
			}
		case <-deadline.C:
			return
		}
	}
}

// wailsViewEmitter forwards ViewModel snapshots to the Wails event bridge.
// Latest-wins: Emit never blocks the projection fold loop. A dedicated
// goroutine batches emissions — at most one Wails IPC round-trip in flight
// at a time, stale snapshots are dropped. This keeps the projection's
// subscription buffer empty even when Wails IPC is slow, so critical
// events like StatusChanged{Done} never drop under burst load.
type wailsViewEmitter struct {
	app     atomic.Pointer[application.App]
	pending atomic.Pointer[projection.ViewModel]
	wake    chan struct{}
}

func newWailsViewEmitter() *wailsViewEmitter {
	e := &wailsViewEmitter{wake: make(chan struct{}, 1)}
	go e.loop()
	return e
}

func (e *wailsViewEmitter) bind(a *application.App) { e.app.Store(a) }

func (e *wailsViewEmitter) Emit(vm projection.ViewModel) {
	e.pending.Store(&vm)
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *wailsViewEmitter) loop() {
	for range e.wake {
		vm := e.pending.Swap(nil)
		a := e.app.Load()
		if vm == nil || a == nil {
			continue
		}
		a.Event.Emit("ritual:view", *vm)
	}
}

// wailsLogEmitter window-scopes every LogLine to the logs console window.
// logsWindow.EmitEvent does not broadcast — main window never receives
// log traffic.
type wailsLogEmitter struct {
	win atomic.Pointer[application.WebviewWindow]
}

func (e *wailsLogEmitter) bind(w *application.WebviewWindow) { e.win.Store(w) }

func (e *wailsLogEmitter) Emit(line logsink.LogLine) {
	w := e.win.Load()
	if w == nil {
		return
	}
	w.EmitEvent("log:line", line)
}

// wailsWindowControl adapts a Wails WebviewWindow to services.WindowControl.
type wailsWindowControl struct{ win *application.WebviewWindow }

func (c *wailsWindowControl) Show()  { c.win.Show() }
func (c *wailsWindowControl) Focus() { c.win.Focus() }

// fakerunCmdBuilder is the POC CmdBuilder. fakerun reads JSON instructions
// from stdin and writes/deletes files under --root. The running stage
// streams output but the body of the game loop is a real subprocess — so
// lifecycle events (STARTING/STOPPING/STOPPED) fire exactly as they would
// for a real server.
type fakerunCmdBuilder struct {
	bin  string
	root string
}

func (b *fakerunCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, b.bin, "--root", b.root)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd, nil
}

// immediateReady short-circuits the readiness probe. Fakerun has no TCP
// socket; a real integration will dial the configured Minecraft port.
type immediateReady struct{}

func (immediateReady) Wait(context.Context) error { return nil }

// locateFakerun looks for a prebuilt fakerun binary next to the GUI binary
// and falls back to the Go build cache. On first launch the developer runs
// `go build -o ./bin/fakerun ritual/cmd/fakerun` once; lookups are stable
// thereafter.
func locateFakerun() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), fakerunName())
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath("fakerun"); err == nil {
		return p, nil
	}
	// TODO(ritual-gui-poc): last-ditch fallback — go run compiles on demand.
	// Remove when the production cmd builder replaces fakerun.
	if _, err := exec.LookPath("go"); err == nil {
		cwd, _ := os.Getwd()
		built := filepath.Join(cwd, "bin", fakerunName())
		if err := buildFakerun(built); err == nil {
			return built, nil
		}
	}
	return "", errors.New("fakerun binary not found: run `go build -o bin/fakerun ritual/cmd/fakerun`")
}

func fakerunName() string {
	if os.Getenv("GOOS") == "windows" || filepath.Ext(os.Args[0]) == ".exe" {
		return "fakerun.exe"
	}
	return "fakerun"
}

func buildFakerun(out string) error {
	if err := os.MkdirAll(filepath.Dir(out), config.DirPermission); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "ritual/cmd/fakerun") //nolint:gosec // POC-only on-demand build
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
