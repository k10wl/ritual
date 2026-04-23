// Package main is the CLI entry point.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
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
	"syscall"

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
			_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
		}
	}()
	if err := run(ctx); err != nil {
		fmt.Printf("Ritual failed: %v\n", err)
		return
	}
	success = true
}

//nolint:gocyclo // composition root — sequential wiring, intentionally linear.
func run(ctx context.Context) error {
	// --- Environment validation ---
	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		return errors.New("build error: R2 credentials not injected")
	}

	// --- Infrastructure setup (filesystem, logging, event bus) ---
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer func() { _ = workRoot.Close() }()

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
	rawRemote, err := adapters.NewR2Repository(ctx, envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, bus)
	if err != nil {
		return fmt.Errorf("remote storage: %w", err)
	}
	rawRemote = rawRemote.WithPrefix(envBucket)
	localStorage := observed.NewStorage(rawLocal, bus)
	remoteStorage := observed.NewStorage(rawRemote, bus)
	localManifests := adapters.NewManifestStore(localStorage)
	remoteManifests := adapters.NewManifestStore(remoteStorage)

	remoteManifest, err := remoteManifests.Get(ctx)
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	// --- Subsystem builders (CLI-specific wiring) ---
	sk, err := synckit.Build(ctx, workRoot, localStorage, remoteStorage, localManifests, remoteManifests, bus)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	// --- Heartbeat (needs WorldSync from kit) ---
	_, stopHeartbeat := heartbeat.Attach(bus, localManifests, remoteManifests, sk.WorldSync) //nolint:contextcheck // supervisor owns its own lifecycle via bus events

	defer stopHeartbeat()
	sysInfo := adapters.NewSystemInfo()
	javaInfo := adapters.NewJavaInfo()
	rets, err := retention.Build(localStorage, remoteStorage, bus, remoteManifest)
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	settings, err := services.PromptSettings(ctx, bus, prompter)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	preflightChecks := conditions.Build(settings, sysInfo, javaInfo, bus)
	cmdBuilder, err := adapters.NewServerCmdBuilder(workRoot, remoteManifest.Server.StartScript, settings.ToServerRuntime)
	if err != nil {
		return fmt.Errorf("cmd builder: %w", err)
	}
	readiness := adapters.NewTCPReadinessCheck(fmt.Sprintf("127.0.0.1:%d", settings.Port), bus)

	// --- Ritual: build once, listen for commands ---
	r := app.New(
		bus,
		localStorage, remoteStorage,
		localManifests, remoteManifests,
		preflightChecks, sk.Updaters, sk.ExitUpdaters, rets,
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
				case app.Idle, app.Running:
					// transient states — keep waiting
				}
			}
		}
	}()

	r.Listen(ctx)
	bus.Publish(app.StartRequested{})
	return <-done
}
