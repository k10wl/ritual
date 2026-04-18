package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"ritual/internal/subsystems/conditions"
	"ritual/internal/subsystems/heartbeat"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/prompt"
	"ritual/internal/subsystems/retention"
	synckit "ritual/internal/subsystems/sync"
)

var (
	envAccountID       string
	envAccessKeyID     string
	envSecretAccessKey string
	envBucket          string
)

func main() {
	if services.HandleUpdateProcess() {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	success := false
	defer func() {
		if !success {
			fmt.Println("\nPress Enter to exit...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		}
	}()
	if err := run(ctx); err != nil {
		fmt.Printf("Ritual failed: %v\n", err)
		return
	}
	success = true
}

func run(ctx context.Context) error {
	// --- Environment validation ---
	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		return fmt.Errorf("build error: R2 credentials not injected")
	}

	// --- Infrastructure setup (filesystem, logging, event bus) ---
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer workRoot.Close()

	logFile, logCleanup, err := logging.CreateLogFile(workRoot)
	if err != nil {
		fmt.Printf("Warning: log file: %v\n", err)
	}
	if logCleanup != nil {
		defer logCleanup()
	}

	bus := adapters.NewEventBus(128)
	stopLog := logging.Attach(bus, logFile)
	defer stopLog()

	prompter := prompt.NewStdin(os.Stdin, os.Stdout)

	// --- Storage and manifest adapters ---
	// Adapters are wrapped with observed.NewStorage so every Get/Put/Copy/
	// Rename/Delete/DeleteBatch/List publishes a Storage*Info event on the bus.
	rawLocal, err := adapters.NewFSRepository(workRoot, "local")
	if err != nil {
		return fmt.Errorf("local storage: %w", err)
	}
	rawRemote, err := adapters.NewR2Repository(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, bus)
	if err != nil {
		return fmt.Errorf("remote storage: %w", err)
	}
	rawRemote = rawRemote.WithPrefix(envBucket)
	localStorage := observed.NewStorage(rawLocal, bus)
	remoteStorage := observed.NewStorage(rawRemote, bus)
	localManifests := adapters.NewManifestStore(localStorage)
	remoteManifests := adapters.NewManifestStore(remoteStorage)

	remoteManifest, err := remoteManifests.Get(context.Background())
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	// --- Subsystem builders (CLI-specific wiring) ---
	sk, err := synckit.Build(workRoot, localStorage, remoteStorage, localManifests, remoteManifests, bus)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	// --- Heartbeat (needs WorldSync from kit) ---
	_, stopHeartbeat := heartbeat.Attach(bus, localManifests, remoteManifests, sk.WorldSync)
	defer stopHeartbeat()
	sysInfo := adapters.NewSystemInfo()
	javaInfo := adapters.NewJavaInfo()
	conds, err := conditions.Build(remoteManifest, remoteManifests, sysInfo, javaInfo)
	if err != nil {
		return fmt.Errorf("conditions: %w", err)
	}
	rets, err := retention.Build(localStorage, remoteStorage, bus, remoteManifest)
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	settings, err := services.PromptSettings(bus, prompter, remoteManifest.GetMinRAMMB())
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	cmdBuilder, err := adapters.NewServerCmdBuilder(workRoot, remoteManifest.Server.StartScript, settings.ToServerRuntime)
	if err != nil {
		return fmt.Errorf("cmd builder: %w", err)
	}
	readiness := adapters.NewTCPReadinessCheck(fmt.Sprintf("localhost:%d", settings.Port))

	// --- Ritual: build once, listen for commands ---
	r := app.New(
		bus,
		localStorage, remoteStorage,
		localManifests, remoteManifests,
		conds, sk.Updaters, sk.ExitUpdaters, rets,
		sk.WorldScanner,
		cmdBuilder,
		readiness,
	)

	// Wait for terminal status via bus
	done := make(chan error, 1)
	ch, unsub := bus.Subscribe()
	go func() {
		defer unsub()
		for event := range ch {
			if sc, ok := event.(app.StatusChanged); ok {
				switch sc.Status {
				case app.Done:
					done <- nil
					return
				case app.Failed:
					done <- sc.Err
					return
				}
			}
		}
	}()

	go r.Listen(ctx)
	bus.Publish(app.StartRequested{})
	return <-done
}
