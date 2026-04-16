package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/core/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const forgeBat = "@echo off\njava %1 @user_jvm_args.txt @libraries/net/minecraftforge/forge/win_args.txt nogui\n"

func stubRuntime(port, memory int) func() (*domain.ServerRuntime, error) {
	return func() (*domain.ServerRuntime, error) {
		return domain.NewServerRuntime(port, memory)
	}
}

func TestNewServerCmdBuilder(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "instance/run.bat", stubRuntime(25565, 1024))

	assert.NoError(t, err)
	assert.NotNil(t, b)
}

func TestNewServerCmdBuilder_NilWorkRoot(t *testing.T) {
	b, err := NewServerCmdBuilder(nil, "run.bat", stubRuntime(25565, 1024))

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "workRoot cannot be nil")
}

func TestNewServerCmdBuilder_EmptyStartScript(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "", stubRuntime(25565, 1024))

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
	require.NoError(t, os.WriteFile(scriptPath, []byte(forgeBat), 0644))

	b, err := NewServerCmdBuilder(workRoot, startScript, stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	assert.Equal(t, instanceDir, cmd.Dir)
	expected := []string{
		"java",
		"-Xmx1024M",
		"@user_jvm_args.txt",
		"@libraries/net/minecraftforge/forge/win_args.txt",
		"nogui",
	}
	assert.Equal(t, expected, cmd.Args)
}

func TestServerCmdBuilder_Build_ScriptNotFound(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	b, err := NewServerCmdBuilder(workRoot, "nonexistent.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

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

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "settings unavailable")
}

func TestServerCmdBuilder_Build_NoJavaLine(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "run.bat"), []byte("@echo off\n"), 0644))

	b, err := NewServerCmdBuilder(workRoot, "run.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "no java invocation found")
}

func TestServerCmdBuilder_Build_ContextWired(t *testing.T) {
	tempDir := t.TempDir()
	workRoot, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer workRoot.Close()

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "run.bat"), []byte(forgeBat), 0644))

	b, err := NewServerCmdBuilder(workRoot, "run.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd, err := b.Build(ctx, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	err = cmd.Run()
	assert.Error(t, err)
}
