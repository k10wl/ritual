// Package adapters provides concrete implementations of the core ports.
package adapters

import (
	"context"
	"errors"
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

// ServerCmdBuilder composes the server start command from a start script and runtime settings.
type ServerCmdBuilder struct {
	workRoot    *os.Root
	startScript string
	runtime     func() (*domain.ServerRuntime, error)
}

// NewServerCmdBuilder validates inputs and returns a ServerCmdBuilder ready to build commands.
func NewServerCmdBuilder(workRoot *os.Root, startScript string, runtime func() (*domain.ServerRuntime, error)) (*ServerCmdBuilder, error) {
	if workRoot == nil {
		return nil, errors.New("workRoot cannot be nil")
	}
	if startScript == "" {
		return nil, errors.New("start script cannot be empty")
	}
	if runtime == nil {
		return nil, errors.New("runtime factory cannot be nil")
	}

	return &ServerCmdBuilder{
		workRoot:    workRoot,
		startScript: startScript,
		runtime:     runtime,
	}, nil
}

// Build reads the start script and returns an *exec.Cmd configured with runtime memory/port.
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
	defer func() { _ = f.Close() }()

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
	// #nosec G204 -- parts sourced from project-controlled start script, not user input
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd, nil
}
