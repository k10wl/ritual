package adapters

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"ritual/internal/core/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ritualRun = "java %1 @user_jvm_args.txt @libraries/net/minecraftforge/forge/win_args.txt nogui\n"

func stubRuntime(port, memory int) func() (*domain.ServerRuntime, error) {
	return func() (*domain.ServerRuntime, error) {
		return domain.NewServerRuntime(port, memory)
	}
}

func TestNewServerCmdBuilder(t *testing.T) {
	b, err := NewServerCmdBuilder(t.TempDir(), "instance/run.bat", stubRuntime(25565, 1024))

	assert.NoError(t, err)
	assert.NotNil(t, b)
}

func TestNewServerCmdBuilder_EmptyServerPath(t *testing.T) {
	b, err := NewServerCmdBuilder("", "run.bat", stubRuntime(25565, 1024))

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "server path cannot be empty")
}

func TestNewServerCmdBuilder_EmptyStartScript(t *testing.T) {
	b, err := NewServerCmdBuilder(t.TempDir(), "", stubRuntime(25565, 1024))

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "start script cannot be empty")
}

func TestNewServerCmdBuilder_NilRuntime(t *testing.T) {
	b, err := NewServerCmdBuilder(t.TempDir(), "run.bat", nil)

	assert.Error(t, err)
	assert.Nil(t, b)
	assert.Contains(t, err.Error(), "runtime factory cannot be nil")
}

func TestServerCmdBuilder_Build(t *testing.T) {
	tempDir := t.TempDir()

	instanceDir := filepath.Join(tempDir, "instance")
	require.NoError(t, os.MkdirAll(instanceDir, 0o755))

	startScript := filepath.Join("instance", "run.bat")
	scriptPath := filepath.Join(tempDir, startScript)
	require.NoError(t, os.WriteFile(scriptPath, []byte(ritualRun), 0o644))

	b, err := NewServerCmdBuilder(tempDir, startScript, stubRuntime(25565, 1024))
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
		"--port",
		"25565",
	}
	assert.Equal(t, expected, cmd.Args)
}

// design-log/040: Build never MkdirAll's the server sandbox. When server/ is
// absent (fresh-host skip-sync, no prior Apply) it surfaces an honest error and
// leaves nothing behind — no empty server/ folder.
func TestServerCmdBuilder_Build_NoServerDir(t *testing.T) {
	serverPath := filepath.Join(t.TempDir(), "server")

	b, err := NewServerCmdBuilder(serverPath, "run.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "no server files")

	_, statErr := os.Stat(serverPath)
	assert.True(t, os.IsNotExist(statErr), "server dir must not be created on launch")
}

func TestServerCmdBuilder_Build_ScriptNotFound(t *testing.T) {
	b, err := NewServerCmdBuilder(t.TempDir(), "nonexistent.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "start script not found")
}

func TestServerCmdBuilder_Build_RuntimeError(t *testing.T) {
	failing := func() (*domain.ServerRuntime, error) {
		return nil, errors.New("settings unavailable")
	}

	b, err := NewServerCmdBuilder(t.TempDir(), "run.bat", failing)
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "settings unavailable")
}

func TestServerCmdBuilder_Build_EmptyScript(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".ritual_run"), []byte("   \n"), 0o644))

	b, err := NewServerCmdBuilder(tempDir, ".ritual_run", stubRuntime(25565, 1024))
	require.NoError(t, err)

	cmd, err := b.Build(context.Background(), nil, nil)

	assert.Error(t, err)
	assert.Nil(t, cmd)
	assert.Contains(t, err.Error(), "empty start script")
}

func TestServerCmdBuilder_Build_ContextWired(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "run.bat"), []byte(ritualRun), 0o644))

	b, err := NewServerCmdBuilder(tempDir, "run.bat", stubRuntime(25565, 1024))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd, err := b.Build(ctx, nil, nil)

	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	err = cmd.Run()
	assert.Error(t, err)
}
