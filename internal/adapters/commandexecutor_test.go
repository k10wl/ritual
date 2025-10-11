package adapters

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandExecutorAdapter_Execute_Success(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	workingDir, err := os.MkdirTemp("", "commandexecutor_test")
	assert.NoError(t, err)
	defer os.RemoveAll(workingDir)

	cmdPath, err := exec.LookPath("cmd")
	assert.NoError(t, err)

	err = adapter.Execute(cmdPath, []string{"/c", "exit 0"}, workingDir)
	assert.NoError(t, err)
}

func TestCommandExecutorAdapter_Execute_NilReceiver(t *testing.T) {
	var adapter *CommandExecutorAdapter

	err := adapter.Execute("test", []string{}, "/tmp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestCommandExecutorAdapter_Execute_EmptyCommand(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	err := adapter.Execute("", []string{}, "/tmp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestCommandExecutorAdapter_Execute_NilArgs(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	err := adapter.Execute("test", nil, "/tmp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestCommandExecutorAdapter_Execute_EmptyWorkingDir(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	err := adapter.Execute("test", []string{}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "working directory cannot be empty")
}

func TestCommandExecutorAdapter_Execute_ExecutionFailure(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	workingDir, err := os.MkdirTemp("", "commandexecutor_test")
	assert.NoError(t, err)
	defer os.RemoveAll(workingDir)

	err = adapter.Execute("nonexistent-command-xyz", []string{}, workingDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute command")

	var execErr *exec.Error
	assert.True(t, errors.As(err, &execErr))
}

func TestCommandExecutorAdapter_Execute_CommandExitFailure(t *testing.T) {
	adapter := NewCommandExecutorAdapter()

	workingDir, err := os.MkdirTemp("", "commandexecutor_test")
	assert.NoError(t, err)
	defer os.RemoveAll(workingDir)

	cmdPath, err := exec.LookPath("cmd")
	assert.NoError(t, err)

	err = adapter.Execute(cmdPath, []string{"/c", "exit 1"}, workingDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute command")
}
