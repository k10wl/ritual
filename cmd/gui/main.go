// Package main launches the Wails GUI with the embedded frontend.
//
// Composition root. The pipeline runs the real NeoForge launcher
// (settings.StartScript, default "start.bat") via
// adapters.NewServerCmdBuilder and probes 127.0.0.1:<settings.Port>
// via adapters.NewTCPReadinessCheck. The "remote" backend can be a plain
// local-filesystem mock (ModeMock, unthrottled — full disk speed for
// fast dev iteration) or real Cloudflare R2 (ModeR2).
package main

import (
	"context"
	"encoding/json"
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
	"ritual/internal/subsystems/loadedref"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/notify"
	"ritual/internal/subsystems/pipeline"
	"ritual/internal/subsystems/remote"
	"ritual/internal/subsystems/retention"
	"ritual/internal/subsystems/selfupdate"
	"ritual/internal/subsystems/transferwatch"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func init() {
	application.RegisterEvent[projection.ViewModel]("ritual:view")
	application.RegisterEvent[logsink.ServerLogBatch]("server:logs")
}

func main() { //nolint:gocyclo // composition root — high fanout is structural, not logical (mirrors buildRuntime below)
	config.LoadEnvFiles()

	runtime, err := buildRuntime()
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}

	controlSvc := control.NewControlService(runtime.bus, runtime.projection, runtime.syncProber, runtime.dirtyProber, runtime.versionLister, nil)
	// Design-log/045 §A — per-version delete + loaded-id clear, and §E — local
	// on-disk stats. Wired here rather than through the constructor to keep the
	// positional signature stable; tests stub these via the setters as needed.
	controlSvc.SetVersionDeleter(runtime.localVersionDeleter, runtime.loadedRefIDFn, runtime.clearLoadedRefID)
	controlSvc.SetRemoteVersionDeleter(runtime.remoteVersionDeleter)
	controlSvc.SetLocalStatsFn(runtime.localStatsFn)

	// OS notifications (design-log/047). Registered as a Wails service so its
	// ServiceStartup runs the Windows AppUserModelID/CLSID registration before
	// the first toast; the notify subsystem (attached below) drives it off the
	// bus. No runtime permission on Windows.
	notifSvc := notifications.New()

	appOptions := application.Options{
		Name:        config.DisplayName(),
		Description: "Ritual — Minecraft server manager (POC)",
		Services: []application.Service{
			application.NewService(controlSvc),
			application.NewService(notifSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(ritualassets.GUIAssets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	}
	if config.AppName == "ritualdev" {
		// Chrome DevTools Protocol on WebView2 (design-log/053): dev builds only,
		// lets an external CDP client (Playwright, etc.) attach to the real
		// window for live inspection/reproduction of GUI bugs. CDP's own
		// default binds loopback-only.
		appOptions.Windows = application.WindowsOptions{
			AdditionalBrowserArgs: []string{"--remote-debugging-port=9222"},
		}
	}
	wailsApp := application.New(appOptions)

	var shuttingDown atomic.Bool
	// lifecycleRunning mirrors the latest lifecycle.StatusChanged so the
	// main-window close hook can tell whether a graceful drain is needed.
	// Without this, waitTerminal blocked the full 20s budget on every close
	// because lifecycle.stop() is a no-op outside Running and no subsequent
	// StatusChanged is ever published — the app looked like it ignored close.
	var lifecycleRunning atomic.Bool

	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                "main",
		Title:               config.DisplayName(),
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
	// Lazy logs window (design-log/043): built on the first ShowLogs, never at
	// startup. The factory creates the window, wires its close→hide hook, binds
	// the console emitter, and returns a WindowControl. ShowLogs only fires from
	// the RUN-stage console affordance (no global entry).
	controlSvc.SetLogsWindowFactory(func() control.WindowControl {
		w := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             "logs",
			Title:            config.DisplayName() + " — Logs",
			Width:            960,
			Height:           640,
			Hidden:           true,
			BackgroundColour: application.NewRGB(16, 20, 28),
			URL:              "/logs.html",
		})
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if shuttingDown.Load() {
				return
			}
			e.Cancel()
			w.Hide()
		})
		runtime.logEmitter.bind(w)
		return &wailsWindowControl{win: w}
	})
	controlSvc.SetConsoleReader(runtime.consoleReader)

	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if shuttingDown.Load() {
			return
		}
		shuttingDown.Store(true)
		if !lifecycleRunning.Load() {
			go wailsApp.Quit()
			return
		}
		e.Cancel()
		go func() {
			runtime.bus.Publish(ritual.StopRequested{})
			waitTerminal(runtime.bus, 20*time.Second)
			wailsApp.Quit()
		}()
	})

	ctx := wailsApp.Context()
	// Track lifecycle.Running so the close hook can skip the 20s drain when
	// nothing is in flight. Subscribed before lifecycle.Attach below so we see
	// every transition; the initial Idle from Attach leaves the flag false.
	go func() {
		ch, unsub := runtime.bus.Subscribe()
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if sc, ok := evt.(lifecycle.StatusChanged); ok {
					lifecycleRunning.Store(sc.Status == lifecycle.Running)
					if sc.Status == lifecycle.Done || sc.Status == lifecycle.Failed {
						// Upload/Download/Session/etc. all resolve here exactly
						// once per flow — the zstd worker pools created on
						// demand during the flow have no reason to survive
						// past it (see CompressingStorage.Release).
						runtime.localCompressed.Release()
						runtime.remoteCompressed.Release()
					}
				}
			}
		}
	}()
	// OS notifications (design-log/047): project the critical run transitions
	// (server up / clean stop / failure) onto native toasts. Pure bus consumer
	// behind the notify.Notifier port — wailsNotifier is the only platform seam.
	stopNotify := notify.Attach(ctx, runtime.bus, &wailsNotifier{svc: notifSvc})
	defer stopNotify()
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
	// Three flows from shared stage nodes (design-log/031, /038, /045).
	// Download/Upload/Restore/Revert/RetentionApply ignore d.Drainable (no
	// Running, no livesync), so passing the session deps verbatim is harmless.
	entries := lifecycle.Entries{
		Session:        pipeline.Build(pipelineDeps),
		LocalSession:   pipeline.BuildLocalSession(pipelineDeps),
		Download:       pipeline.BuildDownload(pipelineDeps),
		Upload:         pipeline.BuildUpload(pipelineDeps),
		Restore:        pipeline.BuildRestore(pipelineDeps),
		Revert:         pipeline.BuildRevert(pipelineDeps),
		RetentionApply: pipeline.BuildRetentionApply(pipelineDeps),
	}

	sessionHook := func(rs *ritual.RunState) {
		dispatcher.SetTarget(func(id domain.RefID) { rs.RefID = id })
	}
	stopLifecycle := lifecycle.Attach(ctx, runtime.bus, entries, sessionHook)
	defer stopLifecycle()
	// loadedref keeps settings.LoadedRefID in sync with what the workdir reflects
	// (design-log/044) by subscribing to pulling.HeadResolvedInfo +
	// committing.CommittedInfo. Reads + saves are best-effort — a stale field
	// only ever degrades the "current" badge to a fallback.
	stopLoadedRef := loadedref.Attach(ctx, runtime.bus,
		domain.LoadSettings,
		func(s *domain.Settings) error { return s.Save() },
	)
	defer stopLoadedRef()
	defer runtime.stopLogFile()
	go runtime.projection.Run(ctx)
	go runtime.logsink.Run(ctx)
	go runtime.logEmitter.loop(ctx) // idle-quiescent console batching (006/042)
	go runtime.ticker.Run(ctx)
	// Arm the ticker's stall-heartbeat for the wire-transfer windows only, so a
	// quiet R2 PutStream still pulses liveness. Subscribes on New (before Run)
	// to avoid missing the first StateChanged. Design-log/022 #2.
	go transferwatch.New(runtime.bus, runtime.ticker).Run(ctx)

	// Autoupdate (design-log/037). relaunch publishes UpdateRestartInfo, then
	// re-execs the (already-swapped) binary and tears the window down via Quit
	// — Wails v3 has no restart API. The updater reads the bin/<os-arch>/
	// listing through the observed remoteStorage, so its List/GetStream land in
	// the single log; the observed.Updater publishes the Update* dial stream.
	relaunch := func() error {
		runtime.bus.Publish(observed.UpdateRestartInfo{})
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("relaunch: resolve executable: %w", err)
		}
		cmd := exec.Command(exe) // #nosec G204 -- exe is os.Executable(), not user input
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("relaunch: start: %w", err)
		}
		wailsApp.Quit()
		return nil
	}
	updater := observed.NewUpdater(
		selfupdate.New(runtime.remoteStorage, config.AppVersion, goruntime.GOOS, goruntime.GOARCH, relaunch),
		config.AppVersion, runtime.bus,
	)
	// Pre-IDLE Preflight on launch + the same flow on every manual re-check
	// (selfupdate.CheckRequested from control). Runs off the Wails thread so it
	// never blocks Run(); failures publish UpdateFailed → PhaseFailed → usable IDLE.
	go runUpdateFlow(ctx, updater)
	go watchUpdateRequests(ctx, runtime.bus, updater)

	if err := wailsApp.Run(); err != nil {
		log.Println(err)
	}
}

// runUpdateFlow runs one Preflight pass: Check, and if the running binary is
// outdated, Apply (which on success replaces the process and never returns).
// Every transition is published by the observed.Updater, so this orchestrator
// holds no UI logic — the projection folds the events into the gray dial and a
// failure drops to a usable IDLE (design-log/037 §Q4/Q5).
func runUpdateFlow(ctx context.Context, updater ports.UpdaterService) {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	up, outdated, err := updater.Check(checkCtx)
	if err != nil || !outdated {
		return // events already published: timed-out/up-to-date → IDLE, real error → PhaseFailed
	}
	_ = updater.Apply(ctx, up) // success replaces the process; failure → UpdateFailed
}

// watchUpdateRequests re-runs the Preflight flow whenever the user taps
// Advanced ▸ Check for update (control publishes selfupdate.CheckRequested) —
// one code path with launch, one gray-dial takeover (design-log/037 §Q6).
func watchUpdateRequests(ctx context.Context, bus ports.EventBus, updater ports.UpdaterService) {
	ch, unsub := bus.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if _, ok := evt.(selfupdate.CheckRequested); ok {
				go runUpdateFlow(ctx, updater)
			}
		}
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
	logEmitter    *batchingLogEmitter
	ticker        *progress.Ticker
	stopLogFile   func()
	committer     ports.Committer
	pusher        ports.Pusher
	commitTargets []string
	syncProber    control.SyncProber
	dirtyProber   control.LocalDirtyProber
	versionLister control.VersionLister                   // historical-ref listing for restore (design-log/038)
	remoteStorage ports.StorageRepository                 // for the autoupdate feed (design-log/037)
	consoleReader func(context.Context) ([]string, error) // on-demand latest.log backfill (design-log/043)
	// design-log/045 §A — per-version delete + loaded-id readers/writers.
	// remoteVersionDeleter parallels localVersionDeleter for the 045 post-ship
	// remote-delete extension (user direction 2026-06-05).
	localVersionDeleter  control.LocalDeleter
	remoteVersionDeleter control.RemoteDeleter
	loadedRefIDFn        control.LoadedIDFn
	clearLoadedRefID     control.SettingsClearer
	// design-log/045 §E — local on-disk stats walker.
	localStatsFn control.StorageStatFn
	// localCompressed/remoteCompressed back onto localStorage/remoteStorage's
	// zstd encoder/decoder pools. Released once a flow's StatusChanged reaches
	// Done/Failed — workers are created lazily on next use, so there's no
	// reason for their 8MB buffers to survive between syncs.
	localCompressed  *adapters.CompressingStorage
	remoteCompressed *adapters.CompressingStorage
}

func buildRuntime() (*guiRuntime, error) { //nolint:gocyclo // composition root — high fanout is structural, not logical
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
	// Materialise settings.json on first boot if it's missing (user direction
	// 2026-06-05). LoadSettings collapses a missing file to in-memory defaults
	// but never writes it, so a fresh install had no file for the user to
	// inspect/edit and the Retention section read pure defaults. Persist those
	// defaults once, only when absent (never clobber a real file). The root dir
	// already exists (sandbox anchor, design-log/040 §Q1) so this writes a
	// content file, not an empty folder — consistent with the lazy-dir rule.
	if _, statErr := os.Stat(domain.SettingsPath()); os.IsNotExist(statErr) {
		if err := settings.Save(); err != nil {
			return nil, fmt.Errorf("init settings file: %w", err)
		}
	}

	// Remote backend selected by ResolveMode: RITUAL_REMOTE_MODE env wins,
	// else the bakedRemoteMode ldflag (gui:build:dev:local — design-log/048),
	// else default ModeR2 (design-log/030). ModeR2 reads credentials from
	// RITUAL_R2_* — typically loaded by config.LoadEnvFiles from
	// .env.{RITUAL_ENV}.local at startup. Set RITUAL_REMOTE_MODE=mock to opt
	// into the local-FS dev backend without a rebuild.
	rawRemote, err := remote.Build(context.Background(), remote.ResolveMode(), bus)
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
	// makeRefReader loads refs/{id}.json → domain.Ref from a given store. Used
	// by the dirty prober (local) and the version lister (local + remote,
	// design-log/038).
	makeRefReader := func(store ports.StorageRepository) control.RefReader {
		return func(ctx context.Context, id domain.RefID) (*domain.Ref, error) {
			rc, err := store.GetStream(ctx, "refs/"+string(id)+".json")
			if err != nil {
				return nil, fmt.Errorf("read ref %s: %w", id, err)
			}
			defer func() { _ = rc.Close() }()
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
	}
	readRef := makeRefReader(localStorage)
	workdirScan := func(ctx context.Context, since time.Time, previous map[string]domain.FileEntry, targets []string) (map[string]domain.FileEntry, error) {
		sc, err := adapters.NewMtimeScanner(config.RootPath, since, previous)
		if err != nil {
			return nil, err
		}
		return sc.Scan(ctx, targets)
	}
	dirtyProber := control.NewLocalDirtyProber(localHeadResolver, readRef, workdirScan, commitTargets)

	// Version lister (design-log/038): enumerate historical refs per scope so
	// the Versions section in Advanced can offer a restore target. Remote is the
	// canonical history; a remote failure degrades to the cached local refs.
	// Loaded ref id (design-log/044): the lister flags IsLoaded so the Versions
	// "current" badge follows the workdir instead of HEAD. Read fresh per list
	// so a Restore/Publish landed between Advanced mounts is visible without
	// re-wiring; a load error / empty field falls back to IsHead inside the
	// lister.
	loadedRefIDFn := func() domain.RefID {
		s, err := domain.LoadSettings()
		if err != nil || s == nil {
			return ""
		}
		return s.LoadedRefID
	}
	versionLister := control.NewVersionLister(
		control.VersionScope{List: localStorage.List, ReadRef: readRef},
		control.VersionScope{List: remoteStorage.List, ReadRef: makeRefReader(remoteStorage)},
		loadedRefIDFn,
	)
	// Per-version delete + GC (design-log/045 §A + post-ship remote extension).
	// Composition root closures: delete refs/<id>.json then sweep orphaned
	// objects/. Uses the same refs.Collector the Retaining stage's GC job uses,
	// so the cleanup semantics are identical to a normal sync's prune — once
	// per side, since each side has its own object store.
	localCollector := refs.NewCollector(localStorage)
	localDeleter := func(ctx context.Context, id domain.RefID) error {
		key := "refs/" + string(id) + ".json"
		if err := localStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
		if err := localCollector.Collect(ctx); err != nil {
			return fmt.Errorf("collect orphans: %w", err)
		}
		return nil
	}
	// Remote-side parallel (user direction 2026-06-05: "allow me to delete
	// anything"). Same shape, distinct store. The collector sweep on remote
	// objects/ removes blobs no surviving remote ref pins; no remote lock is
	// held (v1 trade-off — cross-client coordination is deferred).
	remoteCollector := refs.NewCollector(remoteStorage)
	remoteDeleter := func(ctx context.Context, id domain.RefID) error {
		key := "refs/" + string(id) + ".json"
		if err := remoteStorage.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
		if err := remoteCollector.Collect(ctx); err != nil {
			return fmt.Errorf("collect orphans: %w", err)
		}
		return nil
	}
	clearLoadedRefID := func() error {
		s, err := domain.LoadSettings()
		if err != nil || s == nil {
			return err
		}
		s.LoadedRefID = ""
		return s.Save()
	}
	// Local on-disk stats (design-log/045 §E). Walks the local FS sandbox
	// directly to sum file sizes under a prefix (objects/). Stays inside the
	// existing os.Root so the read can't escape the work root.
	localStatsFn := func(_ context.Context, prefix string) (int64, int, error) {
		return walkLocalPrefix(workRoot, prefix)
	}

	// Wire pull/push plan callbacks into the bus so the projection can
	// populate ViewModel.BytesTotal before the first progress.Tick lands —
	// audit open item #1: without this the bar reads 0%% the whole transfer
	// even though BytesDone climbs every second.
	puller.OnPlan(func(p ritual.PlanInfo) { bus.Publish(p) })
	pusher.OnPlan(func(p ritual.PlanInfo) { bus.Publish(p) })

	// Real NeoForge launcher: settings.StartScript (default "start.bat")
	// resolved relative to <root>/server/. Operators may override via
	// settings.json — empty/missing falls back to domain.DefaultStartScript.
	// Server sandbox is NOT created here: the os.Root is opened lazily by the
	// builder on first launch, so a fresh host carries no empty server/ until an
	// Apply has written into it (design-log/040). The only eager MkdirAll is the
	// root sandbox anchor above, which never stays empty (logs/ lands at once).
	serverPath := filepath.Join(config.RootPath, config.ServerDir)
	cmdBuilder, err := adapters.NewServerCmdBuilder(serverPath, settings.StartScript, settings.ToServerRuntime)
	if err != nil {
		return nil, fmt.Errorf("server cmd builder: %w", err)
	}

	// On-demand console backfill (design-log/043): read the running server's own
	// <cwd>/logs/latest.log tail when the logs window opens. cwd mirrors the
	// cmd builder (filepath.Dir of the start script), so this stays correct for
	// instance-subfolder start scripts.
	consoleReader := newConsoleReader(serverPath, settings.StartScript)

	readiness := adapters.NewTCPReadinessCheck(fmt.Sprintf("127.0.0.1:%d", settings.Port), bus)

	localRets, remoteRets := retention.Build(localStorage, remoteStorage, bus)

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
	logEmitter := newBatchingLogEmitter()

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

	// Sweep the leftover ".old" backup a prior in-place self-update left beside
	// us: minio/selfupdate can't delete the running binary on Windows, so its
	// sidecar lingers until the next launch (design-log/037). Best-effort, and
	// logged via the bus now that the file sink is live.
	if exe, exeErr := os.Executable(); exeErr == nil {
		if removed, rmErr := selfupdate.CleanupBackup(exe); rmErr != nil {
			bus.Publish(observed.UpdateCleanupInfo{Path: selfupdate.BackupPath(exe), Err: rmErr})
		} else if removed {
			bus.Publish(observed.UpdateCleanupInfo{Path: selfupdate.BackupPath(exe), Removed: true})
		}
	}

	return &guiRuntime{
		bus:                  bus,
		pipelineDeps:         pipelineDeps,
		projection:           proj,
		logsink:              sink,
		viewEmitter:          viewEmitter,
		logEmitter:           logEmitter,
		ticker:               remoteTicker,
		stopLogFile:          stopLogFile,
		committer:            committer,
		pusher:               pusher,
		commitTargets:        commitTargets,
		syncProber:           syncProber,
		dirtyProber:          dirtyProber,
		versionLister:        versionLister,
		remoteStorage:        remoteStorage,
		consoleReader:        consoleReader,
		localVersionDeleter:  localDeleter,
		remoteVersionDeleter: remoteDeleter,
		loadedRefIDFn:        loadedRefIDFn,
		clearLoadedRefID:     clearLoadedRefID,
		localStatsFn:         localStatsFn,
		localCompressed:      localCompressed,
		remoteCompressed:     remoteCompressed,
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

// batchingLogEmitter window-scopes the MC console stream to the logs window and
// coalesces it into one IPC per ~16ms (design-log/006 mechanism applied to the
// narrow 042 stream). It is idle-quiescent: the loop parks on `wake` with no
// ticker, so an idle server costs zero wakeups ("no DoS when there are no
// requests"). On overflow it drops oldest and reports the count in the next
// batch. logsWindow.EmitEvent does not broadcast — the main window never
// receives console traffic.
type batchingLogEmitter struct {
	out     atomic.Pointer[func(logsink.ServerLogBatch)] // window emit, swapped in on bind; tests inject a recorder
	mu      sync.Mutex
	ring    []logsink.ServerLog // FIFO, cap = cfg.Capacity
	dropped int
	wake    chan struct{} // buffered cap 1; coalesces nudges
	cfg     batchCfg
}

type batchCfg struct {
	Capacity int           // ring size — drop oldest beyond this
	BatchMax int           // size trigger — flush immediately at this many lines
	Interval time.Duration // coalescing window after the first line in an empty ring
}

func newBatchingLogEmitter() *batchingLogEmitter {
	return &batchingLogEmitter{
		wake: make(chan struct{}, 1),
		cfg:  batchCfg{Capacity: 1024, BatchMax: 128, Interval: 16 * time.Millisecond},
	}
}

func (e *batchingLogEmitter) bind(w *application.WebviewWindow) {
	fn := func(b logsink.ServerLogBatch) { w.EmitEvent("server:logs", b) }
	e.out.Store(&fn)
}

// Emit appends a line and nudges the loop only when the ring transitions
// empty→non-empty (arm the deadline) or crosses BatchMax (preempt the timer).
// Within an active coalescing window, a normal append nudges nothing — the
// running deadline drains it. No Emit ⇒ no wake ⇒ the loop stays parked.
func (e *batchingLogEmitter) Emit(line logsink.ServerLog) {
	e.mu.Lock()
	wasEmpty := len(e.ring) == 0
	if len(e.ring) == e.cfg.Capacity {
		e.ring = e.ring[1:]
		e.dropped++
	}
	e.ring = append(e.ring, line)
	sizeTrigger := len(e.ring) >= e.cfg.BatchMax
	e.mu.Unlock()
	if wasEmpty || sizeTrigger {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

// loop coalesces lines into one EmitEvent per Interval (or per BatchMax),
// parking on `wake` at idle. Lazy timer — no ticker fires when nothing is
// pending (design-log/006 §Design, 042 §Q5).
func (e *batchingLogEmitter) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		}
		timer := time.NewTimer(e.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-e.wake: // size trigger — flush now
			timer.Stop()
		case <-timer.C:
		}
		e.flush()
	}
}

func (e *batchingLogEmitter) flush() {
	e.mu.Lock()
	if len(e.ring) == 0 && e.dropped == 0 {
		e.mu.Unlock()
		return
	}
	n := len(e.ring)
	if n > e.cfg.BatchMax {
		n = e.cfg.BatchMax
	}
	lines := append([]logsink.ServerLog(nil), e.ring[:n]...)
	e.ring = e.ring[n:]
	dropped := e.dropped
	e.dropped = 0
	leftover := len(e.ring) > 0
	e.mu.Unlock()

	if out := e.out.Load(); out != nil {
		(*out)(logsink.ServerLogBatch{Lines: lines, Dropped: dropped})
	}
	// BatchMax capped this flush — re-arm so the leftover doesn't wait idle.
	if leftover {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

// walkLocalPrefix sums file sizes + counts under a top-level directory inside
// the work-root sandbox (design-log/045 §E). Used by the local on-disk stats
// for the Versions Local-tab header. Honours os.Root so the walk can't escape
// the sandbox. Shallow Readdir is enough: objects/ is a flat keyspace (one
// file per hash, no subdirs). A missing prefix (fresh install with no blobs
// yet) reads as (0, 0, nil) — the header simply renders 0 B on disk.
func walkLocalPrefix(root *os.Root, prefix string) (int64, int, error) {
	dir, err := root.Open(prefix)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("open %s: %w", prefix, err)
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.Readdir(0)
	if err != nil {
		return 0, 0, fmt.Errorf("readdir %s: %w", prefix, err)
	}
	var bytes int64
	var count int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		bytes += e.Size()
		count++
	}
	return bytes, count, nil
}

// wailsWindowControl adapts a Wails WebviewWindow to services.WindowControl.
type wailsWindowControl struct{ win *application.WebviewWindow }

func (c *wailsWindowControl) Show()  { c.win.Show() }
func (c *wailsWindowControl) Focus() { c.win.Focus() }
