package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strconv"
	"strings"
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

	f, err := b.workRoot.Open(b.startScript)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("start script not found at %s", b.startScript)
		}
		return nil, fmt.Errorf("open start script at %s: %w", b.startScript, err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read start script %s: %w", b.startScript, err)
	}

	memoryArg := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	line := strings.ReplaceAll(strings.TrimSpace(string(content)), "%1", memoryArg)
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty start script %s", b.startScript)
	}
	parts = append(parts, "--port", strconv.Itoa(server.Port))

	scriptPath := filepath.Join(b.workRoot.Name(), b.startScript)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd, nil
}
