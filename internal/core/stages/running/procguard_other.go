//go:build !windows

// Package running implements the server-process stage. The job-object guard is
// Windows-only; this file stubs it out on dev hosts so the package still builds.
package running

import (
	"io"
	"os/exec"
)

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// startGuarded on non-Windows falls back to plain cmd.Start. Ritual
// targets Windows only; this stub exists so the package builds on dev
// machines (darwin/linux) without pulling in go-winjob's Windows deps.
func startGuarded(cmd *exec.Cmd) (io.Closer, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return noopCloser{}, nil
}
