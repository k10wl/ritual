package services

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	ErrRitualUpdaterStoreNil     = errors.New("manifest store cannot be nil")
	ErrRitualUpdaterStorageNil   = errors.New("storage repository cannot be nil")
	ErrRitualUpdaterVersionEmpty = errors.New("binary version cannot be empty")
	ErrRitualUpdaterNil          = errors.New("ritual updater cannot be nil")
	ErrRitualCtxNil              = errors.New("context cannot be nil")
	ErrRitualRemoteManifestNil   = errors.New("remote manifest cannot be nil")
)

// RitualUpdater implements UpdaterService for ritual self-updates.
// Compares local and remote ritual versions and performs self-update if local is outdated.
type RitualUpdater struct {
	local         ports.ManifestStore
	remote        ports.ManifestStore
	storage       ports.StorageRepository
	binaryVersion string
}

// Compile-time check to ensure RitualUpdater implements ports.UpdaterService
var _ ports.UpdaterService = (*RitualUpdater)(nil)

// NewRitualUpdater creates a new ritual updater.
// binaryVersion is the version baked into the current binary (e.g., "1.0.0")
func NewRitualUpdater(
	local, remote ports.ManifestStore,
	storage ports.StorageRepository,
	binaryVersion string,
) (*RitualUpdater, error) {
	if local == nil || remote == nil {
		return nil, ErrRitualUpdaterStoreNil
	}
	if storage == nil {
		return nil, ErrRitualUpdaterStorageNil
	}
	if binaryVersion == "" {
		return nil, ErrRitualUpdaterVersionEmpty
	}

	return &RitualUpdater{
		local:         local,
		remote:        remote,
		storage:       storage,
		binaryVersion: binaryVersion,
	}, nil
}

// Run executes the ritual self-update process.
// Downloads new binary if local version is outdated, replaces current exe, and restarts.
func (u *RitualUpdater) Run(ctx context.Context) error {
	if u == nil {
		return ErrRitualUpdaterNil
	}
	if ctx == nil {
		return ErrRitualCtxNil
	}

	remoteManifest, err := u.remote.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get remote manifest: %w", err)
	}
	if remoteManifest == nil {
		return ErrRitualRemoteManifestNil
	}

	if !IsVersionOlder(u.binaryVersion, remoteManifest.RitualVersion) {
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", u.binaryVersion, remoteManifest.RitualVersion)

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}
	fmt.Printf("Current exe: %s\n", currentExe)

	// Update local manifest BEFORE replacing binary. Missing local manifest →
	// seed from remote (first run).
	fmt.Println("Updating local manifest...")
	localManifest, err := u.local.Get(ctx)
	if err != nil || localManifest == nil {
		localManifest = remoteManifest.Clone()
	} else {
		localManifest.RitualVersion = remoteManifest.RitualVersion
	}
	if err := u.local.Save(ctx, localManifest); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	// Write new binary to temp dir (can't overwrite running exe on Windows)
	// Use epoch nanoseconds to avoid collisions
	updateExe := filepath.Join(os.TempDir(), fmt.Sprintf(config.UpdateFilePattern, time.Now().UnixNano()))
	fmt.Printf("Downloading %s -> %s\n", config.RemoteBinaryKey, updateExe)
	n, err := streamToFile(ctx, u.storage, config.RemoteBinaryKey, updateExe)
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded %d bytes\n", n)

	// Launch new binary with replace flag - it will replace the old exe and restart
	fmt.Println("Launching new version...")
	cmd := exec.Command(updateExe, config.ReplaceFlag, currentExe) // #nosec G204 -- updateExe is project-controlled temp path
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

	// Copy current exe over old exe (streamed; full binary never held in RAM)
	if err := copyFile(currentExe, oldExe); err != nil {
		fmt.Printf("Failed to replace old exe: %v\n", err)
		os.Exit(1)
	}

	// Launch the replaced exe with cleanup flag
	fmt.Println("Starting updated version...")
	cmd := exec.Command(oldExe, config.CleanupFlag, currentExe) // #nosec G204,G702 -- oldExe is project self-update path
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
	_ = os.Remove(updateFile) // #nosec G703 -- updateFile is project self-update temp path
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
		_ = os.Remove(match)
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

// streamToFile downloads key from storage and copies the stream into dst.
// File handle is closed before return so the caller can immediately exec dst
// (Windows refuses exec while a write handle is open in the same process).
func streamToFile(ctx context.Context, storage ports.StorageRepository, key, dst string) (int64, error) {
	body, err := storage.GetStream(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to download %s: %w", key, err)
	}
	defer func() { _ = body.Close() }()

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, config.FilePermission) // #nosec G304 -- dst is project-controlled temp path
	if err != nil {
		return 0, fmt.Errorf("failed to open update file %s: %w", dst, err)
	}
	n, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		return n, fmt.Errorf("failed to write update file %s: %w", dst, copyErr)
	}
	if closeErr != nil {
		return n, fmt.Errorf("failed to close update file %s: %w", dst, closeErr)
	}
	return n, nil
}

// copyFile streams src → dst. Used by handleReplace to overwrite the old exe
// without buffering the whole binary.
func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is os.Executable output
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, config.FilePermission) // #nosec G304,G306 -- dst is project self-update path
	if err != nil {
		return fmt.Errorf("open %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", dst, closeErr)
	}
	return nil
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
