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
	VersionMinor = 3
	VersionPatch = 0
)

// Application identity
const (
	GroupName   = "k10wl"
	ProductName = "Ritual"
	Description = "Ritual - Minecraft Server Manager"
)

// AppName identifies the build variant: "ritualdev" for the dev iteration
// build, "ritual" for the production release. Set via -ldflags
// -X ritual/internal/config.AppName=<name> by build/windows/Taskfile.yml
// based on RITUAL_ENV. Drives RootPath (~/<GroupName>/<AppName>) so the two
// variants never share on-disk state, and DisplayName() so the window title
// telegraphs which variant is running.
var AppName = "ritualdev"

// AppVersion is the semver string derived from VersionMajor/Minor/Patch.
var AppVersion string

// DisplayName returns the user-visible product name. Dev builds get a " Dev"
// suffix so the window title and OS taskbar entry are distinguishable from
// the production release at a glance — mirrors the .syso ProductName written
// by cmd/genversioninfo -dev.
func DisplayName() string {
	if AppName == "ritualdev" {
		return ProductName + " Dev"
	}
	return ProductName
}

// Directory names
const (
	ServerDir = "server"
	WorldsDir = "worlds"
	TmpDir    = "temp"
	LogsDir   = "logs"
)

// DefaultCommitTargets is the production allowlist of doublestar globs
// passed to refs.Committer when the workdir is the project root. It scopes
// what a commit captures and — via Apply — what a pull is allowed to
// prune. Operational dirs (refs/, objects/, logs/, remote-mock/), the
// settings file, and host-local server caches are deliberately absent so
// they never enter the ref or get destroyed by a downstream Apply.
//
// Origin: docs/dev-session-2026-04-25-poc-setup.md audit fix #8 — pre-fix
// targets=[]string{"**"} with workdir=worlds/ tracked nothing under
// server/, so a fresh host could not pull-and-run.
//
// Editing this list is a behavioural change. Read the audit doc + run
// the regression test in internal/core/refs/commit_test.go before
// extending the scope.
var DefaultCommitTargets = []string{
	"worlds/**",
	"server/server.jar",
	"server/server.properties",
	"server/eula.txt",
	"server/start.bat",
	"server/user_jvm_args.txt",
	"server/libraries/**",
	"server/mods/**",
	"server/config/**",
	"server/defaultconfigs/**",
	"server/ops.json",
	"server/whitelist.json",
	"server/banned-*.json",
}

// File names and keys
const (
	ManifestFilename  = "manifest.json"
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

// Self-update lives in internal/subsystems/selfupdate now (design-log/037):
// the old CLI-era --replace-old/--cleanup-update process dance + update_*.exe
// temp files are gone, replaced by minio/selfupdate's in-process atomic
// rename and the bin/<os-arch>/<version>/<sha> layout.

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

// WorkRoot is the content root (objects/refs/server/worlds, design-log/055);
// defaults to RootPath and may be relocated by the user. Set once at boot
// by buildRuntime via ResolveWorkRoot, and thereafter mutated only by
// relocating.Strategy's commit step.
var WorkRoot string

// ResolveWorkRoot returns workRoot if it is a non-empty absolute path, else
// RootPath (today's single-root layout). Takes the already-extracted
// settings.WorkRoot string rather than *domain.Settings to avoid an import
// cycle (internal/core/domain already imports this package).
func ResolveWorkRoot(workRoot string) string {
	if workRoot != "" && filepath.IsAbs(workRoot) {
		return workRoot
	}
	return RootPath
}

func init() {
	AppVersion = fmt.Sprintf("%d.%d.%d", VersionMajor, VersionMinor, VersionPatch)

	workDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	RootPath = filepath.Join(workDir, GroupName, AppName)
}
