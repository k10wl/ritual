// Package main is the CLI entry point.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/adapters/observed"
	"ritual/internal/app"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/services"
	"ritual/internal/core/stages/pulling"
	"ritual/internal/subsystems/conditions"
	"ritual/internal/subsystems/heartbeat"
	"ritual/internal/subsystems/logging"
	"ritual/internal/subsystems/prompt"
	"ritual/internal/subsystems/retention"
	"strings"
	"syscall"
)

var (
	envAccountID       string
	envAccessKeyID     string
	envSecretAccessKey string
	envBucket          string
)

func main() {
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

	// --- Refs pipeline (Pull, Apply, Commit, Push) shares one ParallelRunner
	// across the four verbs so the 10-way blob concurrency saturates uniformly.
	worldsPath := filepath.Join(config.RootPath, config.WorldsDir)
	if err := os.MkdirAll(worldsPath, config.DirPermission); err != nil {
		return fmt.Errorf("create worlds dir: %w", err)
	}
	worldsRoot, err := os.OpenRoot(worldsPath)
	if err != nil {
		return fmt.Errorf("open worlds root: %w", err)
	}
	defer func() { _ = worldsRoot.Close() }()
	workdirStorage, err := adapters.NewFSRepository(worldsRoot, "workdir")
	if err != nil {
		return fmt.Errorf("workdir storage: %w", err)
	}
	scanner := adapters.NewFullScanner(os.DirFS(worldsPath))
	const refsConcurrency = 10
	runner := adapters.NewParallelRunner(refsConcurrency)
	puller := refs.NewPuller(remoteStorage, localStorage, runner)
	applier := refs.NewApplier(localStorage, workdirStorage, scanner, runner)
	committer := refs.NewCommitter(scanner, workdirStorage, localStorage, runner)
	pusher := refs.NewPusher(localStorage, remoteStorage, runner)
	commitTargets := []string{"**"}
	headResolver := newRemoteHeadResolver(remoteStorage)

	// --- Heartbeat (attached after app.New so the supervisor shares
	// the same Locker as the state machine) ---
	var stopHeartbeat func()
	defer func() {
		if stopHeartbeat != nil {
			stopHeartbeat()
		}
	}()
	sysInfo := adapters.NewSystemInfo()
	javaInfo := adapters.NewJavaInfo()
	localRets, remoteRets, err := retention.Build(localStorage, remoteStorage, bus)
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	settings, err := services.PromptSettings(ctx, bus, prompter)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	preflightChecks := conditions.Build(settings, sysInfo, javaInfo, bus)
	// TODO(v2.1): start-script path should come from settings.StartScript once that field exists.
	cmdBuilder, err := adapters.NewServerCmdBuilder(workRoot, "start.bat", settings.ToServerRuntime)
	if err != nil {
		return fmt.Errorf("cmd builder: %w", err)
	}
	readiness := adapters.NewTCPReadinessCheck(fmt.Sprintf("127.0.0.1:%d", settings.Port), bus)

	// --- Ritual: build once, listen for commands ---
	r := app.New(
		bus,
		localStorage, remoteStorage,
		preflightChecks, puller, applier, headResolver, committer, pusher, commitTargets, localRets, remoteRets,
		cmdBuilder,
		readiness,
	)
	_, stopHeartbeat = heartbeat.Attach(bus, r.Heartbeat) //nolint:contextcheck // supervisor owns its own lifecycle via bus events

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

func newRemoteHeadResolver(remote ports.StorageRepository) pulling.HeadResolver {
	return func(ctx context.Context) (domain.RefID, error) {
		keys, err := remote.List(ctx, "refs/")
		if err != nil {
			return "", fmt.Errorf("list refs: %w", err)
		}
		var head string
		for _, key := range keys {
			name := strings.TrimPrefix(key, "refs/")
			name = strings.TrimSuffix(name, ".json")
			if name == "" {
				continue
			}
			if name > head {
				head = name
			}
		}
		if head == "" {
			return "", errors.New("no refs on remote")
		}
		return domain.RefID(head), nil
	}
}
