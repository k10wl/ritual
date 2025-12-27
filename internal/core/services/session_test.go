package services_test

import (
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/services"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// writeUTF16LE writes content as UTF-16 LE with BOM (like PowerShell's Tee-Object)
func writeUTF16LE(f *os.File, content string) error {
	encoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	writer := transform.NewWriter(f, encoder)
	_, err := writer.Write([]byte(content))
	if err != nil {
		return err
	}
	return writer.Close()
}

// CheckPlayersJoined Tests
//
// CheckPlayersJoined parses server.log to detect if any players joined the session.
// Used to skip backups when no players connected (no meaningful world changes).

func TestCheckPlayersJoined(t *testing.T) {
	logContentWithJoin := `[21Dec2025 20:42:48.251] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Starting Minecraft server on 0.0.0.0:25565
[21Dec2025 20:42:54.738] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Done (6.172s)! For help, type "help"
[21Dec2025 20:43:43.001] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: owl joined the game
[21Dec2025 20:44:38.959] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: owl left the game
`
	logContentNoJoin := `[21Dec2025 20:42:48.251] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Starting Minecraft server on 0.0.0.0:25565
[21Dec2025 20:42:54.738] [Server thread/INFO] [net.minecraft.server.dedicated.DedicatedServer/]: Done (6.172s)! For help, type "help"
[21Dec2025 20:44:45.997] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: Stopping the server
`

	// Test both UTF-16 LE (PowerShell Tee-Object) and UTF-8 encodings
	t.Run("UTF-16 LE: returns true when player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logPath := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
		logFile, err := os.Create(logPath)
		require.NoError(t, err)
		err = writeUTF16LE(logFile, logContentWithJoin)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})

	t.Run("UTF-16 LE: returns false when no player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logPath := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
		logFile, err := os.Create(logPath)
		require.NoError(t, err)
		err = writeUTF16LE(logFile, logContentNoJoin)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.False(t, joined)
	})

	t.Run("UTF-8: returns true when player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logPath := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
		err = os.WriteFile(logPath, []byte(logContentWithJoin), 0644)
		require.NoError(t, err)

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})

	t.Run("UTF-8: returns false when no player joined", func(t *testing.T) {
		tempDir := t.TempDir()
		workRoot, err := os.OpenRoot(tempDir)
		require.NoError(t, err)
		defer workRoot.Close()

		err = workRoot.Mkdir(config.LogsDir, 0755)
		require.NoError(t, err)

		logPath := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
		err = os.WriteFile(logPath, []byte(logContentNoJoin), 0644)
		require.NoError(t, err)

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
		logPath := filepath.Join(tempDir, config.LogsDir, config.ServerLogFilename)
		logFile, err := os.Create(logPath)
		require.NoError(t, err)
		err = writeUTF16LE(logFile, logContent)
		require.NoError(t, err)
		logFile.Close()

		joined, err := services.CheckPlayersJoined(workRoot)
		assert.NoError(t, err)
		assert.True(t, joined)
	})
}
