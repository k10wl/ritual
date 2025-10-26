package config

import (
	"os"
	"path/filepath"
)

const (
	groupName = "k10wl"
	appName   = "ritualdev"

	LocalBackups  = "world_backups"
	RemoteBackups = "worlds"

	InstanceDir = "instance"
	TmpDir      = "temp"
)

var RootPath string

func init() {
	workDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	RootPath = filepath.Join(workDir, groupName, appName)
}
