package adapters

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strconv"
	"strings"
)

// Compile-time check to ensure ServerRunner implements ports.ServerRunner interface
var _ ports.ServerRunner = (*ServerRunner)(nil)

// ServerRunner implements the ServerRunner interface for executing Minecraft servers
type ServerRunner struct {
	homedir         string
	workRoot        *os.Root
	startScript     string
	commandExecutor ports.CommandExecutor
}

// NewServerRunner creates a new ServerRunner instance
// startScript is the path to the bat file relative to ritual root
func NewServerRunner(homedir string, workRoot *os.Root, startScript string, commandExecutor ports.CommandExecutor) (*ServerRunner, error) {
	if homedir == "" {
		return nil, fmt.Errorf("homedir cannot be empty")
	}
	if workRoot == nil {
		return nil, fmt.Errorf("workRoot cannot be nil")
	}
	if startScript == "" {
		return nil, fmt.Errorf("start script cannot be empty")
	}
	if commandExecutor == nil {
		return nil, fmt.Errorf("command executor cannot be nil")
	}

	return &ServerRunner{
		homedir:         homedir,
		workRoot:        workRoot,
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

	// Check if script exists using workRoot
	if _, err := s.workRoot.Stat(s.startScript); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("start script not found at %s", s.startScript)
		}
		return fmt.Errorf("failed to check start script at %s: %w", s.startScript, err)
	}

	// Update server.properties with IP and port before starting
	if err := s.updateServerProperties(server); err != nil {
		return fmt.Errorf("failed to update server.properties: %w", err)
	}

	scriptPath := filepath.Join(s.homedir, s.startScript)
	memoryArg := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	args := []string{
		"/C", "start", "/wait", "cmd", "/C", scriptPath,
		memoryArg,
	}

	workingDir := filepath.Dir(scriptPath)
	if err := s.commandExecutor.Execute("cmd", args, workingDir); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

// updateServerProperties modifies server.properties to set IP and port
func (s *ServerRunner) updateServerProperties(server *domain.Server) error {
	propsPath := filepath.Join(filepath.Dir(s.startScript), "server.properties")

	// Read existing properties using workRoot
	file, err := s.workRoot.Open(propsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new file with just IP and port
			return s.writeServerProperties(propsPath, server, nil)
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

	return s.writeServerProperties(propsPath, server, lines)
}

// writeServerProperties writes the updated server.properties file
func (s *ServerRunner) writeServerProperties(propsPath string, server *domain.Server, existingLines []string) error {
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

	// Add missing properties
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

	// Write using workRoot
	file, err := s.workRoot.Create(propsPath)
	if err != nil {
		return fmt.Errorf("failed to create server.properties: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	return err
}
