package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"

	"ritual/internal/adapters"
	"ritual/internal/config"
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

	// Create event channel and start consumer
	events := make(chan ports.Event, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumeEvents(events, logFile)
	}()

	// Create local storage
	localStorage, err := adapters.NewFSRepository(workRoot)
	if err != nil {
		fmt.Printf("Failed to create local storage: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create remote storage (R2) and uploader
	remoteStorage, r2Uploader, err := adapters.NewR2RepositoryWithUploader(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, events)
	if err != nil {
		fmt.Printf("Failed to create remote storage: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create librarian service
	librarian, err := services.NewLibrarianService(localStorage, remoteStorage)
	if err != nil {
		fmt.Printf("Failed to create librarian service: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create validator service
	validator, err := services.NewValidatorService()
	if err != nil {
		fmt.Printf("Failed to create validator service: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create updaters
	instanceUpdater, err := services.NewInstanceUpdater(librarian, validator, remoteStorage, envBucket, workRoot)
	if err != nil {
		fmt.Printf("Failed to create instance updater: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	worldsUpdater, err := services.NewWorldsUpdater(librarian, validator, remoteStorage, envBucket, workRoot, events)
	if err != nil {
		fmt.Printf("Failed to create worlds updater: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	updaters := []ports.UpdaterService{instanceUpdater, worldsUpdater}

	// Create backupper (R2 with local tee - single archive stream to both destinations)
	r2Backupper, err := services.NewR2Backupper(r2Uploader, envBucket, workRoot, true, nil, events)
	if err != nil {
		fmt.Printf("Failed to create R2 backupper: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	backuppers := []ports.BackupperService{r2Backupper}

	// Create retention services
	localRetention, err := services.NewLocalRetention(localStorage, events)
	if err != nil {
		fmt.Printf("Failed to create local retention: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	r2Retention, err := services.NewR2Retention(remoteStorage, events)
	if err != nil {
		fmt.Printf("Failed to create R2 retention: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	logRetention, err := services.NewLogRetention(localStorage, events)
	if err != nil {
		fmt.Printf("Failed to create log retention: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	retentions := []ports.RetentionService{localRetention, r2Retention, logRetention}

	// Fetch remote manifest to get start script path
	remoteManifest, err := librarian.GetRemoteManifest(context.Background())
	if err != nil {
		fmt.Printf("Failed to get remote manifest: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create server runner
	commandExecutor := adapters.NewCommandExecutorAdapter()
	serverRunner, err := adapters.NewServerRunner(config.RootPath, remoteManifest.StartScript, commandExecutor)
	if err != nil {
		fmt.Printf("Failed to create server runner: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Create Molfar service
	molfar, err := services.NewMolfarService(updaters, backuppers, retentions, serverRunner, librarian, events, workRoot)
	if err != nil {
		fmt.Printf("Failed to create molfar service: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Prompt for settings and create server config
	settings, err := services.PromptSettings(events)
	if err != nil {
		fmt.Printf("Failed to get settings: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	server, err := settings.ToServer()
	if err != nil {
		fmt.Printf("Failed to create server config: %v\n", err)
		close(events)
		wg.Wait()
		return
	}

	// Run lifecycle
	fmt.Println("Starting Ritual")

	if err := molfar.Prepare(); err != nil {
		fmt.Printf("Prepare phase failed: %v\n", err)
		close(events)
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
		close(events)
		wg.Wait()
		return
	}

	// Close event channel and wait for consumer to finish
	close(events)
	wg.Wait()

	if runErr != nil {
		return
	}

	fmt.Println("Ritual completed successfully")
	success = true
}
