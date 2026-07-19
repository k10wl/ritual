//go:build !windows

package adapters

import "os/exec"

func hideCmdWindow(_ *exec.Cmd) {}
