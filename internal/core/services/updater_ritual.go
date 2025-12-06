package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"strconv"
	"strings"
	"time"
)

// RitualUpdater error constants
var (
	ErrRitualUpdaterLibrarianNil  = errors.New("librarian service cannot be nil")
	ErrRitualUpdaterStorageNil    = errors.New("storage repository cannot be nil")
	ErrRitualUpdaterVersionEmpty  = errors.New("binary version cannot be empty")
	ErrRitualUpdaterNil           = errors.New("ritual updater cannot be nil")
	ErrRitualCtxNil               = errors.New("context cannot be nil")
	ErrRitualRemoteManifestNil    = errors.New("remote manifest cannot be nil")
)

// RitualUpdater implements UpdaterService for ritual self-updates
// Compares local and remote ritual versions and performs self-update if local is outdated
type RitualUpdater struct {
	librarian     ports.LibrarianService
	storage       ports.StorageRepository
	binaryVersion string
}

// Compile-time check to ensure RitualUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*RitualUpdater)(nil)

// NewRitualUpdater creates a new ritual updater
// binaryVersion is the version baked into the current binary (e.g., "1.0.0")
func NewRitualUpdater(
	librarian ports.LibrarianService,
	storage ports.StorageRepository,
	binaryVersion string,
) (*RitualUpdater, error) {
	if librarian == nil {
		return nil, ErrRitualUpdaterLibrarianNil
	}
	if storage == nil {
		return nil, ErrRitualUpdaterStorageNil
	}
	if binaryVersion == "" {
		return nil, ErrRitualUpdaterVersionEmpty
	}

	return &RitualUpdater{
		librarian:     librarian,
		storage:       storage,
		binaryVersion: binaryVersion,
	}, nil
}

// Run executes the ritual self-update process
// Downloads new binary if local version is outdated, replaces current exe, and restarts
func (u *RitualUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrRitualUpdaterNil
	}
	if ctx == nil {
		return ErrRitualCtxNil
	}

	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest == nil {
		return ErrRitualRemoteManifestNil
	}

	// Compare binary version (source of truth) against remote manifest
	if !IsVersionOlder(u.binaryVersion, remoteManifest.RitualVersion) {
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", u.binaryVersion, remoteManifest.RitualVersion)

	// Download new binary from remote (always stored as ritual.exe by convention)
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}
	fmt.Printf("Current exe: %s\n", currentExe)

	fmt.Printf("Downloading %s...\n", config.RemoteBinaryKey)
	data, err := u.storage.Get(ctx, config.RemoteBinaryKey)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", config.RemoteBinaryKey, err)
	}
	fmt.Printf("Downloaded %d bytes\n", len(data))

	// Update local manifest BEFORE replacing binary
	// If local manifest doesn't exist (first run), create from remote
	fmt.Println("Updating local manifest...")
	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		// First run - create local manifest from remote
		localManifest = remoteManifest.Clone()
	} else {
		localManifest.RitualVersion = remoteManifest.RitualVersion
	}
	if err := u.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	// Write new binary to temp dir (can't overwrite running exe on Windows)
	// Use epoch nanoseconds to avoid collisions
	updateExe := filepath.Join(os.TempDir(), fmt.Sprintf(config.UpdateFilePattern, time.Now().UnixNano()))
	fmt.Printf("Writing update to: %s\n", updateExe)
	if err := os.WriteFile(updateExe, data, config.FilePermission); err != nil {
		return fmt.Errorf("failed to write update file: %w", err)
	}

	// Launch new binary with replace flag - it will replace the old exe and restart
	fmt.Println("Launching new version...")
	cmd := exec.Command(updateExe, config.ReplaceFlag, currentExe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start update: %w", err)
	}

	os.Exit(0)
	return nil
}

// HandleUpdateProcess handles update-related flags and cleanup
// Returns true if this is an update process and main should exit
func HandleUpdateProcess() bool {
	// Handle --replace-old flag (called by old version to replace itself)
	if len(os.Args) >= 3 && os.Args[1] == config.ReplaceFlag {
		handleReplace(os.Args[2])
		return true
	}

	// Handle --cleanup-update flag (called after replacement to clean temp file)
	if len(os.Args) >= 3 && os.Args[1] == config.CleanupFlag {
		handleCleanup(os.Args[2])
		// Continue running normally after cleanup
		return false
	}

	// Normal startup - try to clean any leftover update file
	cleanupLeftoverUpdateFile()
	return false
}

func handleReplace(oldExe string) {
	currentExe, err := os.Executable()
	if err != nil {
		fmt.Printf("Failed to get current exe: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Replacing %s with %s\n", oldExe, currentExe)

	// Wait for old process to exit
	time.Sleep(config.UpdateProcessDelayMs * time.Millisecond)

	// Copy current exe over old exe
	data, err := os.ReadFile(currentExe)
	if err != nil {
		fmt.Printf("Failed to read current exe: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(oldExe, data, config.FilePermission); err != nil {
		fmt.Printf("Failed to replace old exe: %v\n", err)
		os.Exit(1)
	}

	// Launch the replaced exe with cleanup flag
	fmt.Println("Starting updated version...")
	cmd := exec.Command(oldExe, config.CleanupFlag, currentExe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to start: %v\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func handleCleanup(updateFile string) {
	// Wait for update process to exit
	time.Sleep(config.UpdateProcessDelayMs * time.Millisecond)
	os.Remove(updateFile)
	// Remove cleanup args so app runs normally
	os.Args = append(os.Args[:1], os.Args[3:]...)
}

func cleanupLeftoverUpdateFile() {
	// Clean any leftover ritual_update_*.exe files from temp dir
	pattern := filepath.Join(os.TempDir(), config.UpdateFileGlob)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, match := range matches {
		os.Remove(match)
	}
}

// IsVersionOlder returns true if local version is older than remote version
// Compares semantic versions: major.minor.patch (e.g., "1.2.3")
func IsVersionOlder(local, remote string) bool {
	localParts := parseVersion(local)
	remoteParts := parseVersion(remote)

	// Compare each part: major, minor, patch
	for i := 0; i < len(localParts) && i < len(remoteParts); i++ {
		if localParts[i] < remoteParts[i] {
			return true
		}
		if localParts[i] > remoteParts[i] {
			return false
		}
	}

	// If all compared parts are equal, shorter version is older (1.0 < 1.0.1)
	return len(localParts) < len(remoteParts)
}

// parseVersion parses a version string into numeric parts
// "1.2.3" -> [1, 2, 3]
func parseVersion(version string) []int {
	var parts []int
	for part := range strings.SplitSeq(version, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}
