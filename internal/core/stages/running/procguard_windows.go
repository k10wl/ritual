//go:build windows

package running

import (
	"io"
	"os/exec"

	"github.com/kolesnikovae/go-winjob"
)

// startGuarded starts cmd attached to a Windows Job Object with
// KILL_ON_JOB_CLOSE. When the Go parent dies abnormally (panic, SIGKILL,
// terminal close), the kernel kills every process in the job — no orphan
// java subprocess. Returned Closer releases the job handle on normal
// teardown; the same kernel cleanup fires on abnormal exit.
func startGuarded(cmd *exec.Cmd) (io.Closer, error) {
	return winjob.Start(cmd, winjob.LimitKillOnJobClose)
}
