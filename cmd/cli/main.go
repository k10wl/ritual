package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/core/services"
	"ritual/internal/testhelpers"
	"time"
)

const _ritualPath = "/k10wl/ritual"

func main() {
	fmt.Println("Hello, World!")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	ritualPath := filepath.Join(homeDir, _ritualPath)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	mockpath := filepath.Join(ritualPath, "mockworld")
	if _, err := os.Stat(mockpath); os.IsNotExist(err) {
		if err := os.MkdirAll(mockpath, 0755); err != nil {
			panic(err)
		}
	} else {
		// Clean up existing directory
		if err := os.RemoveAll(mockpath); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(mockpath, 0755); err != nil {
			panic(err)
		}
	}
	tempDir, createdFiles, _, err := testhelpers.PaperMinecraftWorldSetup(mockpath)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Println("World created successfully")
	fmt.Println("Temp dir:", tempDir)
	fmt.Println("Created files:", createdFiles)

	// Create storage and archive services using ritual root
	fs, err := adapters.NewFSRepository(ritualPath)
	if err != nil {
		log.Fatalf("Error creating storage: %v", err)
	}
	defer fs.Close()

	archiveService, err := services.NewArchiveService(ritualPath)
	if err != nil {
		log.Fatalf("Error creating archive service: %v", err)
	}

	// Generate timestamp for backup
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("world_backup_%s", timestamp)

	log.Printf("Starting world archiving process...")
	log.Printf("Source world directory: %s", mockpath)
	log.Printf("Backup name: %s", backupName)

	// Archive the world
	ctx := context.Background()
	archivePath, _, err := services.ArchivePaperWorld(
		ctx,
		fs,
		archiveService,
		"mockworld",
		"backups",
		backupName,
	)
	if err != nil {
		log.Fatalf("Error archiving world: %v", err)
	}

	log.Printf("World archived successfully to: %s", archivePath)
	log.Printf("Archive size: checking...")

	// Check archive size
	if stat, err := os.Stat(filepath.Join(ritualPath, archivePath)); err == nil {
		log.Printf("Archive size: %d bytes", stat.Size())
	}

	fmt.Println("Archiving completed successfully!")
}
