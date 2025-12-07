package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/config"
	"ritual/internal/core/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewServerRunner(t *testing.T) {
	mockExecutor := &MockCommandExecutor{}
	homedir := "/test/home"

	runner, err := NewServerRunner(homedir, mockExecutor)

	assert.NoError(t, err)
	assert.NotNil(t, runner)
	assert.Equal(t, homedir, runner.homedir)
	assert.Equal(t, mockExecutor, runner.commandExecutor)
}

func TestNewServerRunner_EmptyHomedir(t *testing.T) {
	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner("", mockExecutor)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "homedir cannot be empty")
}

func TestNewServerRunner_NilExecutor(t *testing.T) {
	homedir := "/test/home"

	runner, err := NewServerRunner(homedir, nil)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "command executor cannot be nil")
}

func TestServerRunner_Run(t *testing.T) {
	tempDir := t.TempDir()
	instanceDir := filepath.Join(tempDir, config.InstanceDir)
	err := os.MkdirAll(instanceDir, 0755)
	assert.NoError(t, err)

	jarPath := filepath.Join(instanceDir, config.ServerJarFilename)
	err = os.WriteFile(jarPath, []byte("mock jar"), 0644)
	assert.NoError(t, err)

	expectedArgs := []string{"/C", "start", "/wait", "java", "-Xms1024M", "-Xmx1024M", "-jar", config.ServerJarFilename, "nogui"}
	mockExecutor := &MockCommandExecutor{}
	mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(nil)

	runner, err := NewServerRunner(tempDir, mockExecutor)
	assert.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	assert.NoError(t, err)

	err = runner.Run(server)

	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestServerRunner_Run_NilRunner(t *testing.T) {
	var runner *ServerRunner
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	assert.NoError(t, err)

	err = runner.Run(server)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server runner cannot be nil")
}

func TestServerRunner_Run_NilServer(t *testing.T) {
	mockExecutor := &MockCommandExecutor{}
	runner, err := NewServerRunner("/test", mockExecutor)
	assert.NoError(t, err)

	err = runner.Run(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server cannot be nil")
}

func TestServerRunner_Run_JarFileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	instanceDir := filepath.Join(tempDir, config.InstanceDir)
	err := os.MkdirAll(instanceDir, 0755)
	assert.NoError(t, err)

	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner(tempDir, mockExecutor)
	assert.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	assert.NoError(t, err)

	err = runner.Run(server)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), config.ServerJarFilename+" not found")
}

func TestServerRunner_Run_CommandExecutionError(t *testing.T) {
	tempDir := t.TempDir()
	instanceDir := filepath.Join(tempDir, config.InstanceDir)
	err := os.MkdirAll(instanceDir, 0755)
	assert.NoError(t, err)

	jarPath := filepath.Join(instanceDir, config.ServerJarFilename)
	err = os.WriteFile(jarPath, []byte("mock jar"), 0644)
	assert.NoError(t, err)

	expectedArgs := []string{"/C", "start", "/wait", "java", "-Xms1024M", "-Xmx1024M", "-jar", config.ServerJarFilename, "nogui"}
	mockExecutor := &MockCommandExecutor{}
	expectedError := errors.New("command failed")
	mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(expectedError)

	runner, err := NewServerRunner(tempDir, mockExecutor)
	assert.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	assert.NoError(t, err)

	err = runner.Run(server)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start server")
	mockExecutor.AssertExpectations(t)
}

type MockCommandExecutor struct {
	mock.Mock
}

func (m *MockCommandExecutor) Execute(command string, args []string, workingDir string) error {
	argsMock := m.Called(command, args, workingDir)
	return argsMock.Error(0)
}
