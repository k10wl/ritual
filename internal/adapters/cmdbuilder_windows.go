//go:build windows

package adapters

import (
	"os/exec"
	"syscall"
)

// hideCmdWindow suppresses the console window that Windows would open for
// a console-subsystem child process (e.g. java.exe) when spawned from a
// windowsgui parent. CREATE_NO_WINDOW and HideWindow together cover both
// the CreateProcess flag and the STARTUPINFO wShowWindow path.
func hideCmdWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
