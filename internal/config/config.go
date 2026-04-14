package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	DirPermission  = 0755
	FilePermission = 0644
)

var RootPath string

func init() {
	AppVersion = fmt.Sprintf("%d.%d.%d", VersionMajor, VersionMinor, VersionPatch)

	workDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	RootPath = filepath.Join(workDir, GroupName, AppName)
}
