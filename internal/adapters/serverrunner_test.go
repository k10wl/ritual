package adapters

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"ritual/internal/config"
	"ritual/internal/core/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewServerRunner(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	mockExecutor := &MockCommandExecutor{}
	startScript := "instance/run.bat"

	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)

	assert.NoError(t, err)
	assert.NotNil(t, runner)
	assert.Equal(t, tempDir, runner.homedir)
	assert.Equal(t, startScript, runner.startScript)
	assert.Equal(t, mockExecutor, runner.commandExecutor)
}

func TestNewServerRunner_EmptyHomedir(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner("", workRoot, "run.bat", mockExecutor)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "homedir cannot be empty")
}

func TestNewServerRunner_NilWorkRoot(t *testing.T) {
	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner("/test/home", nil, "run.bat", mockExecutor)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "workRoot cannot be nil")
}

func TestNewServerRunner_EmptyStartScript(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner(tempDir, workRoot, "", mockExecutor)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "start script cannot be empty")
}

func TestNewServerRunner_NilExecutor(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	runner, err := NewServerRunner(tempDir, workRoot, "run.bat", nil)

	assert.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "command executor cannot be nil")
}

func TestServerRunner_Run(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	err = os.WriteFile(scriptPath, []byte("@echo off"), 0644)
	require.NoError(t, err)

	// Create server.properties file
	propsPath := filepath.Join(instanceDir, "server.properties")
	err = os.WriteFile(propsPath, []byte("server-ip=\nserver-port=25565\n"), 0644)
	require.NoError(t, err)

	// Create logs directory for Tee-Object
	logsDir := filepath.Join(tempDir, config.LogsDir)
	err = os.MkdirAll(logsDir, 0755)
	require.NoError(t, err)

	logFile := filepath.Join(tempDir, config.LogsDir, "server.log")
	psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, "-Xmx1024M", logFile)
	expectedArgs := []string{"/C", "start", "/wait", "powershell", "-Command", psCommand}
	mockExecutor := &MockCommandExecutor{}
	mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(nil)

	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
	require.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	require.NoError(t, err)

	err = runner.Run(server)

	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)

	// Verify server.properties was updated
	propsContent, err := os.ReadFile(propsPath)
	assert.NoError(t, err)
	assert.Contains(t, string(propsContent), "server-ip=127.0.0.1")
	assert.Contains(t, string(propsContent), "server-port=25565")
}

func TestServerRunner_Run_UpdatesServerProperties(t *testing.T) {
	// This test verifies that memory is passed to script and IP/port are written to server.properties
	testCases := []struct {
		name           string
		ip             string
		port           int
		memory         int
		expectedMemory string
	}{
		{
			name:           "standard config",
			ip:             "0.0.0.0",
			port:           25565,
			memory:         4096,
			expectedMemory: "-Xmx4096M",
		},
		{
			name:           "custom port and IP",
			ip:             "192.168.1.100",
			port:           25566,
			memory:         8192,
			expectedMemory: "-Xmx8192M",
		},
		{
			name:           "localhost",
			ip:             "127.0.0.1",
			port:           19132,
			memory:         2048,
			expectedMemory: "-Xmx2048M",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			workRoot, err := os.OpenRoot(tempDir)
			require.NoError(t, err)
			defer workRoot.Close()

			instanceDir := filepath.Join(tempDir, "instance")
			err = os.MkdirAll(instanceDir, 0755)
			require.NoError(t, err)

			startScript := filepath.Join("instance", "run.bat")
			scriptPath := filepath.Join(tempDir, startScript)
			err = os.WriteFile(scriptPath, []byte("@echo off"), 0644)
			require.NoError(t, err)

			// Create initial server.properties with different values to test override
			propsPath := filepath.Join(instanceDir, "server.properties")
			err = os.WriteFile(propsPath, []byte("server-ip=old-ip\nserver-port=12345\nother-setting=value\n"), 0644)
			require.NoError(t, err)

			logFile := filepath.Join(tempDir, config.LogsDir, "server.log")
			psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, tc.expectedMemory, logFile)
			expectedArgs := []string{
				"/C", "start", "/wait", "powershell", "-Command", psCommand,
			}

			mockExecutor := &MockCommandExecutor{}
			mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(nil)

			runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
			require.NoError(t, err)

			address := tc.ip + ":" + strconv.Itoa(tc.port)
			server, err := domain.NewServer(address, tc.memory)
			require.NoError(t, err)

			err = runner.Run(server)

			assert.NoError(t, err)
			mockExecutor.AssertExpectations(t)

			// Verify server.properties was updated with correct IP and port (overriding old values)
			propsContent, err := os.ReadFile(propsPath)
			assert.NoError(t, err)
			assert.Contains(t, string(propsContent), "server-ip="+tc.ip)
			assert.Contains(t, string(propsContent), "server-port="+strconv.Itoa(tc.port))
			assert.Contains(t, string(propsContent), "other-setting=value", "Other settings should be preserved")
			assert.NotContains(t, string(propsContent), "old-ip", "Old IP should be replaced")
			assert.NotContains(t, string(propsContent), "12345", "Old port should be replaced")
		})
	}
}

func TestServerRunner_Run_CreatesServerPropertiesIfMissing(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	err = os.WriteFile(scriptPath, []byte("@echo off"), 0644)
	require.NoError(t, err)

	// Note: NOT creating server.properties - it should be created

	logFile := filepath.Join(tempDir, config.LogsDir, "server.log")
	psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, "-Xmx2048M", logFile)
	expectedArgs := []string{"/C", "start", "/wait", "powershell", "-Command", psCommand}
	mockExecutor := &MockCommandExecutor{}
	mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(nil)

	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
	require.NoError(t, err)
	server, err := domain.NewServer("192.168.1.50:25570", 2048)
	require.NoError(t, err)

	err = runner.Run(server)

	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)

	// Verify server.properties was created with IP and port
	propsPath := filepath.Join(instanceDir, "server.properties")
	propsContent, err := os.ReadFile(propsPath)
	assert.NoError(t, err)
	assert.Contains(t, string(propsContent), "server-ip=192.168.1.50")
	assert.Contains(t, string(propsContent), "server-port=25570")
}

func TestServerRunner_Run_NilRunner(t *testing.T) {
	var runner *ServerRunner
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	require.NoError(t, err)

	err = runner.Run(server)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server runner cannot be nil")
}

func TestServerRunner_Run_NilServer(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	startScript := "run.bat"
	scriptPath := filepath.Join(tempDir, startScript)
	err = os.WriteFile(scriptPath, []byte("@echo off"), 0644)
	require.NoError(t, err)

	mockExecutor := &MockCommandExecutor{}
	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
	require.NoError(t, err)

	err = runner.Run(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server cannot be nil")
}

func TestServerRunner_Run_ScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	startScript := "nonexistent.bat"

	mockExecutor := &MockCommandExecutor{}

	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
	require.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	require.NoError(t, err)

	err = runner.Run(server)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "start script not found")
}

func TestServerRunner_Run_CommandExecutionError(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	err = os.MkdirAll(instanceDir, 0755)
	require.NoError(t, err)

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	err = os.WriteFile(scriptPath, []byte("@echo off"), 0644)
	require.NoError(t, err)

	// Create server.properties file
	propsPath := filepath.Join(instanceDir, "server.properties")
	err = os.WriteFile(propsPath, []byte("server-ip=\nserver-port=25565\n"), 0644)
	require.NoError(t, err)

	logFile := filepath.Join(tempDir, config.LogsDir, "server.log")
	psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, "-Xmx1024M", logFile)
	expectedArgs := []string{"/C", "start", "/wait", "powershell", "-Command", psCommand}
	mockExecutor := &MockCommandExecutor{}
	expectedError := errors.New("command failed")
	mockExecutor.On("Execute", "cmd", expectedArgs, instanceDir).Return(expectedError)

	runner, err := NewServerRunner(tempDir, workRoot, startScript, mockExecutor)
	require.NoError(t, err)
	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	require.NoError(t, err)

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
