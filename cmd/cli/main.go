package main

//go:generate goversioninfo

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/ritual"
	"ritual/internal/core/services"
	"ritual/internal/core/stages/acquiring"
	"ritual/internal/core/stages/archiving"
	"ritual/internal/core/stages/checking"
	"ritual/internal/core/stages/failed"
	"ritual/internal/core/stages/fetching"
	"ritual/internal/core/stages/publishing"
	"ritual/internal/core/stages/retaining"
	"ritual/internal/core/stages/running"
	"ritual/internal/core/stages/unlocking"
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
	success := false
	defer func() {
		if !success {
			fmt.Println("\nPress Enter to exit...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		}
	}()
	if err := run(); err != nil {
		fmt.Printf("Ritual failed: %v\n", err)
		return
	}
	success = true
}

func run() error {
	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		return fmt.Errorf("build error: R2 credentials not injected")
	}
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

	localStorage, err := adapters.NewFSRepository(workRoot)
	if err != nil {
		return fmt.Errorf("local storage: %w", err)
	}
	remoteStorage, err := adapters.NewR2Repository(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, bus)
	if err != nil {
		return fmt.Errorf("remote storage: %w", err)
	}
	localManifests := adapters.NewManifestStore(localStorage)
	remoteManifests := adapters.NewManifestStore(remoteStorage)

	_, stopHeartbeat := heartbeat.Attach(bus, remoteManifests)
	defer stopHeartbeat()

	remoteManifest, err := remoteManifests.Get(context.Background())
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	sk, err := synckit.Build(workRoot, localStorage, remoteStorage, localManifests, remoteManifests, bus)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	sysInfo := adapters.NewSystemInfo()
	javaInfo := adapters.NewJavaInfo()
	conds, err := conditions.Build(remoteManifest, remoteManifests, sysInfo, sysInfo, javaInfo)
	if err != nil {
		return fmt.Errorf("conditions: %w", err)
	}
	rets, err := retention.Build(localStorage, remoteStorage, bus, remoteManifest)
	if err != nil {
		return fmt.Errorf("retention: %w", err)
	}

	executor := adapters.NewCommandExecutorAdapter()
	serverRunner, err := adapters.NewServerRunner(config.RootPath, workRoot, remoteManifest.Server.StartScript, executor)
	if err != nil {
		return fmt.Errorf("server runner: %w", err)
	}
	settings, err := services.PromptSettings(bus, prompter, remoteManifest.GetMinRAMMB())
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	server, err := settings.ToServerRuntime()
	if err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	hostname, _ := os.Hostname()
	runID := fmt.Sprintf("%s%s%d", hostname, config.LockIDSeparator, time.Now().UnixNano())
	rs := &ritual.RunState{RunID: runID, Bus: bus}

	failCheck := failed.New(ritual.StageChecking)
	failFetch := failed.New(ritual.StageFetching)
	failAcq := failed.New(ritual.StageAcquiring)
	failRet := failed.New(ritual.StageRetaining)

	retain := retaining.New(rets, failRet)
	unlock := unlocking.New(localManifests, remoteManifests, retain)
	archive := archiving.New(localStorage, remoteStorage, localManifests, unlock)
	publish := publishing.New(sk.ExitUpdaters, archive)
	run := running.New(server, serverRunner, publish)
	rollback := unlocking.New(localManifests, remoteManifests, failAcq)
	acquire := acquiring.New(localManifests, remoteManifests, run, failAcq, rollback)
	fetch := fetching.New(sk.Updaters, acquire, failFetch)
	check := checking.New(conds, fetch, failCheck)

	fmt.Println("Starting Ritual")
	return ritual.Run(context.Background(), rs, check)
}
