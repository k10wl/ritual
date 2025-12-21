package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strconv"
)

// Compile-time check to ensure ServerRunner implements ports.ServerRunner interface
var _ ports.ServerRunner = (*ServerRunner)(nil)

// ServerRunner implements the ServerRunner interface for executing Minecraft servers
type ServerRunner struct {
	homedir         string
	startScript     string
	commandExecutor ports.CommandExecutor
}

// NewServerRunner creates a new ServerRunner instance
// startScript is the path to the bat file relative to ritual root
func NewServerRunner(homedir string, startScript string, commandExecutor ports.CommandExecutor) (*ServerRunner, error) {
	if homedir == "" {
		return nil, fmt.Errorf("homedir cannot be empty")
	}
	if startScript == "" {
		return nil, fmt.Errorf("start script cannot be empty")
	}
	if commandExecutor == nil {
		return nil, fmt.Errorf("command executor cannot be nil")
	}

	return &ServerRunner{
		homedir:         homedir,
		startScript:     startScript,
		commandExecutor: commandExecutor,
	}, nil
}

// Run executes the Minecraft server process using the configured start script
func (s *ServerRunner) Run(server *domain.Server) error {
	if s == nil {
		return fmt.Errorf("server runner cannot be nil")
	}
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}

	scriptPath := filepath.Join(s.homedir, s.startScript)

	if _, err := os.Stat(scriptPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("start script not found at %s", scriptPath)
		}
		return fmt.Errorf("failed to check start script at %s: %w", scriptPath, err)
	}

	memoryStr := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	args := []string{
		"/C", "start", "/wait", scriptPath,
		memoryStr,
	}

	workingDir := filepath.Dir(scriptPath)
	if err := s.commandExecutor.Execute("cmd", args, workingDir); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
