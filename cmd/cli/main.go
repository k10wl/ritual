package main

//go:generate goversioninfo

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// Injected at build time via ldflags
var (
	envAccountID       string
	envAccessKeyID     string
	envSecretAccessKey string
	envBucket          string
)

func main() {
	// Handle update process flags (--replace-old, --cleanup-update)
	if services.HandleUpdateProcess() {
		return
	}

	success := false
	defer func() {
		if !success {
			fmt.Println("\nPress Enter to exit...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		}
	}()

	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		fmt.Println("Build error: R2 credentials not injected")
		return
	}

	// Ensure root directory exists
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		fmt.Printf("Failed to create root directory: %v\n", err)
		return
	}

	// Open work root
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		fmt.Printf("Failed to open work root: %v\n", err)
		return
	}
	defer workRoot.Close()

	// Create log file
	logFile, logCleanup, err := createLogFile(workRoot)
	if err != nil {
		fmt.Printf("Warning: failed to create log file: %v\n", err)
		// Continue without logging to file
	}
	if logCleanup != nil {
		defer logCleanup()
	}

	// Create event bus + stdin prompter; start consumer subscription.
	bus := adapters.NewEventBus(128)
	prompter := newStdinPrompter(os.Stdin, os.Stdout)
	busCh, cancelSub := bus.Subscribe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumeEvents(busCh, logFile)
	}()

	// Backwards-compat alias for callers still taking an events bus parameter.
	events := bus

	// shutdown waits for the consumer to drain before returning.
	shutdown := func() {
		cancelSub()
		wg.Wait()
	}
	_ = prompter // wired into services during state-machine cutover
	_ = shutdown // used by error paths below

	// Create local storage
	localStorage, err := adapters.NewFSRepository(workRoot)
	if err != nil {
		fmt.Printf("Failed to create local storage: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create remote storage (R2)
	remoteStorage, err := adapters.NewR2Repository(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, events)
	if err != nil {
		fmt.Printf("Failed to create remote storage: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create librarian service (still used by Molfar; dies in state-machine Phase 6)
	librarian, err := services.NewLibrarianService(localStorage, remoteStorage)
	if err != nil {
		fmt.Printf("Failed to create librarian service: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// ManifestStore, two sides. Ready for state-machine Deps consumption.
	localManifests := adapters.NewManifestStore(localStorage)
	remoteManifests := adapters.NewManifestStore(remoteStorage)
	_ = localManifests  // wired in state-machine sprint
	_ = remoteManifests // wired in state-machine sprint

	// Create updaters (ritual updater first - must self-update before anything else)
	ritualUpdater, err := services.NewRitualUpdater(librarian, remoteStorage, config.AppVersion)
	if err != nil {
		fmt.Printf("Failed to create ritual updater: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Generate sync session ID (same format as lock ID for traceability)
	hostname, _ := os.Hostname()
	syncSessionID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())

	// Staging bases
	localStagingBase := filepath.Join(config.TempRitualPath(), fmt.Sprintf(config.SyncStagingPattern, time.Now().UnixNano()))
	remoteStagingBase := "sync/" + syncSessionID

	// Scanners — use fs.Sub for scoped FS per target
	worldsPath := filepath.Join(config.RootPath, config.WorldsDir)
	serverPath := filepath.Join(config.RootPath, config.ServerDir)

	// Ensure directories exist
	os.MkdirAll(worldsPath, config.DirPermission)
	os.MkdirAll(serverPath, config.DirPermission)

	worldsFS, _ := fs.Sub(workRoot.FS(), config.WorldsDir)
	serverFS, _ := fs.Sub(workRoot.FS(), config.ServerDir)

	// Parse .ritualsync filters
	worldsFilter, err := adapters.ParseRitualSync(worldsFS)
	if err != nil {
		fmt.Printf("Failed to parse worlds .ritualsync: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}
	serverFilter, err := adapters.ParseRitualSync(serverFS)
	if err != nil {
		fmt.Printf("Failed to parse server .ritualsync: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// World scanner: MtimeScanner if previous hash map exists, FullScanner otherwise
	var worldInnerScanner ports.DirectoryScanner
	localManifestForScanner, scannerManifestErr := librarian.GetLocalManifest(context.Background())
	if scannerManifestErr == nil && len(localManifestForScanner.Worlds.XXHashMap) > 0 {
		mtimeScanner, err := adapters.NewMtimeScanner(worldsPath, localManifestForScanner.Worlds.XXHashSyncAt, localManifestForScanner.Worlds.XXHashMap)
		if err != nil {
			fmt.Printf("Failed to create mtime scanner, falling back to full: %v\n", err)
			worldInnerScanner = adapters.NewFullScanner(worldsFS)
		} else {
			worldInnerScanner = mtimeScanner
		}
	} else {
		worldInnerScanner = adapters.NewFullScanner(worldsFS)
	}
	worldScanner := adapters.NewFilteredScanner(worldInnerScanner, worldsFilter)

	// Server scanner: always FullScanner (small file count)
	serverScanner := adapters.NewFilteredScanner(adapters.NewFullScanner(serverFS), serverFilter)

	// Two sync services — same code, different config.
	// remoteStorage already retries on transient errors (inline in R2Repository).
	worldSync := services.NewSyncService(
		worldScanner, localStorage, remoteStorage, events,
		services.SyncConfig{Prefix: config.WorldsDir, LocalDir: worldsPath},
		filepath.Join(localStagingBase, config.WorldsDir),
		remoteStagingBase+"/"+config.WorldsDir,
	)
	serverSync := services.NewSyncService(
		serverScanner, localStorage, remoteStorage, events,
		services.SyncConfig{Prefix: config.ServerDir, LocalDir: serverPath},
		filepath.Join(localStagingBase, config.ServerDir),
		remoteStagingBase+"/"+config.ServerDir,
	)

	// Updaters
	worldSyncDownloader := services.NewSyncDownloadUpdater(worldSync, librarian, func(m *domain.Manifest) *domain.SyncState {
		return &m.Worlds.SyncState
	})
	serverSyncDownloader := services.NewSyncDownloadUpdater(serverSync, librarian, func(m *domain.Manifest) *domain.SyncState {
		return &m.Server.SyncState
	})
	updaters := []ports.UpdaterService{ritualUpdater, serverSyncDownloader, worldSyncDownloader}

	// Create conditions (pre-flight checks before updaters run)
	// Fetch remote manifest to get thresholds for conditions
	remoteManifestForConditions, err := librarian.GetRemoteManifest(context.Background())
	if err != nil {
		fmt.Printf("Failed to get remote manifest for conditions: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create system info adapter for RAM and disk space checks
	systemInfo := adapters.NewWindowsSystemInfo()

	// Create Java info adapter for Java version check
	javaInfo := adapters.NewJavaInfo()

	// Create manifest lock condition
	lockCondition, err := services.NewManifestLockCondition(librarian)
	if err != nil {
		fmt.Printf("Failed to create lock condition: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create RAM condition
	ramCondition, err := services.NewRAMCondition(remoteManifestForConditions.GetMinRAMMB(), systemInfo)
	if err != nil {
		fmt.Printf("Failed to create RAM condition: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create disk space condition
	diskCondition, err := services.NewDiskSpaceCondition(remoteManifestForConditions.GetMinDiskMB(), config.RootPath, systemInfo)
	if err != nil {
		fmt.Printf("Failed to create disk condition: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create Java version condition
	javaCondition, err := services.NewJavaVersionCondition(remoteManifestForConditions.GetMinJavaVersion(), javaInfo)
	if err != nil {
		fmt.Printf("Failed to create Java condition: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	conditions := []ports.ConditionService{lockCondition, ramCondition, diskCondition, javaCondition}

	// Parse strategy chain (v2 dir + v1 tar compatibility)
	parseBackupTimestamp := services.ChainStrategies(
		services.ParseTimestampDir,
		services.ParseTimestampTar,
	)

	// Load host settings for local retention rules
	retentionSettings, err := domain.LoadSettings()
	if err != nil {
		fmt.Printf("Failed to load settings: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Apply defaults if zero
	localRules := retentionSettings.LocalRetention
	if localRules == (domain.RetentionRules{}) {
		localRules = domain.DefaultRetentionRules()
	}
	remoteRules := remoteManifestForConditions.RemoteRetention
	if remoteRules == (domain.RetentionRules{}) {
		remoteRules = domain.DefaultRetentionRules()
	}

	// Generic retentions (local + R2) + log retention (unchanged)
	localRetention, err := services.NewRetention(localStorage, localRules, config.BackupsDir, parseBackupTimestamp)
	if err != nil {
		fmt.Printf("Failed to create local retention: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}
	r2Retention, err := services.NewRetention(remoteStorage, remoteRules, config.BackupsDir, parseBackupTimestamp)
	if err != nil {
		fmt.Printf("Failed to create R2 retention: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}
	logRetention, err := services.NewLogRetention(localStorage, events)
	if err != nil {
		fmt.Printf("Failed to create log retention: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	retentions := []ports.RetentionService{localRetention, r2Retention, logRetention}

	// Exit updaters — only worlds upload on exit
	worldSyncUploader := services.NewSyncUploader(worldSync, librarian, func(m *domain.Manifest) *domain.SyncState {
		return &m.Worlds.SyncState
	})
	exitUpdaters := []ports.UpdaterService{worldSyncUploader}

	// Create server runner
	commandExecutor := adapters.NewCommandExecutorAdapter()
	serverRunner, err := adapters.NewServerRunner(config.RootPath, workRoot, remoteManifestForConditions.Server.StartScript, commandExecutor)
	if err != nil {
		fmt.Printf("Failed to create server runner: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Create Molfar service
	molfar, err := services.NewMolfarService(conditions, updaters, exitUpdaters, retentions, serverRunner, librarian, localStorage, remoteStorage, events, workRoot)
	if err != nil {
		fmt.Printf("Failed to create molfar service: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Prompt for settings and create server config
	// Pass min RAM from manifest so user can't enter less than required
	settings, err := services.PromptSettings(bus, prompter, remoteManifestForConditions.GetMinRAMMB())
	if err != nil {
		fmt.Printf("Failed to get settings: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	server, err := settings.ToServerRuntime()
	if err != nil {
		fmt.Printf("Failed to create server config: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Run lifecycle
	fmt.Println("Starting Ritual")

	if err := molfar.Prepare(); err != nil {
		fmt.Printf("Prepare phase failed: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	runErr := molfar.Run(server)
	if runErr != nil {
		fmt.Printf("Run phase failed: %v\n", runErr)
	}

	// Always attempt Exit to unlock manifests, even if Run failed
	if err := molfar.Exit(); err != nil {
		fmt.Printf("Exit phase failed: %v\n", err)
		shutdown()
		wg.Wait()
		return
	}

	// Close event channel and wait for consumer to finish
	shutdown()
	wg.Wait()

	if runErr != nil {
		return
	}

	fmt.Println("Ritual completed successfully")
	success = true
}
