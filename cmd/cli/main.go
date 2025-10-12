package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/domain"
	"ritual/internal/core/services"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// Initialize structured logger
	logger := slog.Default()
	logger.Info("Starting R.I.T.U.A.L. application")

	err := godotenv.Load()
	if err != nil {
		logger.Warn("Environment file not found", "error", err)
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("Failed to get home directory", "error", err)
		log.Fatalf("Failed to get home directory: %v", err)
	}

	workdir := filepath.Join(homedir, "k10wl", "ritual")
	logger.Info("Setting up working directory", "workdir", workdir)
	err = os.MkdirAll(workdir, 0755)
	if err != nil {
		logger.Error("Failed to create workdir", "workdir", workdir, "error", err)
		log.Fatalf("Failed to create workdir: %v", err)
	}

	logger.Info("Initializing R2 repository")
	r2, err := adapters.NewR2Repository(
		os.Getenv("R2_BUCKET_NAME"),
		os.Getenv("R2_ACCOUNT_ID"),
		os.Getenv("R2_ACCESS_KEY_ID"),
		os.Getenv("R2_SECRET_ACCESS_KEY"),
	)
	if err != nil {
		logger.Error("Failed to create R2 repository", "error", err)
		log.Fatalf("Failed to create R2 repository: %v", err)
	}

	logger.Info("Initializing filesystem repository", "workdir", workdir)
	fs, err := adapters.NewFSRepository(workdir)
	if err != nil {
		logger.Error("Failed to create filesystem repository", "workdir", workdir, "error", err)
		log.Fatalf("Failed to create filesystem repository: %v", err)
	}

	logger.Info("Initializing services")
	librarian, err := services.NewLibrarianService(fs, r2)
	if err != nil {
		logger.Error("Failed to create librarian service", "error", err)
		log.Fatalf("Failed to create librarian service: %v", err)
	}

	validator, err := services.NewValidatorService()
	if err != nil {
		logger.Error("Failed to create validator service", "error", err)
		log.Fatalf("Failed to create validator service: %v", err)
	}
	archive := services.NewArchiveService()

	logger.Info("Initializing command executor")
	commandExecutor := adapters.NewCommandExecutorAdapter()

	logger.Info("Initializing server runner")
	serverRunner, err := adapters.NewServerRunner(homedir, commandExecutor)
	if err != nil {
		logger.Error("Failed to create server runner", "error", err)
		log.Fatalf("Failed to create server runner: %v", err)
	}

	logger.Info("Initializing Molfar service")
	molfar, err := services.NewMolfarService(
		librarian,
		validator,
		archive,
		fs,
		r2,
		serverRunner,
		logger,
		workdir,
	)
	if err != nil {
		logger.Error("Failed to create Molfar service", "error", err)
		log.Fatalf("Failed to create Molfar service: %v", err)
	}

	logger.Info("Creating new manifest for QA testing")

	// Create QA world
	world, err := domain.NewWorld("worlds.zip")
	if err != nil {
		logger.Error("Failed to create world", "error", err)
		log.Fatalf("Failed to create world: %v", err)
	}

	newManifest := &domain.Manifest{
		RitualVersion:   "1.0.0-qa",
		LockedBy:        "",
		InstanceVersion: fmt.Sprintf("qa-%d", time.Now().Unix()),
		StoredWorlds:    []domain.World{*world},
		UpdatedAt:       time.Now(),
	}

	logger.Info("Uploading new manifest to R2", "instance_version", newManifest.InstanceVersion)
	ctx := context.Background()
	err = librarian.SaveRemoteManifest(ctx, newManifest)
	if err != nil {
		logger.Error("Failed to upload manifest to R2", "error", err)
		log.Fatalf("Failed to upload manifest to R2: %v", err)
	}

	logger.Info("Running Molfar preparation")
	err = molfar.Prepare()
	if err != nil {
		logger.Error("Failed to prepare Molfar", "error", err)
		log.Fatalf("Failed to prepare Molfar: %v", err)
	}

	logger.Info("Molfar preparation completed successfully")
}
