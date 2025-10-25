package testhelpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants for instance setup
const (
	expectedInstanceFileCount = 16          // Updated count for instance without worlds
	serverJarSize             = 1024 * 1024 // 1MB
	pluginJarSize             = 512 * 1024  // 512KB
)

// Unit Tests for PaperInstanceSetup

func TestPaperInstanceSetup(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, createdFiles, compareFunc, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Verify temp directory exists
	_, err = os.Stat(tempDir)
	require.NoError(t, err)

	// Verify all expected files were created
	expectedFiles := []string{
		// Server files
		"server.properties",
		"server.jar",
		"eula.txt",
		"bukkit.yml",
		"spigot.yml",
		"paper.yml",
		// Plugin files
		"plugins/worldedit.jar",
		"plugins/worldedit.yml",
		"plugins/essentials.jar",
		"plugins/essentials.yml",
		"plugins/luckperms.jar",
		"plugins/luckperms.yml",
		"plugins/vault.jar",
		"plugins/vault.yml",
		// Log files
		"logs/latest.log",
		"logs/debug.log",
	}

	assert.ElementsMatch(t, expectedFiles, createdFiles)

	// Verify files actually exist
	for _, file := range createdFiles {
		filePath := filepath.Join(tempDir, file)
		_, err := os.Stat(filePath)
		require.NoError(t, err, "File %s should exist", file)
	}

	// Test comparison function with identical directory
	err = compareFunc(tempDir)
	assert.NoError(t, err, "Comparison should pass for identical directory")

	// Test comparison function with different directory (should fail)
	differentDir := t.TempDir()
	err = compareFunc(differentDir)
	assert.Error(t, err, "Comparison should fail for different directory")
}

func TestPaperInstanceSetupWithCustomDir(t *testing.T) {
	customDir := filepath.Join(t.TempDir(), "custom")
	err := os.MkdirAll(customDir, 0755)
	require.NoError(t, err)

	tempDir, _, compareFunc, err := PaperInstanceSetup(customDir, "1.20.1")
	require.NoError(t, err)

	// Verify custom directory was used
	assert.Equal(t, customDir, tempDir)

	// Verify temp directory exists
	_, err = os.Stat(tempDir)
	require.NoError(t, err)

	// Verify server files exist
	serverPropsPath := filepath.Join(tempDir, "server.properties")
	_, err = os.Stat(serverPropsPath)
	require.NoError(t, err)

	serverJarPath := filepath.Join(tempDir, "server.jar")
	_, err = os.Stat(serverJarPath)
	require.NoError(t, err)

	// Test comparison function with identical directory
	err = compareFunc(tempDir)
	assert.NoError(t, err, "Comparison should pass for identical directory")
}

func TestPaperInstanceSetupComparison(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, _, compareFunc, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Create a copy of the directory structure
	copyDir := t.TempDir()

	// Copy all files to new directory
	err = copyDirectory(tempDir, copyDir)
	require.NoError(t, err)

	// Test comparison with copied directory (should pass)
	err = compareFunc(copyDir)
	assert.NoError(t, err, "Comparison should pass for copied directory")

	// Modify one file in the copy
	modifiedFile := filepath.Join(copyDir, "server.properties")
	err = os.WriteFile(modifiedFile, []byte("modified content"), 0644)
	require.NoError(t, err)

	// Test comparison with modified file (should fail)
	err = compareFunc(copyDir)
	assert.Error(t, err, "Comparison should fail for modified file")
}

func TestPaperInstanceSetupFailure(t *testing.T) {
	tempDir := t.TempDir()
	tempDir, _, compareFunc, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Create a directory with missing files
	incompleteDir := t.TempDir()

	// Only create server.properties
	serverPropsPath := filepath.Join(incompleteDir, "server.properties")
	err = os.WriteFile(serverPropsPath, []byte("incomplete server"), 0644)
	require.NoError(t, err)

	// Test comparison should fail due to missing files
	err = compareFunc(incompleteDir)
	assert.Error(t, err, "Comparison should fail for incomplete directory")
	// The error could be either missing file or content mismatch
	assert.True(t, strings.Contains(err.Error(), "does not exist in new location") ||
		strings.Contains(err.Error(), "content differs between original and new location"))

	// Create directory with wrong content
	wrongContentDir := t.TempDir()
	err = copyDirectory(tempDir, wrongContentDir)
	require.NoError(t, err)

	// Modify a file content
	modifiedFile := filepath.Join(wrongContentDir, "server.properties")
	err = os.WriteFile(modifiedFile, []byte("wrong content"), 0644)
	require.NoError(t, err)

	// Test comparison should fail due to content mismatch
	err = compareFunc(wrongContentDir)
	assert.Error(t, err, "Comparison should fail for modified content")
	assert.Contains(t, err.Error(), "content differs between original and new location")
}

func TestPaperInstanceSetup_VersionParameter(t *testing.T) {
	// Test with different versions
	testVersions := []string{"1.19.4", "1.20.1", "1.20.4", "1.21.0"}

	for _, version := range testVersions {
		t.Run("version_"+version, func(t *testing.T) {
			tempDir := t.TempDir()
			_, _, _, err := PaperInstanceSetup(tempDir, version)
			require.NoError(t, err)

			// Verify version is written to paper.yml
			paperPath := filepath.Join(tempDir, "paper.yml")
			content, err := os.ReadFile(paperPath)
			require.NoError(t, err)
			assert.Contains(t, string(content), "version: "+version, "Paper config should have correct version %s", version)
		})
	}
}

func TestPaperInstanceSetup_NegativeCases(t *testing.T) {
	// Test with invalid directory path
	invalidDir := "/invalid/path/that/does/not/exist"
	_, _, _, err := PaperInstanceSetup(invalidDir, "1.20.1")
	assert.Error(t, err, "Should fail with invalid directory path")

	// Test with read-only directory (if possible on current OS)
	tempDir := t.TempDir()
	readOnlyDir := filepath.Join(tempDir, "readonly")
	err = os.MkdirAll(readOnlyDir, 0444) // Read-only permissions
	require.NoError(t, err)

	// On Windows, read-only directories may still allow file creation
	// So we'll skip this test if it doesn't fail as expected
	_, _, _, err = PaperInstanceSetup(readOnlyDir, "1.20.1")
	if err != nil {
		assert.Error(t, err, "Should fail with read-only directory")
	} else {
		t.Log("Read-only directory test skipped - OS allows file creation in read-only dir")
	}
}

func TestPaperInstanceSetup_ComparisonFunction_NegativeCases(t *testing.T) {
	tempDir := t.TempDir()
	_, _, compareFunc, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Test comparison with non-existent directory
	err = compareFunc("/non/existent/path")
	assert.Error(t, err, "Comparison should fail with non-existent directory")

	// Test comparison with empty directory
	emptyDir := t.TempDir()
	err = compareFunc(emptyDir)
	assert.Error(t, err, "Comparison should fail with empty directory")

	// Test comparison with directory containing only some files
	partialDir := t.TempDir()
	serverPropsPath := filepath.Join(partialDir, "server.properties")
	err = os.WriteFile(serverPropsPath, []byte("test"), 0644)
	require.NoError(t, err)

	err = compareFunc(partialDir)
	assert.Error(t, err, "Comparison should fail with partial directory structure")
}

// Integration Tests

func TestPaperInstanceIntegration_ServerFileFormatCompliance(t *testing.T) {
	tempDir := t.TempDir()
	_, createdFiles, _, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Test server.properties format compliance
	serverPropsPath := filepath.Join(tempDir, "server.properties")
	content, err := os.ReadFile(serverPropsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "server-port=25565", "Server properties should have port")
	assert.Contains(t, string(content), "level-name=world", "Server properties should have level name")

	// Test eula.txt format compliance
	eulaPath := filepath.Join(tempDir, "eula.txt")
	content, err = os.ReadFile(eulaPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "eula=true", "EULA should be accepted")

	// Test bukkit.yml format compliance
	bukkitPath := filepath.Join(tempDir, "bukkit.yml")
	content, err = os.ReadFile(bukkitPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "settings:", "Bukkit config should have settings")

	// Test spigot.yml format compliance
	spigotPath := filepath.Join(tempDir, "spigot.yml")
	content, err = os.ReadFile(spigotPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "config-version:", "Spigot config should have version")

	// Test paper.yml format compliance
	paperPath := filepath.Join(tempDir, "paper.yml")
	content, err = os.ReadFile(paperPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "version: 1.20.1", "Paper config should have correct version")

	// Test server.jar size compliance
	serverJarPath := filepath.Join(tempDir, "server.jar")
	info, err := os.Stat(serverJarPath)
	require.NoError(t, err)
	assert.Equal(t, int64(serverJarSize), info.Size(), "Server jar should be 1MB")

	// Test plugin jar size compliance
	pluginJarPath := filepath.Join(tempDir, "plugins/worldedit.jar")
	info, err = os.Stat(pluginJarPath)
	require.NoError(t, err)
	assert.Equal(t, int64(pluginJarSize), info.Size(), "Plugin jar should be 512KB")

	// Verify comprehensive file count
	assert.GreaterOrEqual(t, len(createdFiles), expectedInstanceFileCount, "Should create comprehensive instance structure")
}

func TestPaperInstanceIntegration_DirectoryStructure(t *testing.T) {
	tempDir := t.TempDir()
	_, _, _, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Test instance directory structure
	expectedDirs := []string{
		"plugins",
		"logs",
	}

	for _, dir := range expectedDirs {
		dirPath := filepath.Join(tempDir, dir)
		info, err := os.Stat(dirPath)
		require.NoError(t, err, "Directory %s should exist", dir)
		assert.True(t, info.IsDir(), "Path %s should be a directory", dir)
	}
}

func TestPaperInstanceIntegration_PluginConfiguration(t *testing.T) {
	tempDir := t.TempDir()
	_, _, _, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Test plugin configuration files
	pluginNames := []string{"worldedit", "essentials", "luckperms", "vault"}
	for _, pluginName := range pluginNames {
		// Test plugin jar exists
		pluginJarPath := filepath.Join(tempDir, "plugins", pluginName+".jar")
		_, err := os.Stat(pluginJarPath)
		require.NoError(t, err, "Plugin jar %s should exist", pluginName)

		// Test plugin config exists
		configPath := filepath.Join(tempDir, "plugins", pluginName+".yml")
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), pluginName, "Plugin config should contain plugin name")
		assert.Contains(t, string(content), "enabled: true", "Plugin should be enabled")
	}
}

func TestPaperInstanceIntegration_LogFiles(t *testing.T) {
	tempDir := t.TempDir()
	_, _, _, err := PaperInstanceSetup(tempDir, "1.20.1")
	require.NoError(t, err)

	// Test latest.log format
	latestLogPath := filepath.Join(tempDir, "logs/latest.log")
	content, err := os.ReadFile(latestLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Starting minecraft server", "Latest log should contain server start")
	assert.Contains(t, string(content), "Paper version", "Latest log should contain Paper version")

	// Test debug.log format
	debugLogPath := filepath.Join(tempDir, "logs/debug.log")
	content, err = os.ReadFile(debugLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Server startup initiated", "Debug log should contain startup")
	assert.Contains(t, string(content), "Loading configuration", "Debug log should contain config loading")
}
