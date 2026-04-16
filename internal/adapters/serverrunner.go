package adapters

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/config"
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

func (b *ServerCmdBuilder) Build(ctx context.Context) (*exec.Cmd, error) {
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

	if err := b.updateServerProperties(server); err != nil {
		return nil, fmt.Errorf("failed to update server.properties: %w", err)
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
	cmd := exec.CommandContext(ctx, "cmd", args...)
	cmd.Dir = workingDir

	return cmd, nil
}

func (b *ServerCmdBuilder) updateServerProperties(server *domain.ServerRuntime) error {
	propsPath := filepath.Join(filepath.Dir(b.startScript), "server.properties")

	file, err := b.workRoot.Open(propsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return b.writeServerProperties(propsPath, server, nil)
		}
		return fmt.Errorf("failed to open server.properties: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read server.properties: %w", err)
	}

	return b.writeServerProperties(propsPath, server, lines)
}

func (b *ServerCmdBuilder) writeServerProperties(propsPath string, server *domain.ServerRuntime, existingLines []string) error {
	portStr := strconv.Itoa(server.Port)
	foundIP := false
	foundPort := false

	var newLines []string
	for _, line := range existingLines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "server-ip=") {
			newLines = append(newLines, "server-ip="+server.IP)
			foundIP = true
		} else if strings.HasPrefix(trimmed, "server-port=") {
			newLines = append(newLines, "server-port="+portStr)
			foundPort = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !foundIP {
		newLines = append(newLines, "server-ip="+server.IP)
	}
	if !foundPort {
		newLines = append(newLines, "server-port="+portStr)
	}

	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}

	file, err := b.workRoot.Create(propsPath)
	if err != nil {
		return fmt.Errorf("failed to create server.properties: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	return err
}
