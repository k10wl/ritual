package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strconv"
)

var _ ports.CmdBuilder = (*ServerCmdBuilder)(nil)

type ServerCmdBuilder struct {
	workRoot    *os.Root
	startScript string
	runtime     func() (*domain.ServerRuntime, error)
}

func NewServerCmdBuilder(workRoot *os.Root, startScript string, runtime func() (*domain.ServerRuntime, error)) (*ServerCmdBuilder, error) {
	if workRoot == nil {
		return nil, fmt.Errorf("workRoot cannot be nil")
	}
	if startScript == "" {
		return nil, fmt.Errorf("start script cannot be empty")
	}
	if runtime == nil {
		return nil, fmt.Errorf("runtime factory cannot be nil")
	}

	return &ServerCmdBuilder{
		workRoot:    workRoot,
		startScript: startScript,
		runtime:     runtime,
	}, nil
}

func (b *ServerCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	server, err := b.runtime()
	if err != nil {
		return nil, fmt.Errorf("resolve runtime: %w", err)
	}

	if _, err := b.workRoot.Stat(b.startScript); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("start script not found at %s", b.startScript)
		}
		return nil, fmt.Errorf("failed to check start script at %s: %w", b.startScript, err)
	}

	rootPath := b.workRoot.Name()
	scriptPath := filepath.Join(rootPath, b.startScript)
	memoryArg := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	logFile := filepath.Join(rootPath, config.LogsDir, config.ServerLogFilename)
	psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, memoryArg, logFile)
	args := []string{
		"/C", "start", "/wait", "powershell", "-Command", psCommand,
	}

	workingDir := filepath.Dir(scriptPath)
	cmd := exec.Command("cmd", args...)
	cmd.Dir = workingDir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd, nil
}

