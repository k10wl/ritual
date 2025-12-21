package config

import (
	"os"
	"path/filepath"
)

// Application identity
const (
	GroupName = "k10wl"
	AppName   = "ritual"
)

// Directory names
const (
	LocalBackups  = "world_backups"
	RemoteBackups = "worlds"
	InstanceDir   = "instance"
	TmpDir        = "temp"
	LogsDir       = "logs"
)

// File names and keys
const (
	ManifestFilename    = "manifest.json"
	InstanceArchiveKey  = "instance.tar"
	RemoteBinaryKey     = "ritual.exe"
	ManualWorldFilename = "manual.tar"
	ServerJarFilename   = "paper.jar"
)

// Backup configuration
const (
	R2MaxBackups    = 2
	LocalMaxBackups = 2
	MaxFiles        = 1000
	MaxLogFiles     = 10

	TimestampFormat = "20060102150405"
	BackupExtension = ".tar"
	LogExtension    = ".log"
)

// World directories (relative to instance)
var WorldDirs = []string{
	"world",
	"world_nether",
	"world_the_end",
}

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
	UpdateFilePattern = "ritual_update_%d.exe"
	UpdateFileGlob    = "ritual_update_*.exe"
)

// Lock ID format
const (
	LockIDSeparator = "::"
)

// S3/R2 configuration
const (
	S3PartSize    = 5 * 1024 * 1024 // 5 MB parts for multipart upload
	S3Concurrency = 1               // Sequential upload to minimize memory
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
	workDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	RootPath = filepath.Join(workDir, GroupName, AppName)
}
