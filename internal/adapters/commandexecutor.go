package adapters

import (
	"fmt"
	"os/exec"
)

// CommandExecutorAdapter implements the CommandExecutor interface
type CommandExecutorAdapter struct{}

// NewCommandExecutorAdapter creates a new CommandExecutorAdapter instance
func NewCommandExecutorAdapter() *CommandExecutorAdapter {
	return &CommandExecutorAdapter{}
}

// Execute runs a command with the given arguments and working directory
func (c *CommandExecutorAdapter) Execute(command string, args []string, workingDir string) error {
	if c == nil {
		return fmt.Errorf("command executor adapter cannot be nil")
	}
	if command == "" {
		return fmt.Errorf("command cannot be empty")
	}
	if args == nil {
		return fmt.Errorf("args cannot be nil")
	}
	if workingDir == "" {
		return fmt.Errorf("working directory cannot be empty")
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = workingDir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}
