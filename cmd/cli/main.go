package main

import (
	"bufio"
	"fmt"
	"os"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

func waitForEnter() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// Injected at build time via ldflags
var (
	envAccountID       string
	envAccessKeyID     string
	envSecretAccessKey string
	envBucket          string
)

func main() {
	defer waitForEnter()

	if envAccountID == "" || envAccessKeyID == "" || envSecretAccessKey == "" || envBucket == "" {
		fmt.Println("Build error: R2 credentials not injected")
		return
	}

	logger := adapters.NewSlogLogger()

	// Ensure root directory exists
	if err := os.MkdirAll(config.RootPath, config.DirPermission); err != nil {
		logger.Error("Failed to create root directory", "path", config.RootPath, "error", err)
		return
	}

	// Open work root
	workRoot, err := os.OpenRoot(config.RootPath)
	if err != nil {
		logger.Error("Failed to open work root", "path", config.RootPath, "error", err)
		return
	}
	defer workRoot.Close()

	// Create local storage
	localStorage, err := adapters.NewFSRepository(workRoot)
	if err != nil {
		logger.Error("Failed to create local storage", "error", err)
		return
	}

	// Create remote storage (R2) and uploader
	remoteStorage, r2Uploader, err := adapters.NewR2RepositoryWithUploader(envBucket, envAccountID, envAccessKeyID, envSecretAccessKey, logger)
	if err != nil {
		logger.Error("Failed to create remote storage", "error", err)
		return
	}

	// Create librarian service
	librarian, err := services.NewLibrarianService(localStorage, remoteStorage)
	if err != nil {
		logger.Error("Failed to create librarian service", "error", err)
		return
	}

	// Create validator service
	validator, err := services.NewValidatorService()
	if err != nil {
		logger.Error("Failed to create validator service", "error", err)
		return
	}

	// Create updaters
	instanceUpdater, err := services.NewInstanceUpdater(librarian, validator, remoteStorage, envBucket, workRoot)
	if err != nil {
		logger.Error("Failed to create instance updater", "error", err)
		return
	}

	worldsUpdater, err := services.NewWorldsUpdater(librarian, validator, remoteStorage, envBucket, workRoot, logger)
	if err != nil {
		logger.Error("Failed to create worlds updater", "error", err)
		return
	}

	updaters := []ports.UpdaterService{instanceUpdater, worldsUpdater}

	// Create backuppers
	localBackupper, err := services.NewLocalBackupper(workRoot)
	if err != nil {
		logger.Error("Failed to create local backupper", "error", err)
		return
	}

	r2Backupper, err := services.NewR2Backupper(r2Uploader, envBucket, workRoot, "", nil)
	if err != nil {
		logger.Error("Failed to create R2 backupper", "error", err)
		return
	}

	backuppers := []ports.BackupperService{localBackupper, r2Backupper}

	// Create retention services
	localRetention, err := services.NewLocalRetention(localStorage, logger)
	if err != nil {
		logger.Error("Failed to create local retention", "error", err)
		return
	}

	r2Retention, err := services.NewR2Retention(remoteStorage, logger)
	if err != nil {
		logger.Error("Failed to create R2 retention", "error", err)
		return
	}

	retentions := []ports.RetentionService{localRetention, r2Retention}

	// Create server runner
	commandExecutor := adapters.NewCommandExecutorAdapter()
	serverRunner, err := adapters.NewServerRunner(config.RootPath, commandExecutor)
	if err != nil {
		logger.Error("Failed to create server runner", "error", err)
		return
	}

	// Create Molfar service
	molfar, err := services.NewMolfarService(updaters, backuppers, retentions, serverRunner, librarian, logger, workRoot)
	if err != nil {
		logger.Error("Failed to create molfar service", "error", err)
		return
	}

	// Create server config
	server, err := domain.NewServer("0.0.0.0:25565", 4096)
	if err != nil {
		logger.Error("Failed to create server config", "error", err)
		return
	}

	// Run lifecycle
	logger.Info("Starting Ritual")

	if err := molfar.Prepare(); err != nil {
		logger.Error("Prepare phase failed", "error", err)
		return
	}

	runErr := molfar.Run(server)
	if runErr != nil {
		logger.Error("Run phase failed", "error", runErr)
	}

	// Always attempt Exit to unlock manifests, even if Run failed
	if err := molfar.Exit(); err != nil {
		logger.Error("Exit phase failed", "error", err)
		return
	}

	if runErr != nil {
		return
	}

	logger.Info("Ritual completed successfully")
}
