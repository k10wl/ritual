package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
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

// Run executes the Minecraft server process using Java
func (s *ServerRunner) Run(server *domain.Server) error {
	if s == nil {
		return fmt.Errorf("server runner cannot be nil")
	}
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}

	instancePath := filepath.Join(s.homedir, config.InstanceDir)
	jarPath := filepath.Join(instancePath, config.ServerJarFilename)

	if _, err := os.Stat(jarPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s not found at %s", config.ServerJarFilename, jarPath)
		}
		return fmt.Errorf("failed to check %s at %s: %w", config.ServerJarFilename, jarPath, err)
	}

	memoryStr := strconv.Itoa(server.Memory) + "M"
	args := []string{
		"/C", "start", "/wait", "java",
		"-Xms" + memoryStr,
		"-Xmx" + memoryStr,
		"-jar", config.ServerJarFilename,
		"nogui",
	}

	if err := s.commandExecutor.Execute("cmd", args, instancePath); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
