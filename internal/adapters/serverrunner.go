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
	commandExecutor ports.CommandExecutor
}

// NewServerRunner creates a new ServerRunner instance
func NewServerRunner(homedir string, commandExecutor ports.CommandExecutor) (*ServerRunner, error) {
	if homedir == "" {
		return nil, fmt.Errorf("homedir cannot be empty")
	}
	if commandExecutor == nil {
		return nil, fmt.Errorf("command executor cannot be nil")
	}

	return &ServerRunner{
		homedir:         homedir,
		commandExecutor: commandExecutor,
	}, nil
}

// Run executes the Minecraft server process
func (s *ServerRunner) Run(server *domain.Server) error {
	if s == nil {
		return fmt.Errorf("server runner cannot be nil")
	}
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}

	instancePath := filepath.Join(s.homedir, "instance")
	batPath := filepath.Join(instancePath, server.BatPath)

	if _, err := os.Stat(batPath); os.IsNotExist(err) {
		return fmt.Errorf("server.bat not found at %s", batPath)
	}

	args := []string{
		"/C", "start", batPath,
		server.IP,
		strconv.Itoa(server.Port),
		strconv.Itoa(server.Memory),
	}

	if err := s.commandExecutor.Execute("cmd", args, instancePath); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
