package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"ritual/internal/config"
	"ritual/internal/core/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubRuntime(address string, memory int) func() (*domain.ServerRuntime, error) {
	return func() (*domain.ServerRuntime, error) {
		return domain.NewServerRuntime(address, memory)
	}
}

func TestNewServerCmdBuilder(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "instance/run.bat", stubRuntime("127.0.0.1:25565", 1024))

	assert.NoError(t, err)
	assert.NotNil(t, b)
}

func TestNewServerCmdBuilder_NilWorkRoot(t *testing.T) {
	b, err := NewServerCmdBuilder(nil, "run.bat", stubRuntime("127.0.0.1:25565", 1024))

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "workRoot cannot be nil")
}

func TestNewServerCmdBuilder_EmptyStartScript(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "", stubRuntime("127.0.0.1:25565", 1024))

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "start script cannot be empty")
}

func TestNewServerCmdBuilder_NilRuntime(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "run.bat", nil)

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "runtime factory cannot be nil")
}

func TestServerCmdBuilder_Build(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	require.NoError(t, os.MkdirAll(instanceDir, 0755))

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	require.NoError(t, os.WriteFile(scriptPath, []byte("@echo off"), 0644))

	propsPath := filepath.Join(instanceDir, "server.properties")
	require.NoError(t, os.WriteFile(propsPath, []byte("server-ip=\nserver-port=25565\n"), 0644))

	b, err := NewServerCmdBuilder(workRoot, startScript, stubRuntime("127.0.0.1:25565", 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background())

	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	assert.Equal(t, instanceDir, cmd.Dir)

	logFile := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
	psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, "-Xmx1024M", logFile)
	expectedArgs := []string{"cmd", "/C", "start", "/wait", "powershell", "-Command", psCommand}
	assert.Equal(t, expectedArgs, cmd.Args)

	propsContent, err := os.ReadFile(propsPath)
	assert.NoError(t, err)
	assert.Contains(t, string(propsContent), "server-ip=127.0.0.1")
	assert.Contains(t, string(propsContent), "server-port=25565")
}

func TestServerCmdBuilder_Build_UpdatesServerProperties(t *testing.T) {
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
			require.NoError(t, os.MkdirAll(instanceDir, 0755))

			startScript := filepath.Join("instance", "run.bat")
			scriptPath := filepath.Join(tempDir, startScript)
			require.NoError(t, os.WriteFile(scriptPath, []byte("@echo off"), 0644))

			propsPath := filepath.Join(instanceDir, "server.properties")
			require.NoError(t, os.WriteFile(propsPath, []byte("server-ip=old-ip\nserver-port=12345\nother-setting=value\n"), 0644))

			address := tc.ip + ":" + strconv.Itoa(tc.port)
			b, err := NewServerCmdBuilder(workRoot, startScript, stubRuntime(address, tc.memory))
			require.NoError(t, err)

			cmd, err := b.Build(context.Background())

			assert.NoError(t, err)
			assert.NotNil(t, cmd)

			logFile := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
			psCommand := fmt.Sprintf("& '%s' %s 2>&1 | Tee-Object -FilePath '%s'", scriptPath, tc.expectedMemory, logFile)
			expectedArgs := []string{"cmd", "/C", "start", "/wait", "powershell", "-Command", psCommand}
			assert.Equal(t, expectedArgs, cmd.Args)

			propsContent, err := os.ReadFile(propsPath)
			assert.NoError(t, err)
			assert.Contains(t, string(propsContent), "server-ip="+tc.ip)
			assert.Contains(t, string(propsContent), "server-port="+strconv.Itoa(tc.port))
			assert.Contains(t, string(propsContent), "other-setting=value")
			assert.NotContains(t, string(propsContent), "old-ip")
			assert.NotContains(t, string(propsContent), "12345")
		})
	}
}

func TestServerCmdBuilder_Build_CreatesServerPropertiesIfMissing(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	instanceDir := filepath.Join(tempDir, "instance")
	require.NoError(t, os.MkdirAll(instanceDir, 0755))

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	require.NoError(t, os.WriteFile(scriptPath, []byte("@echo off"), 0644))

	b, err := NewServerCmdBuilder(workRoot, startScript, stubRuntime("192.168.1.50:25570", 2048))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background())

	assert.NoError(t, err)
	assert.NotNil(t, cmd)

	propsPath := filepath.Join(instanceDir, "server.properties")
	propsContent, err := os.ReadFile(propsPath)
	assert.NoError(t, err)
	assert.Contains(t, string(propsContent), "server-ip=192.168.1.50")
	assert.Contains(t, string(propsContent), "server-port=25570")
}

func TestServerCmdBuilder_Build_ScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "nonexistent.bat", stubRuntime("127.0.0.1:25565", 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background())

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "start script not found")
}

func TestServerCmdBuilder_Build_RuntimeError(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	failing := func() (*domain.ServerRuntime, error) {
		return nil, fmt.Errorf("settings unavailable")
	}

	b, err := NewServerCmdBuilder(workRoot, "run.bat", failing)
	require.NoError(t, err)

	cmd, err := b.Build(context.Background())

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "settings unavailable")
}

func TestServerCmdBuilder_Build_ContextWired(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "run.bat"), []byte("@echo off"), 0644))

	b, err := NewServerCmdBuilder(workRoot, "run.bat", stubRuntime("127.0.0.1:25565", 1024))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd, err := b.Build(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	err = cmd.Run()
	assert.Error(t, err)
}
