package services_test

import (
	"os"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CheckPlayersJoined Tests
//
// CheckPlayersJoined parses server.log to detect if any players joined the session.
// Used to skip backups when no players connected (no meaningful world changes).

func TestCheckPlayersJoined(t *testing.T) {
	t.Run("returns true when player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		// Create logs directory and server.log
		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logContent := `[21Dec2025 20:42:48.251] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Starting Minecraft server on 0.0.0.0:25565
[21Dec2025 20:42:54.738] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Done (6.172s)! For help, type "help"
[21Dec2025 20:43:43.001] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: owl joined the game
[21Dec2025 20:44:38.959] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: owl left the game
`
		logFile, err := workRoot.Create(config.LogsDir + "/server.log")
		require.NoError(t, err)
		_, err = logFile.WriteString(logContent)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})

	t.Run("returns false when no player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logContent := `[21Dec2025 20:42:48.251] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Starting Minecraft server on 0.0.0.0:25565
[21Dec2025 20:42:54.738] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Done (6.172s)! For help, type "help"
[21Dec2025 20:44:45.997] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Stopping the server
`
		logFile, err := workRoot.Create(config.LogsDir + "/server.log")
		require.NoError(t, err)
		_, err = logFile.WriteString(logContent)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.False(t, joined)
	})

	t.Run("returns false when log file missing", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		// No logs directory or file created

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.False(t, joined)
	})

	t.Run("handles multiple players joining", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logContent := `[21Dec2025 20:43:43.001] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Player1 joined the game
[21Dec2025 20:44:00.000] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Player2 joined the game
[21Dec2025 20:45:00.000] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Player3 joined the game
`
		logFile, err := workRoot.Create(config.LogsDir + "/server.log")
		require.NoError(t, err)
		_, err = logFile.WriteString(logContent)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})

	t.Run("returns early on first match", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		// First line has a join - should return immediately
		logContent := `[21Dec2025 20:43:43.001] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: owl joined the game
... thousands more lines ...
`
		logFile, err := workRoot.Create(config.LogsDir + "/server.log")
		require.NoError(t, err)
		_, err = logFile.WriteString(logContent)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})
}
