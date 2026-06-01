// Package main launches the Wails GUI with the embedded frontend.
//
// Composition root. The pipeline runs the real NeoForge launcher
// (settings.StartScript, default "start.bat") via
// adapters.NewServerCmdBuilder and probes 127.0.0.1:<settings.Port>
// via adapters.NewTCPReadinessCheck. The "remote" backend is still a
// local-filesystem mock (rate-limited via ThrottledStorage) until the
// R2 wiring lands.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	ritualassets "ritual"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/adapters/progress"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/lock"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/ritual"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/gui/control"
	"ritual/internal/gui/logsink"
	"ritual/internal/gui/netinfo"
	"ritual/internal/gui/projection"
	"ritual/internal/subsystems/lifecycle"
	"ritual/internal/subsystems/livesync"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/pipeline"
	"ritual/internal/subsystems/remote"
	"ritual/internal/subsystems/retention"
	"ritual/internal/subsystems/transferwatch"
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
	config.LoadEnvFiles()

	runtime, err := buildRuntime()
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}

	controlSvc := control.NewControlService(runtime.bus, runtime.projection, runtime.syncProber, runtime.dirtyProber, nil)

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
		Name:                "main",
		Title:               config.ProductName,
		Width:               560,
		Height:              720,
		DisableResize:       true,
		MaximiseButtonState: application.ButtonHidden,
		BackgroundColour:    application.NewRGB(27, 38, 54),
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
	// Live-sync subsystem (design-log/016). 5-min Commit+Push tick during
	// the Running stage; ServerReadyInfo starts it, lifecycle events stop
	// it. parentFn tracks pulling.HeadResolvedInfo so the ticker never
	// reads RunState directly. Dispatcher writes LiveDraftCommitted into
	// rs.RefID (OQ4 Option A); session hook rebinds the target per run.
	// Drainer pre-stage waits up to 10s (OQ5) for the ticker + dispatcher
	// to settle before Committing reads rs.RefID.
	parentFn, stopParent := livesync.ParentFromBus(runtime.bus)
	defer stopParent()
	ticker, engine, stopLiveSync := livesync.New(
		runtime.bus,
		runtime.committer,
		runtime.pusher,
		runtime.commitTargets,
		parentFn,
		livesync.DefaultInterval,
		livesync.DefaultSaveTimeout,
	)
	defer stopLiveSync()
	dispatcher, stopDispatcher := livesync.NewDispatcher(runtime.bus, nil)
	defer stopDispatcher()
	drainer := livesync.NewDrainer(ticker, engine, dispatcher, livesync.DefaultDrainTimeout)
	pipelineDeps := runtime.pipelineDeps
	pipelineDeps.Drainable = drainer
	// Three flows from shared stage nodes (design-log/031). Download/Upload
	// ignore d.Drainable (no Running, no livesync), so passing the session
	// deps verbatim is harmless.
	entries := lifecycle.Entries{
		Session:      pipeline.Build(pipelineDeps),
		LocalSession: pipeline.BuildLocalSession(pipelineDeps),
		Download:     pipeline.BuildDownload(pipelineDeps),
		Upload:       pipeline.BuildUpload(pipelineDeps),
	}

	sessionHook := func(rs *ritual.RunState) {
		dispatcher.SetTarget(func(id domain.RefID) { rs.RefID = id })
	}
	stopLifecycle := lifecycle.Attach(ctx, runtime.bus, entries, sessionHook)
	defer stopLifecycle()
	defer runtime.stopLogFile()
	go runtime.projection.Run(ctx)
	go runtime.logsink.Run(ctx)
	go runtime.ticker.Run(ctx)
	// Arm the ticker's stall-heartbeat for the wire-transfer windows only, so a
	// quiet R2 PutStream still pulses liveness. Subscribes on New (before Run)
	// to avoid missing the first StateChanged. Design-log/022 #2.
	go transferwatch.New(runtime.bus, runtime.ticker).Run(ctx)

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

// guiRuntime bundles everything the Wails app needs after composition.
// Kept internal to cmd/gui — not a public API.
type guiRuntime struct {
	bus           ports.EventBus
	pipelineDeps  pipeline.Deps
	projection    *projection.Projection
	logsink       *logsink.Sink
	viewEmitter   *wailsViewEmitter
	logEmitter    *wailsLogEmitter
	ticker        *progress.Ticker
	stopLogFile   func()
	committer     ports.Committer
	pusher        ports.Pusher
	commitTargets []string
	syncProber    control.SyncProber
	dirtyProber   control.LocalDirtyProber
}

func buildRuntime() (*guiRuntime, error) {
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		return nil, fmt.Errorf("create root: %w", err)
	}
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}

	bus := adapters.NewEventBus(4096)

	rawLocal, err := adapters.NewFSRepository(workRoot, "local")
	if err != nil {
		return nil, fmt.Errorf("local storage: %w", err)
	}

	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	// Remote backend selected at runtime by RITUAL_REMOTE_MODE
	// (design-log/030). Default ModeR2 reads credentials from
	// RITUAL_R2_* — typically loaded by config.LoadEnvFiles from
	// .env.{RITUAL_ENV}.local at startup. Set RITUAL_REMOTE_MODE=mock
	// to opt into the local-FS dev backend without a rebuild.
	rawRemote, err := remote.Build(context.Background(), remote.ResolveModeFromEnv(), bus)
	if err != nil {
		return nil, fmt.Errorf("remote storage: %w", err)
	}
	// Decorate every remote backend (R2 and mock) with retry on classified
	// transient errors. R2 needs it for mid-stream body EOFs the SDK can't
	// recover from (design-log/004); mock pays nothing because the default
	// classifier rejects mock-side terminal errors. Wrap *under* the
	// counter layers so retried bytes count toward wire traffic and the
	// speed metric stays honest (design-log/004 §Q4).
	rawRemote = adapters.NewRetryingStorage(rawRemote, adapters.DefaultRetryPolicy(), bus)

	// Blob-store decorator stack — two counter layers around compression
	// (design-log/001-progress-projection.md). Outside-in:
	//
	//   caller ─► Counter(logical) ─► Compressing ─► Counter(wire) ─► rawFS
	//
	// Logical counter (above compression) sees uncompressed bytes the caller
	// asked for / handed in — drives BytesTotal / BytesDone for the progress
	// bar (matches PlanInfo, which sums FileEntry.Size in logical bytes).
	//
	// Wire counter (below compression) sees the bytes that physically cross
	// the backend boundary — drives the smoothed speed label (and matches an
	// operator's mental model of uplink/downlink).
	//
	// PrefixRouter's "else" branch (refs/, lock, settings) points at the
	// wire-counter-wrapped raw, so every byte that touches the backend lands
	// in the wire counter regardless of which route it took. Compression
	// stays gated to objects/ — human-readable JSON keeps hitting raw FS
	// untouched (audit fix #5).
	localWire := &adapters.StorageCounters{}
	remoteWire := &adapters.StorageCounters{}
	localBackend := adapters.NewCounterStorage(rawLocal, localWire)
	remoteBackend := adapters.NewCounterStorage(rawRemote, remoteWire)

	localCompressed, err := adapters.NewCompressingStorage(localBackend)
	if err != nil {
		return nil, fmt.Errorf("local compressing storage: %w", err)
	}
	remoteCompressed, err := adapters.NewCompressingStorage(remoteBackend)
	if err != nil {
		return nil, fmt.Errorf("remote compressing storage: %w", err)
	}

	localLogical := &adapters.StorageCounters{}
	remoteLogical := &adapters.StorageCounters{}
	localObjects := adapters.NewCounterStorage(localCompressed, localLogical)
	remoteObjects := adapters.NewCounterStorage(remoteCompressed, remoteLogical)
	localStorage := observed.NewStorage(adapters.NewPrefixRouter("objects/", localObjects, localBackend), bus)
	remoteStorage := observed.NewStorage(adapters.NewPrefixRouter("objects/", remoteObjects, remoteBackend), bus)

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
	localHeadResolver := pulling.NewHeadResolver(localStorage)
	committer := refs.NewCommitter(scanner, workdirStorage, localStorage, runner)
	pusher := refs.NewPusher(localStorage, remoteStorage, runner)
	commitTargets := config.DefaultCommitTargets

	// Launch staleness prober (design-log/031): remote HEAD vs local HEAD.
	syncProber := control.NewHeadSyncProber(localHeadResolver, headResolver)

	// Workdir dirty prober (design-log/035): is the workdir different from the
	// local HEAD ref? readRef loads refs/{id}.json from local storage; scan is
	// an mtime-bounded workdir hash seeded from the ref's Objects so only files
	// touched since the last commit are re-hashed.
	readRef := func(ctx context.Context, id domain.RefID) (*domain.Ref, error) {
		rc, err := localStorage.GetStream(ctx, "refs/"+string(id)+".json")
		if err != nil {
			return nil, fmt.Errorf("read ref %s: %w", id, err)
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("read ref %s body: %w", id, err)
		}
		ref := &domain.Ref{}
		if err := json.Unmarshal(raw, ref); err != nil {
			return nil, fmt.Errorf("parse ref %s: %w", id, err)
		}
		return ref, nil
	}
	workdirScan := func(ctx context.Context, since time.Time, previous map[string]domain.FileEntry, targets []string) (map[string]domain.FileEntry, error) {
		sc, err := adapters.NewMtimeScanner(config.RootPath, since, previous)
		if err != nil {
			return nil, err
		}
		return sc.Scan(ctx, targets)
	}
	dirtyProber := control.NewLocalDirtyProber(localHeadResolver, readRef, workdirScan, commitTargets)

	// Wire pull/push plan callbacks into the bus so the projection can
	// populate ViewModel.BytesTotal before the first progress.Tick lands —
	// audit open item #1: without this the bar reads 0%% the whole transfer
	// even though BytesDone climbs every second.
	puller.OnPlan(func(p ritual.PlanInfo) { bus.Publish(p) })
	pusher.OnPlan(func(p ritual.PlanInfo) { bus.Publish(p) })

	// Real NeoForge launcher: settings.StartScript (default "start.bat")
	// resolved relative to <root>/server/. Operators may override via
	// settings.json — empty/missing falls back to domain.DefaultStartScript.
	serverPath := filepath.Join(config.RootPath, config.ServerDir)
	if err := os.MkdirAll(serverPath, config.DirPermission); err != nil {
		return nil, fmt.Errorf("create server dir: %w", err)
	}
	serverRoot, err := os.OpenRoot(serverPath)
	if err != nil {
		return nil, fmt.Errorf("open server dir: %w", err)
	}
	cmdBuilder, err := adapters.NewServerCmdBuilder(serverRoot, settings.StartScript, settings.ToServerRuntime)
	if err != nil {
		return nil, fmt.Errorf("server cmd builder: %w", err)
	}

	readiness := adapters.NewTCPReadinessCheck(fmt.Sprintf("127.0.0.1:%d", settings.Port), bus)

	localRets, remoteRets, err := retention.Build(localStorage, remoteStorage, bus)
	if err != nil {
		return nil, fmt.Errorf("retention: %w", err)
	}

	// Local + remote lock stack: lock.Both calls the local lease first so
	// a same-host PID that already grabbed <root>/lock cannot pin a remote
	// lease that no live process can release. Both sides reuse lock.Locker
	// — no Windows API, no new adapter — per audit open item #0.
	host, _ := os.Hostname()
	localLocker := observed.NewLocker(lock.New(localStorage, host), bus)
	remoteLocker := observed.NewLocker(lock.New(remoteStorage, host), bus)
	locker := lock.NewBoth(localLocker, remoteLocker)
	pipelineDeps := pipeline.Deps{
		Bus:               bus,
		Checks:            nil, // no conditions for POC
		Puller:            puller,
		Applier:           applier,
		HeadResolver:      headResolver,
		LocalHeadResolver: localHeadResolver,
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
		// Drainable filled in by main() after the livesync ticker exists.
	}

	// Progress ticker over both storage sides. Remote side captures the
	// network throughput (Pull/Push). Local side captures Apply (read from
	// local objects/ → workdir) and Commit (workdir → local objects/) so
	// disk-side activity is visible alongside network activity in every
	// Tick — same cadence, same log block, single source of timing.
	remoteTicker := progress.NewTicker(
		progress.CounterSide{Logical: remoteLogical, Wire: remoteWire},
		progress.CounterSide{Logical: localLogical, Wire: localWire},
		bus, time.Second,
	)

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
		bus:           bus,
		pipelineDeps:  pipelineDeps,
		projection:    proj,
		logsink:       sink,
		viewEmitter:   viewEmitter,
		logEmitter:    logEmitter,
		ticker:        remoteTicker,
		stopLogFile:   stopLogFile,
		committer:     committer,
		pusher:        pusher,
		commitTargets: commitTargets,
		syncProber:    syncProber,
		dirtyProber:   dirtyProber,
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
