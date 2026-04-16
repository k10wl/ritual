package adapters

import (
	"bufio"
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

	memoryArg := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	parts, err := parseJavaInvocation(f, memoryArg)
	if err != nil {
		return nil, fmt.Errorf("parse start script %s: %w", b.startScript, err)
	}

	scriptPath := filepath.Join(b.workRoot.Name(), b.startScript)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd, nil
}

func parseJavaInvocation(r io.Reader, memoryArg string) ([]string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(strings.ToLower(line), "rem ") {
			continue
		}
		line = strings.ReplaceAll(line, "%1", memoryArg)
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		return parts, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no java invocation found")
}
