// Package config defines build-time and runtime configuration for the app.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version info (single source of truth)
const (
	VersionMajor = 2
	VersionMinor = 0
	VersionPatch = 0
)

// Application identity
const (
	GroupName   = "k10wl"
	ProductName = "Ritual"
	Description = "Ritual - Minecraft Server Manager"
)

// AppName is injected at build time via ldflags (ritualdev or ritual)
var AppName = "ritualdev"

// AppVersion is the semver string derived from VersionMajor/Minor/Patch.
var AppVersion string

// Directory names
const (
	BackupsDir = "backups" // unified local/R2 backup prefix
	ServerDir  = "server"
	WorldsDir  = "worlds"
	TmpDir     = "temp"
	LogsDir    = "logs"
)

// File names and keys
const (
	ManifestFilename  = "manifest.json"
	RemoteBinaryKey   = "ritual.exe"
	ServerJarFilename = "paper.jar"
	ServerLogFilename = "server.log"
)

// Backup configuration
const (
	MaxFiles    = 1000
	MaxLogFiles = 10

	TimestampFormat = "20060102150405"
	LogExtension    = ".log"
)

// Default manifest thresholds
const (
	DefaultMinRAMMB       = 4096 // 4GB
	DefaultMinDiskMB      = 5120 // 5GB
	DefaultMinJavaVersion = 21
)

// Lease defaults — applied by Manifest.ApplyDefaults when absent on decode.
const (
	DefaultHeartbeatInterval = 5 * time.Minute
	DefaultLeaseTTL          = 21 * time.Minute
)

// Update process flags
const (
	ReplaceFlag = "--replace-old"
	CleanupFlag = "--cleanup-update"
)

// Update process timing
const (
	UpdateProcessDelayMs = 500
)

// Update file patterns
const (
	UpdateFilePattern = "update_%d.exe"
	UpdateFileGlob    = "update_*.exe"
)

// Sync staging patterns
const (
	TempRitualDir      = "ritual"
	SyncStagingPattern = "sync_%d"
	SyncStagingGlob    = "sync_*"
)

// TempRitualPath returns the OS temp directory joined with TempRitualDir.
func TempRitualPath() string {
	return filepath.Join(os.TempDir(), TempRitualDir)
}

// Lock ID format
const (
	LockIDSeparator = "::"
)

// R2 endpoint format
const (
	R2EndpointFormat = "https://%s.r2.cloudflarestorage.com"
)

// File permissions
const (
	DirPermission  = 0o755
	FilePermission = 0o644
)

// RootPath is the absolute on-disk working root, computed at init from UserHomeDir.
var RootPath string

func init() {
	AppVersion = fmt.Sprintf("%d.%d.%d", VersionMajor, VersionMinor, VersionPatch)

	workDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	RootPath = filepath.Join(workDir, GroupName, AppName)
}
